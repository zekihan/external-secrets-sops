package git

import (
	"errors"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/external-secrets-sops/config"
	"os"
	"path"
	"time"
)

var quit chan bool

func clone() error {
	repoPath := config.GetConfig().GitConfig.RepoPath
	repoURL := config.GetConfig().GitConfig.RepoURL
	if repoURL == "" {
		return errors.New("repo_url is empty")
	}

	if _, err := os.Stat(path.Join(repoPath, ".git")); err == nil {
		r, err := git.PlainOpen(repoPath)
		if err != nil {
			return err
		}
		remote, err := r.Remote("origin")
		if err != nil {
			return err
		}
		urls := remote.Config().URLs
		if len(urls) > 0 && urls[0] == repoURL {
			return nil
		} else {
			err := os.RemoveAll(repoPath)
			if err != nil {
				return err
			}
		}
	}

	_, err := git.PlainClone(repoPath, false, &git.CloneOptions{
		URL:      repoURL,
		Progress: log.StandardLogger().Writer(),
		Auth: &http.BasicAuth{
			Username: "X", // anything except an empty string
			Password: config.GetConfig().GitConfig.AccessToken,
		},
	})
	if err != nil {
		return err
	}
	return nil
}

func Pull() error {
	repoPath := config.GetConfig().GitConfig.RepoPath
	if _, err := os.Stat(path.Join(repoPath, ".git")); os.IsNotExist(err) {
		err := clone()
		if err != nil {
			return err
		}
	}

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return err
	}

	remote, err := repo.Remote("origin")
	if err != nil {
		return err
	}

	err = remote.Fetch(&git.FetchOptions{
		Auth: &http.BasicAuth{
			Username: "X", // anything except an empty string
			Password: config.GetConfig().GitConfig.AccessToken,
		},
	})
	if err != nil {
		if errors.Is(err, git.NoErrAlreadyUpToDate) {
			commit, err := GetLastCommit()
			if err != nil {
				return nil
			}
			log.Info("already up to date. last commit: ", commit.String())
			return nil
		}
		return err
	}
	newRef, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", "main"), false)
	if err != nil {
		return err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}

	err = wt.Reset(&git.ResetOptions{
		Commit: newRef.Hash(),
		Mode:   git.HardReset,
	})
	if err != nil {
		return err
	}
	commit, err := repo.CommitObject(newRef.Hash())
	if err != nil {
		return err
	}
	log.Info("new commit: ", commit.String())
	return nil
}

func IsRepoReady() bool {
	repoPath := path.Join(config.GetConfig().GitConfig.RepoPath, ".git")
	return isDirExists(repoPath)
}

func isDirExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func GetLastCommit() (*object.Commit, error) {
	repoPath := config.GetConfig().GitConfig.RepoPath
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, err
	}
	ref, err := repo.Head()
	if err != nil {
		return nil, err
	}
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, err
	}
	return commit, nil
}

func StartPullLoop() {
	err := Pull()
	if err != nil {
		log.Error(err)
	}
	ticker := time.NewTicker(config.GetConfig().GitConfig.PullInterval)
	quit = make(chan bool)
	go func() {
		for {
			select {
			case <-ticker.C:
				err := Pull()
				if err != nil {
					log.Error(err)
				}
			case <-quit:
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
