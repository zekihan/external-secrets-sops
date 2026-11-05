package offline

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zekihan/external-secrets-sops/internal/config"
	"github.com/zekihan/external-secrets-sops/internal/source"
)

func TestMemoryDocuments(t *testing.T) {
	cfg := config.Defaults()
	cfg.Scope = config.Scope{Projects: []string{"apps"}, Environments: []string{"prod"}}
	cfg.Limits.DocumentBytes = 32
	cfg.Limits.SnapshotBytes = 64
	path := `apps/demo/prod/values.yaml`
	tests := []struct {
		name, input, wantErr string
		snapshot             int64
		want                 []source.Document
	}{
		{
			name:  "valid",
			input: `{"apps/demo/prod/values.yaml":"a: b\n","apps/demo/base/values.yaml":"\\uD83D\\uDE00"}`,
			want: []source.Document{
				{Project: "apps", Config: "demo", Environment: "prod", Encrypted: []byte("a: b\n")},
				{
					Project:     "apps",
					Config:      "demo",
					Environment: "base",
					Encrypted:   []byte(`\uD83D\uDE00`),
				},
			},
		},
		{name: "non-object", input: `[]`, wantErr: "invalid memory documents"},
		{
			name:    "duplicate",
			input:   fmt.Sprintf(`{"%s":"a","%s":"b"}`, path, path),
			wantErr: "invalid memory documents",
		},
		{
			name:    "too much input",
			input:   `{}` + strings.Repeat(" ", (4<<20)+385),
			wantErr: "invalid memory documents",
		},
		{
			name:    "invalid utf8",
			input:   `{"` + path + `":"` + string([]byte{0xff}) + `"}`,
			wantErr: "invalid memory documents",
		},
		{
			name:    "lone high surrogate",
			input:   `{"` + path + `":"\uD800"}`,
			wantErr: "invalid memory documents",
		},
		{
			name:    "lone low surrogate",
			input:   `{"` + path + `":"\uDC00"}`,
			wantErr: "invalid memory documents",
		},
		{
			name:    "invalid surrogate pair",
			input:   `{"` + path + `":"\uD800\u0041"}`,
			wantErr: "invalid memory documents",
		},
		{
			name:  "valid surrogate pair",
			input: `{"` + path + `":"\uD83D\uDE00"}`,
			want: []source.Document{
				{Project: "apps", Config: "demo", Environment: "prod", Encrypted: []byte("😀")},
			},
		},
		{
			name:     "overflow",
			input:    `{}`,
			snapshot: math.MaxInt64,
			wantErr:  "memory input limit too large",
		},
		{
			name:    "bad path",
			input:   `{"apps/../prod/values.yaml":"a"}`,
			wantErr: "invalid memory documents",
		},
		{
			name:    "short path",
			input:   `{"apps/values.yaml":"a"}`,
			wantErr: "invalid memory documents",
		},
		{
			name:    "outside scope",
			input:   `{"other/demo/prod/values.yaml":"a"}`,
			wantErr: "memory document outside selected scope",
		},
		{name: "null", input: `{"` + path + `":null}`, wantErr: "invalid memory documents"},
		{name: "non-string", input: `{"` + path + `":3}`, wantErr: "invalid memory documents"},
		{
			name:    "document limit",
			input:   `{"` + path + `":"` + strings.Repeat("x", 33) + `"}`,
			wantErr: "memory document bytes exceed limit",
		},
		{
			name: "snapshot limit",
			input: `{"apps/a/prod/values.yaml":"` + strings.Repeat(
				"x",
				32,
			) + `","apps/b/prod/values.yaml":"` + strings.Repeat(
				"x",
				32,
			) + `","apps/c/prod/values.yaml":"x"}`,
			wantErr: "memory document bytes exceed limit",
		},
		{name: "truncated", input: `{"` + path + `":"a"`, wantErr: "invalid memory documents"},
		{name: "trailing", input: `{} {}`, wantErr: "invalid memory documents"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := cfg
			if tt.snapshot != 0 {
				c.Limits.SnapshotBytes = tt.snapshot
			}
			got, err := memoryDocuments(t.Context(), c, strings.NewReader(tt.input))
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr || got != nil {
					t.Fatalf("got %v, %v; want %q", got, err, tt.wantErr)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestMemoryDocumentsCancellation(t *testing.T) {
	for _, kind := range []string{"file", "readcloser"} {
		t.Run(kind, func(t *testing.T) {
			var reader io.ReadCloser
			var writer io.Closer
			if kind == "file" {
				r, w, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				reader, writer = r, w
			} else {
				r, w := io.Pipe()
				reader, writer = r, w
			}
			defer func() { _ = reader.Close(); _ = writer.Close() }()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := memoryDocuments(ctx, config.Defaults(), reader); done <- err }()
			cancel()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("canceled input accepted")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("cancellation did not interrupt read")
			}
			if kind == "readcloser" {
				if _, err := reader.Read(make([]byte, 1)); err == nil {
					t.Fatal("reader remained open")
				}
			}
		})
	}
}
