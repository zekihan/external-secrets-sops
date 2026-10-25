package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/zekihan/external-secrets-sops/internal/config"
)

type Document struct {
	Project     string
	Config      string
	Environment string
	Encrypted   []byte
}
type Marker struct {
	Generation  string    `json:"generation,omitempty"`
	Revision    string    `json:"revision"`
	Fingerprint string    `json:"fingerprint"`
	ValidatedAt time.Time `json:"validatedAt"`
}
type Repository struct {
	cfg     config.Config
	dir     string
	auth    transport.AuthMethod
	repo    *git.Repository
	stage   string
	fetched string
}

func New(cfg config.Config, token string) (*Repository, error) {
	if err := plumbing.NewBranchReferenceName(cfg.Git.Branch).Validate(); err != nil {
		return nil, errors.New("invalid Git branch")
	}
	sum := sha256.Sum256([]byte(cfg.Git.URL + "\x00" + cfg.Git.Branch))
	r := &Repository{cfg: cfg, dir: filepath.Join(cfg.Git.CacheDir, hex.EncodeToString(sum[:]))}
	if token != "" {
		r.auth = &http.BasicAuth{Username: "x-access-token", Password: token}
	}
	return r, nil
}

// open also understands the pre-quota bare cache for offline upgrade recovery.
func (r *Repository) open() error {
	if r.repo != nil {
		return nil
	}
	m, err := r.marker()
	if err != nil {
		return err
	}
	dir := r.dir
	if m.Generation != "" {
		dir = filepath.Join(r.dir, m.Generation)
	}
	r.repo, err = git.PlainOpen(dir)
	if err != nil {
		return errors.New("cannot open validated Git cache")
	}
	if m.Generation == "" {
		remote, e := r.repo.Remote("origin")
		if e != nil || len(remote.Config().URLs) != 1 || remote.Config().URLs[0] != r.cfg.Git.URL {
			r.repo = nil
			return errors.New("git cache remote mismatch")
		}
	}
	return nil
}

func (r *Repository) marker() (Marker, error) {
	var m Marker
	f, err := os.Open(filepath.Join(r.dir, "validated.json"))
	if err != nil {
		return m, fmt.Errorf("validated cache unavailable: %w", err)
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(b) > 4096 || json.Unmarshal(b, &m) != nil ||
		(m.Generation != "" && m.Generation != "slot-0" && m.Generation != "slot-1") {
		return Marker{}, errors.New("validated cache metadata invalid")
	}
	return m, nil
}

func (r *Repository) Fetch(ctx context.Context) (string, error) {
	// The serialized syncer is the only writer. Never write into the validated slot.
	m, markerErr := r.marker()
	if markerErr != nil && !errors.Is(markerErr, os.ErrNotExist) {
		return "", errors.New("cannot identify validated Git cache")
	}

	slot := "slot-0"
	if m.Generation == slot {
		slot = "slot-1"
	}
	stage := filepath.Join(r.dir, slot)
	r.repo = nil
	r.stage = ""
	r.fetched = ""
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		return "", errors.New("cannot create Git cache")
	}
	if err := os.RemoveAll(stage); err != nil {
		return "", errors.New("cannot clear Git candidate")
	}
	repo, err := git.PlainInit(stage, true)
	if err != nil {
		return "", errors.New("cannot create Git candidate")
	}
	revision, err := r.fetchCandidate(ctx, repo, stage)
	if err != nil {
		_ = os.RemoveAll(stage)
		return "", errors.New("bounded Git fetch failed")
	}
	r.repo, r.stage, r.fetched = repo, slot, revision
	return revision, nil
}

func (r *Repository) Cached() (Marker, error) {
	var m Marker
	if err := r.open(); err != nil {
		return m, err
	}
	m, err := r.marker()
	if err != nil {
		return m, err
	}
	if m.Fingerprint != r.cfg.Fingerprint() || !plumbing.IsHash(m.Revision) ||
		m.ValidatedAt.IsZero() || m.ValidatedAt.After(time.Now()) {
		return Marker{}, errors.New("validated cache metadata invalid")
	}
	return m, nil
}

func (r *Repository) Save(revision string, at time.Time) error {
	if r.repo == nil || r.stage == "" || revision != r.fetched {
		return errors.New("git candidate is not available")
	}
	if err := r.repo.Storer.SetReference(
		plumbing.NewHashReference("refs/ess/validated", plumbing.NewHash(revision)),
	); err != nil {
		return errors.New("cannot retain validated Git revision")
	}
	b, err := json.Marshal(
		Marker{
			Generation:  r.stage,
			Revision:    revision,
			Fingerprint: r.cfg.Fingerprint(),
			ValidatedAt: at,
		},
	)
	if err != nil {
		return errors.New("cannot encode cache marker")
	}
	f, err := os.CreateTemp(r.dir, ".validated-*")
	if err != nil {
		return errors.New("cannot create cache marker")
	}
	name := f.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err = f.Write(b); err != nil {
		_ = f.Close()
		return errors.New("cannot write cache marker")
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return errors.New("cannot sync cache marker")
	}
	if err = f.Close(); err != nil {
		return errors.New("cannot close cache marker")
	}
	if err = os.Rename(name, filepath.Join(r.dir, "validated.json")); err != nil {
		return errors.New("cannot publish cache marker")
	}
	return nil
}

func (b *treeBudget) directory(
	ctx context.Context,
	tree *object.Tree,
	name string,
) (*object.Tree, error) {
	entry, err := b.lookup(ctx, tree, name)
	if errors.Is(err, object.ErrEntryNotFound) {
		return nil, nil
	}
	if err != nil || entry.Mode != filemode.Dir {
		return nil, errors.New("selected path is not a regular directory")
	}
	sub, err := b.load(ctx, entry.Hash)
	if err != nil {
		return nil, err
	}
	return sub, nil
}

func (r *Repository) Documents(ctx context.Context, revision string) ([]Document, error) {
	if err := r.open(); err != nil {
		return nil, err
	}
	commit, err := r.repo.CommitObject(plumbing.NewHash(revision))
	if err != nil {
		return nil, errors.New("cannot read selected commit")
	}
	budget := treeBudget{r: r}
	tree, err := budget.load(ctx, commit.TreeHash)
	if err != nil {
		return nil, err
	}
	if r.cfg.Git.Root != "." {
		for _, part := range strings.Split(r.cfg.Git.Root, "/") {
			tree, err = budget.directory(ctx, tree, part)
			if err != nil {
				return nil, err
			}
			if tree == nil {
				return nil, errors.New("secrets root missing")
			}
		}
	}
	// Prepare the trusted scope once rather than allocating it per component.
	envs := make([]string, 0, len(r.cfg.Scope.Environments)+1)
	seen := make(map[string]bool)
	for _, env := range r.cfg.Scope.Environments {
		if !seen[env] {
			envs = append(envs, env)
			seen[env] = true
		}
	}
	explicitBase := seen["base"]
	if !explicitBase {
		envs = append(envs, "base")
	}
	var docs []Document
	var total int64
	components := 0
	for _, project := range r.cfg.Scope.Projects {
		ptree, e := budget.directory(ctx, tree, project)
		if e != nil {
			return nil, e
		}
		if ptree == nil {
			continue
		}
		for _, entry := range ptree.Entries {
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			if !config.Component(entry.Name) {
				return nil, errors.New("invalid config path")
			}
			// Files directly under a project are not secret documents.
			if entry.Mode == filemode.Regular || entry.Mode == filemode.Executable {
				continue
			}
			components++
			if components > maxComponents {
				return nil, errors.New("selected components exceed limit")
			}
			ctree, e := budget.directory(ctx, ptree, entry.Name)
			if e != nil {
				return nil, e
			}
			configStart := len(docs)
			for _, env := range envs {
				if env == "base" && !explicitBase && len(docs) == configStart {
					continue
				}
				if err = ctx.Err(); err != nil {
					return nil, err
				}
				etree, e := budget.directory(ctx, ctree, env)
				if e != nil {
					return nil, e
				}
				if etree == nil {
					continue
				}
				file, e := budget.lookup(ctx, etree, "values.yaml")
				if errors.Is(e, object.ErrEntryNotFound) {
					continue
				}
				if e != nil || (file.Mode != filemode.Regular && file.Mode != filemode.Executable) {
					return nil, errors.New("secret document must be a regular file")
				}
				if len(docs) >= maxDocuments {
					return nil, errors.New("selected documents exceed limit")
				}
				blob, e := r.repo.BlobObject(file.Hash)
				if e != nil {
					return nil, errors.New("cannot read encrypted blob")
				}
				if blob.Size > r.cfg.Limits.DocumentBytes || blob.Size < 0 {
					return nil, errors.New("encrypted document exceeds limit")
				}
				total += blob.Size
				if total > r.cfg.Limits.SnapshotBytes {
					return nil, errors.New("encrypted candidate exceeds limit")
				}
				reader, e := blob.Reader()
				if e != nil {
					return nil, errors.New("cannot open encrypted blob")
				}
				data, readErr := io.ReadAll(io.LimitReader(reader, r.cfg.Limits.DocumentBytes+1))
				closeErr := reader.Close()
				if readErr != nil || closeErr != nil ||
					int64(len(data)) > r.cfg.Limits.DocumentBytes {
					return nil, errors.New("cannot read bounded encrypted document")
				}
				docs = append(
					docs,
					Document{
						Project:     project,
						Config:      entry.Name,
						Environment: env,
						Encrypted:   data,
					},
				)
			}
		}
	}
	return docs, nil
}
