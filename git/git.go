package git

import (
	"errors"
	"fmt"
	"os"
	"path"
	"sync/atomic"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"

	log "github.com/sirupsen/logrus"

	"github.com/zekihan/external-secrets-sops/config"
)

var (
	quit            = make(chan bool)
	loopRunning     = atomic.Bool{}
	errRepoURLEmpty = errors.New("repoUrl is empty")
)

func Clone() error {
	repoPath := config.GetConfig().GitConfig.RepoPath

	repoURL := config.GetConfig().GitConfig.RepoURL
	if repoURL == "" {
		return errRepoURLEmpty
	}

	_, err := os.Stat(path.Join(repoPath, ".git"))
	if err == nil {
		return cloneCheckIfExists(repoURL, repoPath)
	}

	return cloneIfNotExists(repoURL, repoPath)
}

func cloneCheckIfExists(repoURL, repoPath string) error {
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("failed to open git repository: %w", err)
	}

	remote, err := r.Remote("origin")
	if err != nil {
		return fmt.Errorf("failed to get remote 'origin': %w", err)
	}

	urls := remote.Config().URLs
	if len(urls) > 0 && urls[0] == repoURL {
		return nil
	}

	err = os.RemoveAll(repoPath)
	if err != nil {
		return fmt.Errorf("failed to remove existing repository at %s: %w", repoPath, err)
	}

	return nil
}

func cloneIfNotExists(repoURL, repoPath string) error {
	cloneOptions := &git.CloneOptions{
		URL:      repoURL,
		Progress: log.StandardLogger().Writer(),
	}
	if config.GetConfig().GitConfig.AccessToken != "" {
		cloneOptions.Auth = &http.BasicAuth{
			Username: "X", // anything except an empty string
			Password: config.GetConfig().GitConfig.AccessToken,
		}
	}

	_, err := git.PlainClone(repoPath, false, cloneOptions)
	if err != nil {
		return fmt.Errorf("failed to clone %s repository: %w", repoURL, err)
	}

	return nil
}

func Pull() error {
	repoPath := config.GetConfig().GitConfig.RepoPath

	_, err := os.Stat(path.Join(repoPath, ".git"))
	if os.IsNotExist(err) {
		cloneErr := Clone()
		if cloneErr != nil {
			return fmt.Errorf("git clone failed: %w", cloneErr)
		}
	}

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("failed to open git repository: %w", err)
	}

	remote, err := repo.Remote("origin")
	if err != nil {
		return fmt.Errorf("failed to get remote 'origin': %w", err)
	}

	fetchOptions := &git.FetchOptions{}
	if config.GetConfig().GitConfig.AccessToken != "" {
		fetchOptions.Auth = &http.BasicAuth{
			Username: "X", // anything except an empty string
			Password: config.GetConfig().GitConfig.AccessToken,
		}
	}

	err = remote.Fetch(fetchOptions)
	if err != nil {
		if errors.Is(err, git.NoErrAlreadyUpToDate) {
			commit, commitErr := GetLastCommit()
			if commitErr != nil {
				return nil //nolint:nilerr // ignore error if we can't get the last commit
			}

			log.Info("already up to date. last commit: ", commit.String())

			return nil
		}

		return fmt.Errorf("failed to fetch from remote: %w", err)
	}

	newRef, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", "main"), false)
	if err != nil {
		return fmt.Errorf("failed to get reference for origin/main: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	err = wt.Reset(&git.ResetOptions{
		Commit: newRef.Hash(),
		Mode:   git.HardReset,
	})
	if err != nil {
		return fmt.Errorf("failed to reset worktree: %w", err)
	}

	commit, err := repo.CommitObject(newRef.Hash())
	if err != nil {
		return fmt.Errorf("failed to get commit object: %w", err)
	}

	log.Info("new commit: ", commit.String())

	return nil
}

func IsRepoReady() bool {
	repoPath := path.Join(config.GetConfig().GitConfig.RepoPath, ".git")

	return isDirExists(repoPath)
}

func isDirExists(dirPath string) bool {
	_, err := os.Stat(dirPath)

	return !os.IsNotExist(err)
}

func GetLastCommit() (*object.Commit, error) {
	repoPath := config.GetConfig().GitConfig.RepoPath

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open git repository: %w", err)
	}

	ref, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD reference: %w", err)
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to get commit object: %w", err)
	}

	return commit, nil
}

func StartPullLoop() {
	if !loopRunning.CompareAndSwap(false, true) {
		return // Already running
	}

	err := Pull()
	if err != nil {
		log.Error(err)
	}

	ticker := time.NewTicker(config.GetConfig().GitConfig.PullInterval)

	go func() {
		for {
			select {
			case <-ticker.C:
				err := Pull()
				if err != nil {
					log.Error(err)
				}
			case <-quit:
				loopRunning.Store(false)
				ticker.Stop()

				return
			}
		}
	}()
}

func StopPullLoop() {
	quit <- true

	close(quit)
}
