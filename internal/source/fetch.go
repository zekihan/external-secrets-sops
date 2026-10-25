package source

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	nethttp "net/http"
	"os"
	"path/filepath"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// Fixed ceilings apply independently of selected-document limits, including
// blobs outside the selected scope. No setting can disable these bounds.
const (
	maxTransferBytes int64  = 64 << 20
	maxObjectBytes   int64  = 16 << 20
	maxDecodedBytes  int64  = 128 << 20
	maxPackObjects   uint32 = 16384
)

type boundedBody struct {
	io.ReadCloser
	remaining int64
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		return 0, errors.New("git transfer exceeds limit")
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	return n, err
}

type boundedTransport struct{ base nethttp.RoundTripper }

func (t boundedTransport) RoundTrip(req *nethttp.Request) (*nethttp.Response, error) {
	res, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	limit := maxTransferBytes
	if req.Method == nethttp.MethodGet {
		limit = 1 << 20
	}
	res.Body = &boundedBody{ReadCloser: res.Body, remaining: limit}
	return res, nil
}

type contextSeeker struct {
	context.Context
	*os.File
}

func (r contextSeeker) Read(p []byte) (int, error) {
	if err := r.Err(); err != nil {
		return 0, err
	}
	return r.File.Read(p)
}

func (r *Repository) fetchCandidate(
	ctx context.Context,
	repo *git.Repository,
	dir string,
) (string, error) {
	ep, err := transport.NewEndpoint(r.cfg.Git.URL)
	if err != nil {
		return "", err
	}
	if ep.Protocol != "https" && ep.Protocol != "file" {
		return "", errors.New("unsupported Git transport")
	}
	c, err := client.NewClient(ep)
	if err != nil {
		return "", err
	}
	if ep.Protocol == "https" {
		c = githttp.NewClient(
			&nethttp.Client{Transport: boundedTransport{base: nethttp.DefaultTransport}},
		)
	}
	session, err := c.NewUploadPackSession(ep, r.auth)
	if err != nil {
		return "", err
	}
	defer func() { _ = session.Close() }()
	refs, err := session.AdvertisedReferencesContext(ctx)
	if err != nil {
		return "", err
	}
	hash, ok := refs.References[plumbing.NewBranchReferenceName(r.cfg.Git.Branch).String()]
	if !ok || !refs.Capabilities.Supports(capability.Shallow) {
		return "", errors.New("branch or shallow fetch unavailable")
	}
	// No haves, tags or thin-pack: each candidate is independent of old history.
	req := packp.NewUploadPackRequest()
	req.Wants = []plumbing.Hash{hash}
	req.Depth = packp.DepthCommits(1)
	if err = req.Capabilities.Set(capability.Shallow); err != nil {
		return "", err
	}
	response, err := session.UploadPack(ctx, req)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Close() }()
	f, err := os.CreateTemp(dir, ".transfer-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	n, err := io.Copy(f, io.LimitReader(response, maxTransferBytes+1))
	if err != nil || n > maxTransferBytes {
		return "", errors.New("git pack transfer exceeds limit")
	}
	reader := contextSeeker{Context: ctx, File: f}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if err = checkPack(reader); err != nil {
		return "", err
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	parser, err := packfile.NewParserWithStorage(packfile.NewScanner(reader), repo.Storer)
	if err != nil {
		return "", err
	}
	if _, err = parser.Parse(); err != nil {
		return "", err
	}
	if _, err = repo.CommitObject(hash); err != nil {
		return "", err
	}
	if err = repo.Storer.SetShallow([]plumbing.Hash{hash}); err != nil {
		return "", err
	}
	// Loose-object writes cannot exceed the decoded quota except compression
	// overhead, bounded per file by the object count. Check the resulting cache.
	var total int64
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		total += info.Size()
		if total > maxDecodedBytes+maxTransferBytes+int64(maxPackObjects)*1024 {
			return errors.New("git cache exceeds limit")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hash.String(), ctx.Err()
}

// Preflight before the parser allocates object maps, inflates bases or applies
// deltas. Count both delta instructions and their declared result sizes.
func checkPack(r io.Reader) error {
	scanner := packfile.NewScanner(r)
	_, count, err := scanner.Header()
	if err != nil {
		return err
	}
	if count > maxPackObjects {
		return errors.New("git object count exceeds limit")
	}
	var total int64
	for i := uint32(0); i < count; i++ {
		h, e := scanner.NextObjectHeader()
		if e != nil {
			return e
		}
		if h.Length < 0 || h.Length > maxObjectBytes || h.Length > maxDecodedBytes-total {
			return errors.New("git object size exceeds limit")
		}
		total += h.Length
		prefix := &prefixSink{}
		// Pinned go-git bounds inflation at h.Length, including discarded bytes.
		written, _, e := scanner.NextObject(prefix)
		if e != nil {
			return e
		}
		if written != h.Length {
			return errors.New("git object inflated length mismatch")
		}
		if h.Type == plumbing.REFDeltaObject || h.Type == plumbing.OFSDeltaObject {
			_, n := binary.Uvarint(prefix.data)
			if n <= 0 {
				return errors.New("invalid Git delta")
			}
			size, n := binary.Uvarint(prefix.data[n:])
			if n <= 0 || size > uint64(maxObjectBytes) || size > uint64(maxDecodedBytes-total) {
				return errors.New("git delta size exceeds limit")
			}
			total += int64(size)
		}
	}
	_, err = scanner.Checksum()
	return err
}

type prefixSink struct{ data []byte }

func (s *prefixSink) Write(p []byte) (int, error) {
	n := len(p)
	if len(s.data) < 20 {
		s.data = append(s.data, p[:min(len(p), 20-len(s.data))]...)
	}
	return n, nil
}
