package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
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
	Revision    string    `json:"revision"`
	Fingerprint string    `json:"fingerprint"`
	ValidatedAt time.Time `json:"validatedAt"`
}
type Repository struct {
	cfg  config.Config
	dir  string
	auth transport.AuthMethod
	repo *git.Repository
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

func (r *Repository) open() error {
	if r.repo != nil {
		return nil
	}
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		return errors.New("cannot create Git cache")
	}
	repo, err := git.PlainOpen(r.dir)
	if errors.Is(err, git.ErrRepositoryNotExists) {
		repo, err = git.PlainInit(r.dir, true)
	}
	if err != nil {
		return errors.New("cannot open Git cache")
	}
	remote, err := repo.Remote("origin")
	if errors.Is(err, git.ErrRemoteNotFound) {
		_, err = repo.CreateRemote(
			&gitconfig.RemoteConfig{Name: "origin", URLs: []string{r.cfg.Git.URL}},
		)
	} else if err == nil && (len(remote.Config().URLs) != 1 || remote.Config().URLs[0] != r.cfg.Git.URL) {
		return errors.New("git cache remote mismatch")
	}
	if err != nil {
		return errors.New("cannot configure Git cache")
	}
	r.repo = repo
	return nil
}

func (r *Repository) Fetch(ctx context.Context) (string, error) {
	if err := r.open(); err != nil {
		// Preserve an unreadable cache for diagnosis and recover into a fresh directory.
		if renameErr := os.Rename(
			r.dir,
			r.dir+".invalid-"+time.Now().UTC().Format("20060102T150405.000000000"),
		); renameErr != nil {
			return "", err
		}
		r.repo = nil
		if err = r.open(); err != nil {
			return "", err
		}
	}
	ref := plumbing.NewBranchReferenceName(r.cfg.Git.Branch)
	spec := gitconfig.RefSpec("+" + ref.String() + ":refs/remotes/origin/selected")
	err := r.repo.FetchContext(
		ctx,
		&git.FetchOptions{
			RemoteName: "origin",
			RefSpecs:   []gitconfig.RefSpec{spec},
			Auth:       r.auth,
			Force:      true,
		},
	)
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return "", errors.New("git fetch failed")
	}
	selected, err := r.repo.Reference("refs/remotes/origin/selected", true)
	if err != nil {
		return "", errors.New("configured branch is unavailable")
	}
	return selected.Hash().String(), nil
}

func (r *Repository) Cached() (Marker, error) {
	var m Marker
	if err := r.open(); err != nil {
		return m, err
	}
	b, err := os.ReadFile(filepath.Join(r.dir, "validated.json"))
	if err != nil {
		return m, errors.New("validated cache unavailable")
	}
	if json.Unmarshal(b, &m) != nil || m.Fingerprint != r.cfg.Fingerprint() ||
		!plumbing.IsHash(m.Revision) ||
		m.ValidatedAt.IsZero() ||
		m.ValidatedAt.After(time.Now()) {
		return Marker{}, errors.New("validated cache metadata invalid")
	}
	return m, nil
}

func (r *Repository) Save(revision string, at time.Time) error {
	if r.repo == nil {
		return errors.New("git cache is not open")
	}
	if err := r.repo.Storer.SetReference(
		plumbing.NewHashReference("refs/ess/validated", plumbing.NewHash(revision)),
	); err != nil {
		return errors.New("cannot retain validated Git revision")
	}
	b, err := json.Marshal(
		Marker{Revision: revision, Fingerprint: r.cfg.Fingerprint(), ValidatedAt: at},
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
