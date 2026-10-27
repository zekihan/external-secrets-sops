package source

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/zekihan/external-secrets-sops/internal/config"
)

// LocalDocuments reads committed blobs without fetching, creating a cache, or
// consulting the working tree. The caller validates the scope and limits.
func LocalDocuments(
	ctx context.Context,
	cfg config.Config,
	dir, revision string,
) ([]Document, error) {
	if !plumbing.IsHash(revision) {
		return nil, errors.New("revision must be a full immutable commit hash")
	}
	repo, err := git.PlainOpenWithOptions(dir, &git.PlainOpenOptions{EnableDotGitCommonDir: true})
	if err != nil {
		return nil, errors.New("cannot open local Git repository")
	}
	r := &Repository{cfg: cfg, repo: repo}
	return r.Documents(ctx, revision)
}

type dummyReader struct {
	root                *os.Root
	cfg                 config.Config
	entries, components int
	total               int64
	docs                []Document
}

// DummyDocuments deliberately accepts only explicitly marked synthetic input.
// Selected symlinks and special files are rejected before any document is read.
func DummyDocuments(ctx context.Context, cfg config.Config, dir string) ([]Document, error) {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return nil, errors.New("dummy root must be a regular directory")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, errors.New("cannot open dummy root")
	}
	defer func() { _ = root.Close() }()
	reader := dummyReader{root: root, cfg: cfg}
	marker, err := reader.file(".dummy-secrets", 64)
	if err != nil || string(marker) != "synthetic-only\n" {
		return nil, errors.New("dummy root requires a synthetic-only .dummy-secrets marker")
	}
	envs := slices.Clone(cfg.Scope.Environments)
	explicitBase := slices.Contains(envs, "base")
	if !explicitBase {
		envs = append(envs, "base")
	}
	for _, project := range cfg.Scope.Projects {
		exists, err := reader.directory(project)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		entries, err := reader.readDir(project)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			if !config.Component(entry.Name()) {
				return nil, errors.New("invalid config path")
			}
			if entry.Type().IsRegular() {
				continue
			}
			reader.components++
			if reader.components > maxComponents {
				return nil, errors.New("selected components exceed limit")
			}
			component := filepath.Join(project, entry.Name())
			if _, err = reader.directory(component); err != nil {
				return nil, err
			}
			start := len(reader.docs)
			for _, env := range envs {
				if env == "base" && !explicitBase && len(reader.docs) == start {
					continue
				}
				if err = reader.document(ctx, project, entry.Name(), env); err != nil {
					return nil, err
				}
			}
		}
	}
	return reader.docs, nil
}

func (r *dummyReader) directory(name string) (bool, error) {
	info, err := r.root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || !info.IsDir() {
		return false, errors.New("selected path is not a regular directory")
	}
	return true, nil
}

func (r *dummyReader) readDir(name string) ([]os.DirEntry, error) {
	f, err := r.root.Open(name)
	if err != nil {
		return nil, errors.New("cannot open selected directory")
	}
	defer func() { _ = f.Close() }()
	entries, err := f.ReadDir(maxTreeEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, errors.New("cannot read selected directory")
	}
	r.entries += len(entries)
	if len(entries) > maxTreeEntries || r.entries > maxTraversalEntries {
		return nil, errors.New("selected directory entries exceed limit")
	}
	slices.SortFunc(
		entries,
		func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) },
	)
	return entries, nil
}

func (r *dummyReader) file(name string, limit int64) ([]byte, error) {
	info, err := r.root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("secret document must be a regular file")
	}
	if info.Size() < 0 || info.Size() > limit {
		return nil, errors.New("dummy document exceeds limit")
	}
	f, err := r.root.Open(name)
	if err != nil {
		return nil, errors.New("cannot open dummy document")
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("cannot read bounded dummy document")
	}
	return data, nil
}

func (r *dummyReader) document(ctx context.Context, project, component, env string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name := filepath.Join(project, component, env)
	exists, err := r.directory(name)
	if err != nil || !exists {
		return err
	}
	filename := filepath.Join(name, "values.yaml")
	if _, err = r.root.Lstat(filename); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("cannot inspect dummy document")
	}
	if len(r.docs) >= maxDocuments {
		return errors.New("selected documents exceed limit")
	}
	data, err := r.file(
		filename,
		min(r.cfg.Limits.DocumentBytes, r.cfg.Limits.SnapshotBytes-r.total),
	)
	if err != nil {
		return err
	}
	r.total += int64(len(data))
	r.docs = append(
		r.docs,
		Document{Project: project, Config: component, Environment: env, Encrypted: data},
	)
	return nil
}
