package source

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/zekihan/external-secrets-sops/internal/testutil"
)

func syntheticRepository(t *testing.T) *Repository {
	t.Helper()
	repo, err := git.Init(memory.NewStorage(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return &Repository{cfg: testutil.Config(t), repo: repo}
}

func storeTree(t *testing.T, r *Repository, entries []object.TreeEntry) plumbing.Hash {
	t.Helper()
	encoded := r.repo.Storer.NewEncodedObject()
	tree := object.Tree{Entries: entries}
	if err := tree.Encode(encoded); err != nil {
		t.Fatal(err)
	}
	hash, err := r.repo.Storer.SetEncodedObject(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func storeCommit(t *testing.T, r *Repository, tree plumbing.Hash) string {
	t.Helper()
	encoded := r.repo.Storer.NewEncodedObject()
	commit := object.Commit{TreeHash: tree}
	if err := commit.Encode(encoded); err != nil {
		t.Fatal(err)
	}
	hash, err := r.repo.Storer.SetEncodedObject(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return hash.String()
}

func selectedCommit(t *testing.T, r *Repository, project plumbing.Hash) string {
	t.Helper()
	generated := storeTree(
		t,
		r,
		[]object.TreeEntry{{Name: "project", Mode: filemode.Dir, Hash: project}},
	)
	root := storeTree(
		t,
		r,
		[]object.TreeEntry{{Name: "generated", Mode: filemode.Dir, Hash: generated}},
	)
	return storeCommit(t, r, root)
}

func TestDocumentsRejectWideTreesBeforeSelection(t *testing.T) {
	for _, location := range []string{"root", "project", "component", "environment"} {
		t.Run(location, func(t *testing.T) {
			r := syntheticRepository(t)
			entries := make([]object.TreeEntry, maxTreeEntries+1)
			for i := range entries {
				// No values.yaml: document byte limits cannot stop this tree.
				entries[i] = object.TreeEntry{
					Name: fmt.Sprintf("ignored%05d", i),
					Mode: filemode.Regular,
				}
			}
			wide := storeTree(t, r, entries)
			var revision string
			switch location {
			case "root":
				revision = storeCommit(t, r, wide)
			case "project":
				revision = selectedCommit(t, r, wide)
			case "component":
				project := storeTree(
					t,
					r,
					[]object.TreeEntry{{Name: "app", Mode: filemode.Dir, Hash: wide}},
				)
				revision = selectedCommit(t, r, project)
			case "environment":
				component := storeTree(
					t,
					r,
					[]object.TreeEntry{{Name: "dev", Mode: filemode.Dir, Hash: wide}},
				)
				project := storeTree(
					t,
					r,
					[]object.TreeEntry{{Name: "app", Mode: filemode.Dir, Hash: component}},
				)
				revision = selectedCommit(t, r, project)
			}
			docs, err := r.Documents(t.Context(), revision)
			if err == nil || !strings.Contains(err.Error(), "entries exceed limit") || docs != nil {
				t.Fatalf("documents = %v, %v", docs, err)
			}
		})
	}
}

func TestDocumentsBoundComponentsAndEmptyDocuments(t *testing.T) {
	for _, withDocuments := range []bool{false, true} {
		t.Run(fmt.Sprint(withDocuments), func(t *testing.T) {
			r := syntheticRepository(t)
			empty := storeTree(t, r, nil)
			component := empty
			count := maxComponents + 1
			want := "components exceed limit"
			if withDocuments {
				encoded := r.repo.Storer.NewEncodedObject()
				encoded.SetType(plumbing.BlobObject)
				blob, err := r.repo.Storer.SetEncodedObject(encoded)
				if err != nil {
					t.Fatal(err)
				}
				env := storeTree(
					t,
					r,
					[]object.TreeEntry{{Name: "values.yaml", Mode: filemode.Regular, Hash: blob}},
				)
				component = storeTree(t, r, []object.TreeEntry{
					{Name: "base", Mode: filemode.Dir, Hash: env},
					{Name: "dev", Mode: filemode.Dir, Hash: env},
				})
				count = maxDocuments/2 + 1
				want = "documents exceed limit"
			}
			entries := make([]object.TreeEntry, count)
			for i := range entries {
				entries[i] = object.TreeEntry{
					Name: fmt.Sprintf("app%05d", i),
					Mode: filemode.Dir,
					Hash: component,
				}
			}
			accepted := selectedCommit(t, r, storeTree(t, r, entries[:count-1]))
			docs, err := r.Documents(t.Context(), accepted)
			wantDocs := 0
			if withDocuments {
				wantDocs = maxDocuments
			}
			if err != nil || len(docs) != wantDocs {
				t.Fatalf("at limit: documents = %d, %v", len(docs), err)
			}
			revision := selectedCommit(t, r, storeTree(t, r, entries))
			docs, err = r.Documents(t.Context(), revision)
			if err == nil || !strings.Contains(err.Error(), want) || docs != nil {
				t.Fatalf("documents = %v, %v", docs, err)
			}
		})
	}
}

func TestTreeBudgetBoundaries(t *testing.T) {
	r := syntheticRepository(t)
	entries := make([]object.TreeEntry, maxTreeEntries)
	for i := range entries {
		entries[i] = object.TreeEntry{Name: fmt.Sprintf("file%05d", i), Mode: filemode.Regular}
	}
	hash := storeTree(t, r, entries)
	budget := treeBudget{r: r}
	// Repeated DAG references must consume the aggregate entry budget.
	for range maxTraversalEntries / maxTreeEntries {
		if _, err := budget.load(t.Context(), hash); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := budget.load(t.Context(), hash); err == nil {
		t.Fatal("aggregate entry limit ignored")
	}
	empty := storeTree(t, r, nil)
	budget = treeBudget{r: r, loads: maxTreeLoads - 1}
	if _, err := budget.load(t.Context(), empty); err != nil {
		t.Fatal(err)
	}
	if _, err := budget.load(t.Context(), empty); err == nil {
		t.Fatal("empty trees bypassed load limit")
	}
	budget = treeBudget{r: r, work: maxTraversalWork - 1}
	tree := &object.Tree{}
	if _, err := budget.lookup(
		t.Context(),
		tree,
		"missing",
	); !errors.Is(
		err,
		object.ErrEntryNotFound,
	) {
		t.Fatal(err)
	}
	if _, err := budget.lookup(
		t.Context(),
		tree,
		"missing",
	); err == nil ||
		errors.Is(err, object.ErrEntryNotFound) {
		t.Fatal("lookup work limit ignored")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := budget.load(ctx, empty); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
}

func TestTreeBudgetBytesAndMalformedHeaders(t *testing.T) {
	r := syntheticRepository(t)
	oversized := storeTree(t, r, []object.TreeEntry{{
		Name: strings.Repeat("x", maxTreeBytes), Mode: filemode.Regular,
	}})
	budget := treeBudget{r: r}
	if _, err := budget.load(
		t.Context(),
		oversized,
	); err == nil ||
		!strings.Contains(err.Error(), "bytes exceed limit") {
		t.Fatalf("oversized tree = %v", err)
	}
	small := storeTree(t, r, []object.TreeEntry{{Name: "a", Mode: filemode.Regular}})
	budget.bytes = maxTraversalBytes
	if _, err := budget.load(t.Context(), small); err == nil {
		t.Fatal("aggregate byte limit ignored")
	}
	longName := storeTree(
		t,
		r,
		[]object.TreeEntry{{Name: strings.Repeat("x", 4096), Mode: filemode.Regular}},
	)
	budget = treeBudget{r: r}
	if _, err := budget.load(t.Context(), longName); err == nil {
		t.Fatal("oversized header accepted")
	}
}
