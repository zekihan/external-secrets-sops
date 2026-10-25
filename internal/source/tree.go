package source

import (
	"bufio"
	"context"
	"errors"
	"io"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// These hard ceilings bound selection independently of document byte limits.
// Repeated references count again: a small Git DAG can represent a large scope.
const (
	maxTreeBytes        = 1 << 20
	maxTraversalBytes   = 16 << 20
	maxTreeEntries      = 16384
	maxTraversalEntries = 65536
	maxTreeLoads        = 16384
	maxTraversalWork    = 1 << 25
	maxComponents       = 4096
	maxDocuments        = 4096
)

type treeBudget struct {
	r                    *Repository
	bytes                int64
	entries, loads, work int
}

func (b *treeBudget) load(ctx context.Context, hash plumbing.Hash) (*object.Tree, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.loads++
	if b.loads > maxTreeLoads {
		return nil, errors.New("selected tree traversal exceeds limit")
	}
	// Check the stored size before EncodedObject can materialize the object.
	size, err := b.r.repo.Storer.EncodedObjectSize(hash)
	if err != nil {
		return nil, errors.New("cannot read selected tree size")
	}
	if size < 0 || size > maxTreeBytes || size > maxTraversalBytes-b.bytes {
		return nil, errors.New("selected tree bytes exceed limit")
	}
	b.bytes += size
	encoded, err := b.r.repo.Storer.EncodedObject(plumbing.TreeObject, hash)
	if err != nil {
		return nil, errors.New("cannot read selected tree")
	}
	if err = b.preflight(ctx, encoded); err != nil {
		return nil, err
	}
	tree, err := object.DecodeTree(b.r.repo.Storer, encoded)
	if err != nil {
		return nil, errors.New("cannot decode selected tree")
	}
	return tree, nil
}

// Count entries before go-git's decoder allocates its entry slice and names.
// ReadSlice bounds even a malformed entry header to the reader's 4 KiB buffer.
func (b *treeBudget) preflight(ctx context.Context, encoded plumbing.EncodedObject) error {
	r, err := encoded.Reader()
	if err != nil {
		return errors.New("cannot open selected tree")
	}
	defer func() { _ = r.Close() }()
	reader := bufio.NewReader(io.LimitReader(r, maxTreeBytes+1))
	for count := 0; ; count++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		header, readErr := reader.ReadSlice(0)
		if errors.Is(readErr, io.EOF) && len(header) == 0 {
			return nil
		}
		if readErr != nil {
			return errors.New("invalid or oversized selected tree entry")
		}
		if count >= maxTreeEntries || b.entries >= maxTraversalEntries {
			return errors.New("selected tree entries exceed limit")
		}
		b.entries++
		var hash plumbing.Hash
		if _, err = io.ReadFull(reader, hash[:]); err != nil {
			return errors.New("invalid selected tree entry")
		}
	}
}

func (b *treeBudget) lookup(
	ctx context.Context,
	tree *object.Tree,
	name string,
) (*object.TreeEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Charge worst-case lookup work, including unsorted Git trees.
	cost := len(tree.Entries) + 1
	if cost > maxTraversalWork-b.work {
		return nil, errors.New("selected tree lookup work exceeds limit")
	}
	b.work += cost
	return tree.FindEntry(name)
}
