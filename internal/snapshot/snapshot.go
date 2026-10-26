package snapshot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/zekihan/external-secrets-sops/internal/config"
	"github.com/zekihan/external-secrets-sops/internal/source"
)

type Key struct{ Project, Config, Environment string }
type Snapshot struct {
	revision    string
	validatedAt time.Time
	data        map[Key]map[string]string
	bytes       int64
}

func (s *Snapshot) Revision() string       { return s.revision }
func (s *Snapshot) ValidatedAt() time.Time { return s.validatedAt }
func (s *Snapshot) Bytes() int64           { return s.bytes }
func (s *Snapshot) Count() int             { return len(s.data) }
func (s *Snapshot) Values(k Key) (map[string]string, bool) {
	v, ok := s.data[k]
	return maps.Clone(v), ok
}

func (s *Snapshot) Value(
	k Key,
	name string,
) (string, bool) {
	v, ok := s.data[k][name]
	return v, ok
}
func (s *Snapshot) Renew(at time.Time) *Snapshot {
	return &Snapshot{revision: s.revision, validatedAt: at, data: s.data, bytes: s.bytes}
}

type Store struct{ active atomic.Pointer[Snapshot] }

func (s *Store) Load() *Snapshot             { return s.active.Load() }
func (s *Store) Publish(candidate *Snapshot) { s.active.Store(candidate) }
func (s *Store) Ready(now time.Time, maxAge time.Duration) bool {
	v := s.Load()
	return v != nil && (maxAge == 0 || now.Sub(v.validatedAt) <= maxAge)
}

type Decrypter interface {
	Decrypt(context.Context, []byte) ([]byte, error)
}
type Builder struct {
	cfg     config.Config
	decrypt Decrypter
}

func NewBuilder(cfg config.Config, d Decrypter) *Builder { return &Builder{cfg: cfg, decrypt: d} }

type scalar struct {
	kind   string
	value  string
	remove bool
}

func parse(data []byte) (map[string]scalar, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		return nil, errors.New("invalid secret YAML")
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("secret must contain one YAML document")
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("secret must be a flat mapping")
	}
	mapping := doc.Content[0]
	if mapping.Tag != "!!map" || mapping.Anchor != "" {
		return nil, errors.New("secret mapping must not contain tags or anchors")
	}
	out := make(map[string]scalar, len(mapping.Content)/2)
	for i := 0; i < len(mapping.Content); i += 2 {
		k, v := mapping.Content[i], mapping.Content[i+1]
		if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || !config.Component(k.Value) ||
			k.Value == "<<" {
			return nil, errors.New("invalid secret key")
		}
		if _, ok := out[k.Value]; ok {
			return nil, errors.New("duplicate secret key")
		}
		if k.Anchor != "" || v.Anchor != "" {
			return nil, errors.New("secret values must not contain anchors or aliases")
		}
		if v.Kind == yaml.MappingNode && v.Tag == "!!map" && len(v.Content) == 2 {
			field, value := v.Content[0], v.Content[1]
			if field.Kind != yaml.ScalarNode || field.Tag != "!!str" || field.Anchor != "" ||
				(field.Value != "$ref" && field.Value != "$template") || value.Kind != yaml.ScalarNode || value.Tag != "!!str" || value.Anchor != "" {
				return nil, errors.New("invalid secret descriptor")
			}
			out[k.Value] = scalar{kind: field.Value, value: value.Value}
			continue
		}
		if v.Kind != yaml.ScalarNode {
			return nil, errors.New("invalid secret value")
		}
		switch v.Tag {
		case "!!str", "!!bool", "!!int", "!!float":
			out[k.Value] = scalar{value: v.Value}
		case "!!null":
			out[k.Value] = scalar{remove: true}
		default:
			return nil, errors.New("unsupported secret scalar type")
		}
	}
	return out, nil
}

func (b *Builder) Build(
	ctx context.Context,
	revision string,
	at time.Time,
	docs []source.Document,
) (*Snapshot, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	parsed := make([]map[string]scalar, len(docs))
	jobs := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	var total int64
	fail := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
	}
	for range b.cfg.Limits.DecryptWorkers {
		wg.Go(func() {
			for i := range jobs {
				if ctx.Err() != nil {
					continue
				}
				plain, err := b.decrypt.Decrypt(ctx, docs[i].Encrypted)
				if err != nil {
					fail(errors.New("secret decryption failed"))
					continue
				}
				if ctx.Err() != nil {
					continue
				}
				mu.Lock()
				total += int64(len(plain))
				over := int64(len(plain)) > b.cfg.Limits.DocumentBytes ||
					total > b.cfg.Limits.SnapshotBytes
				mu.Unlock()
				if over {
					fail(errors.New("decrypted candidate exceeds size limit"))
					continue
				}
				values, err := parse(plain)
				if err != nil {
					fail(
						fmt.Errorf(
							"secret document %q/%q/%q: %w",
							docs[i].Project,
							docs[i].Config,
							docs[i].Environment,
							err,
						),
					)
					continue
				}
				parsed[i] = values
			}
		})
	}
send:
	for i := range docs {
		select {
		case <-ctx.Done():
			break send
		case jobs <- i:
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw := make(map[Key]map[string]scalar, len(docs))
	for i, d := range docs {
		k := Key{d.Project, d.Config, d.Environment}
		if _, exists := raw[k]; exists {
			return nil, errors.New("duplicate secret document")
		}
		raw[k] = parsed[i]
	}
	mergedData := make(map[Key]map[string]scalar)
	for k, values := range raw {
		allowed := false
		for _, env := range b.cfg.Scope.Environments {
			if k.Environment == env {
				allowed = true
			}
		}
		if !allowed || !slices.Contains(b.cfg.Scope.Projects, k.Project) {
			continue
		}
		merged := make(map[string]scalar)
		apply := func(v map[string]scalar) {
			for name, s := range v {
				if s.remove {
					delete(merged, name)
				} else {
					merged[name] = s
				}
			}
		}
		if k.Environment != "base" {
			apply(raw[Key{k.Project, k.Config, "base"}])
		}
		apply(values)
		mergedData[k] = merged
	}
	if len(mergedData) == 0 {
		return nil, errors.New("no selected environment documents")
	}
	data, size, err := resolve(ctx, mergedData, b.cfg.Limits)
	if err != nil {
		return nil, err
	}
	return &Snapshot{revision: revision, validatedAt: at, data: data, bytes: size}, nil
}
