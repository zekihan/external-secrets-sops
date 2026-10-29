package offline

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/zekihan/external-secrets-sops/internal/config"
	"github.com/zekihan/external-secrets-sops/internal/source"
)

// memoryDocuments accepts an operator's candidate without persisting plaintext.
func memoryDocuments(
	ctx context.Context,
	cfg config.Config,
	input io.Reader,
) ([]source.Document, error) {
	if file, ok := input.(*os.File); ok {
		stream, err := cancellableFile(file)
		if err != nil {
			return nil, errors.New("cannot prepare memory input")
		}
		defer func() { _ = stream.Close() }()
		input = stream
	}
	if closer, ok := input.(io.ReadCloser); ok {
		closed := make(chan struct{})
		stop := context.AfterFunc(ctx, func() {
			_ = closer.Close()
			close(closed)
		})
		defer func() {
			if !stop() {
				<-closed
			}
		}()
	}
	invalid := errors.New("invalid memory documents")
	// JSON escaping can multiply document bytes by six. Bound path overhead too.
	if cfg.Limits.SnapshotBytes > (math.MaxInt64-(4<<20))/6 {
		return nil, errors.New("memory input limit too large")
	}
	limited := &io.LimitedReader{R: input, N: cfg.Limits.SnapshotBytes*6 + (4 << 20)}
	decoder := json.NewDecoder(limited)
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, invalid
	}
	seen := make(map[string]bool)
	var docs []source.Document
	var total int64
	for decoder.More() {
		if err = ctx.Err(); err != nil {
			return nil, errors.New("memory input canceled")
		}
		if len(docs) >= 4096 {
			return nil, errors.New("memory document count exceeds limit")
		}
		token, err = decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok || seen[name] {
			return nil, invalid
		}
		seen[name] = true
		parts := strings.Split(name, "/")
		if len(parts) != 4 || parts[3] != "values.yaml" {
			return nil, invalid
		}
		for _, part := range parts[:3] {
			if !config.Component(part) {
				return nil, invalid
			}
		}
		if !slices.Contains(cfg.Scope.Projects, parts[0]) ||
			(parts[2] != "base" && !slices.Contains(cfg.Scope.Environments, parts[2])) {
			return nil, errors.New("memory document outside selected scope")
		}
		var value *memoryDocument
		if err = decoder.Decode(&value); err != nil || value == nil {
			return nil, invalid
		}
		document := string(*value)
		total += int64(len(document))
		if int64(len(document)) > cfg.Limits.DocumentBytes || total > cfg.Limits.SnapshotBytes {
			return nil, errors.New("memory document bytes exceed limit")
		}
		docs = append(
			docs,
			source.Document{
				Project:     parts[0],
				Config:      parts[1],
				Environment: parts[2],
				Encrypted:   []byte(document),
			},
		)
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, invalid
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) || limited.N == 0 {
		return nil, invalid
	}
	return docs, nil
}

// memoryDocument rejects Unicode that encoding/json would silently replace with
// U+FFFD. Repairing a document at this boundary would change its secret values.
type memoryDocument string

func (d *memoryDocument) UnmarshalJSON(raw []byte) error {
	invalid := errors.New("invalid memory documents")
	if !utf8.Valid(raw) {
		return invalid
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return invalid
	}
	// Unmarshal has checked JSON syntax, including the length and hex digits of
	// each escape. Check the surrogate pairing it deliberately tolerates.
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if raw[i] != 'u' {
			continue
		}
		unit, _ := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		i += 4
		switch {
		case unit >= 0xdc00 && unit <= 0xdfff:
			return invalid
		case unit >= 0xd800 && unit <= 0xdbff:
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return invalid
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return invalid
			}
			i += 6
		}
	}
	*d = memoryDocument(value)
	return nil
}
