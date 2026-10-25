package source

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/zekihan/external-secrets-sops/internal/testutil"
)

func packFixture(typ byte, declared uint64, body []byte) []byte {
	var b bytes.Buffer
	b.WriteString("PACK")
	_ = binary.Write(&b, binary.BigEndian, uint32(2))
	_ = binary.Write(&b, binary.BigEndian, uint32(1))
	first := byte(declared&15) | (typ << 4)
	declared >>= 4
	if declared > 0 {
		first |= 128
	}
	b.WriteByte(first)
	for declared > 0 {
		v := byte(declared & 127)
		declared >>= 7
		if declared > 0 {
			v |= 128
		}
		b.WriteByte(v)
	}
	if typ == 7 {
		b.Write(make([]byte, 20))
	}
	z := zlib.NewWriter(&b)
	_, _ = z.Write(body)
	_ = z.Close()
	sum := sha1.Sum(b.Bytes())
	b.Write(sum[:])
	return b.Bytes()
}
func TestPackPreflight(t *testing.T) {
	delta := binary.AppendUvarint(nil, 1)
	delta = binary.AppendUvarint(delta, uint64(maxObjectBytes+1))
	tooMany := append(
		[]byte("PACK\x00\x00\x00\x02"),
		binary.BigEndian.AppendUint32(nil, maxPackObjects+1)...)
	for _, tc := range []struct {
		name  string
		data  []byte
		valid bool
	}{
		{"supported", packFixture(3, 3, []byte("abc")), true},
		{"large object header", packFixture(3, uint64(maxObjectBytes+1), nil), false},
		{"large delta result", packFixture(7, uint64(len(delta)), delta), false},
		{"malformed delta", packFixture(7, 1, []byte{128}), false},
		{"inflated header mismatch", packFixture(3, 1, []byte("abc")), false},
		{"object count", tooMany, false},
		{"checksum", packFixture(3, 1, []byte("a"))[:20], false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkPack(bytes.NewReader(tc.data))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, err=%v", tc.valid, err)
			}
		})
	}
}
func cacheBytes(t *testing.T, dir string) int64 {
	t.Helper()
	var n int64
	err := filepath.WalkDir(dir, func(_ string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type().IsRegular() {
			s, e := d.Info()
			if e != nil {
				return e
			}
			n += s.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}
func TestShallowCandidateAndStorageLifecycle(t *testing.T) {
	cfg := testutil.Config(t)
	remote := testutil.InitRepo(t, cfg.Git.URL, "main")
	file := "generated/project/app/dev/values.yaml"
	// An oversized historical blob must not be transferred at depth one.
	old := testutil.Commit(
		t,
		remote,
		map[string][]byte{file: bytes.Repeat([]byte("x"), int(maxObjectBytes+1))},
	)
	good := testutil.Commit(t, remote, map[string][]byte{file: []byte("encrypted")})
	r, _ := New(cfg, "")
	revision, err := r.Fetch(t.Context())
	if err != nil || revision != good {
		t.Fatalf("fetch=%s %v", revision, err)
	}
	if _, err = r.repo.CommitObject(plumbing.NewHash(old)); err == nil {
		t.Fatal("historical commit retained")
	}
	if err = r.Save(good, time.Now()); err != nil {
		t.Fatal(err)
	}
	markerBefore, err := os.ReadFile(filepath.Join(r.dir, "validated.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Unselected data must hit the fetch quota before document selection.
	testutil.Commit(
		t,
		remote,
		map[string][]byte{"unselected.bin": bytes.Repeat([]byte("x"), int(maxObjectBytes+1))},
	)
	if _, err = r.Fetch(t.Context()); err == nil {
		t.Fatal("oversized unselected blob accepted")
	}
	markerAfter, _ := os.ReadFile(filepath.Join(r.dir, "validated.json"))
	if !bytes.Equal(markerBefore, markerAfter) {
		t.Fatal("failed fetch changed validated marker")
	}
	restored, _ := New(cfg, "")
	m, err := restored.Cached()
	if err != nil || m.Revision != good {
		t.Fatalf("restore=%+v %v", m, err)
	}
	if docs, err := restored.Documents(t.Context(), good); err != nil || len(docs) != 1 {
		t.Fatalf("restore documents=%d %v", len(docs), err)
	}
	// Repeated forced divergent tips replace scratch slots instead of appending packs.
	var peak int64
	for i := 0; i < 8; i++ {
		if err = remote.Storer.SetReference(
			plumbing.NewHashReference(
				plumbing.NewBranchReferenceName("main"),
				plumbing.NewHash(good),
			),
		); err != nil {
			t.Fatal(err)
		}
		wt, _ := remote.Worktree()
		if err = wt.Reset(
			&git.ResetOptions{Mode: git.HardReset, Commit: plumbing.NewHash(good)},
		); err != nil {
			t.Fatal(err)
		}
		tip := testutil.Commit(t, remote, map[string][]byte{file: {byte(i)}})
		if got, e := r.Fetch(t.Context()); e != nil || got != tip {
			t.Fatalf("divergent fetch=%s %v", got, e)
		}
		if err = r.Save(tip, time.Now()); err != nil {
			t.Fatal(err)
		}
		n := cacheBytes(t, r.dir)
		peak = max(peak, n)
		if n > 64<<10 {
			t.Fatalf("tiny divergent tips accumulated %d bytes", n)
		}
	}
	t.Logf("peak cache bytes for divergent tips: %d", peak)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = r.Fetch(ctx); err == nil {
		t.Fatal("canceled fetch succeeded")
	}
	final, _ := New(cfg, "")
	if _, err = final.Cached(); err != nil {
		t.Fatal("cancellation damaged cache", err)
	}
	if err = r.Save(good, time.Now()); err == nil {
		t.Fatal("saved revision without a fetched candidate")
	}
}
func TestHTTPResponseBudget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.Copy(w, io.LimitReader(zeroReader{}, (1<<20)+1))
	}))
	defer server.Close()
	client := &http.Client{Transport: boundedTransport{base: http.DefaultTransport}}
	res, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	n, err := io.Copy(io.Discard, res.Body)
	if err == nil || n != 1<<20 {
		t.Fatalf("advertisement read=%d %v", n, err)
	}
	body := &boundedBody{ReadCloser: io.NopCloser(bytes.NewReader([]byte("abcd"))), remaining: 3}
	data, err := io.ReadAll(body)
	if err == nil || string(data) != "abc" {
		t.Fatalf("transfer=%q %v", data, err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestAggregateDeltaBudget(t *testing.T) {
	body := binary.AppendUvarint(nil, 1)
	body = binary.AppendUvarint(body, uint64(maxObjectBytes))
	one := packFixture(7, uint64(len(body)), body)
	var b bytes.Buffer
	b.Write(one[:12])
	binary.BigEndian.PutUint32(b.Bytes()[8:12], 9)
	for range 9 {
		b.Write(one[12 : len(one)-sha1.Size])
	}
	sum := sha1.Sum(b.Bytes())
	b.Write(sum[:])
	if err := checkPack(bytes.NewReader(b.Bytes())); err == nil {
		t.Fatal("aggregate delta budget ignored")
	}
}
func TestCorruptMarkerPreservesSlots(t *testing.T) {
	cfg := testutil.Config(t)
	r, _ := New(cfg, "")
	slot := filepath.Join(r.dir, "slot-0")
	if err := os.MkdirAll(slot, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(slot, "preserve")
	if err := os.WriteFile(sentinel, []byte("encrypted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(r.dir, "validated.json"),
		[]byte(`{"generation":"../../outside"}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Fetch(t.Context()); err == nil {
		t.Fatal("invalid marker accepted")
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatal("slot changed on invalid marker", err)
	}
}

func TestLegacyCacheRecovery(t *testing.T) {
	cfg := testutil.Config(t)
	remote := testutil.InitRepo(t, cfg.Git.URL, "main")
	revision := testutil.Commit(
		t,
		remote,
		map[string][]byte{"generated/project/app/dev/values.yaml": []byte("encrypted")},
	)
	r, _ := New(cfg, "")
	if _, err := git.PlainClone(r.dir, true, &git.CloneOptions{URL: cfg.Git.URL}); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(
		Marker{Revision: revision, Fingerprint: cfg.Fingerprint(), ValidatedAt: time.Now()},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(r.dir, "validated.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Cached(); err != nil {
		t.Fatal(err)
	}
	if docs, e := r.Documents(t.Context(), revision); e != nil || len(docs) != 1 {
		t.Fatalf("legacy recovery=%d %v", len(docs), e)
	}
	if _, err = r.Fetch(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = r.Save(revision, time.Now()); err != nil {
		t.Fatal(err)
	}
	fresh, _ := New(cfg, "")
	marker, err := fresh.Cached()
	if err != nil || marker.Generation == "" {
		t.Fatalf("upgrade=%+v %v", marker, err)
	}
}

func TestSupportedDeltaPack(t *testing.T) {
	base := []byte("hello")
	baseHash := plumbing.ComputeHash(plumbing.BlobObject, base)
	first := packFixture(3, uint64(len(base)), base)
	// Copy all five base bytes and append one literal byte.
	instructions := []byte{5, 6, 0x90, 5, 1, '!'}
	second := packFixture(7, uint64(len(instructions)), instructions)
	copy(second[13:33], baseHash[:])
	var b bytes.Buffer
	b.Write(first[:12])
	binary.BigEndian.PutUint32(b.Bytes()[8:12], 2)
	b.Write(first[12 : len(first)-sha1.Size])
	b.Write(second[12 : len(second)-sha1.Size])
	sum := sha1.Sum(b.Bytes())
	b.Write(sum[:])
	if err := checkPack(bytes.NewReader(b.Bytes())); err != nil {
		t.Fatal(err)
	}
	storage := memory.NewStorage()
	parser, err := packfile.NewParserWithStorage(
		packfile.NewScanner(bytes.NewReader(b.Bytes())),
		storage,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = parser.Parse(); err != nil {
		t.Fatal(err)
	}
	expected := plumbing.ComputeHash(plumbing.BlobObject, []byte("hello!"))
	if _, err = storage.EncodedObject(plumbing.BlobObject, expected); err != nil {
		t.Fatal("delta content integrity", err)
	}
}
