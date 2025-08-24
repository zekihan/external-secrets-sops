package pubsub

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"

	"github.com/zekihan/external-secrets-sops/git"
)

const githubEventsChannel = "github-events"

func PublishGithubEvent(ctx context.Context, message string) error {
	return publish(ctx, githubEventsChannel, message)
}

func ListenGithubEvents() context.CancelFunc {
	return listen(githubEventsChannel, func(ctx context.Context, payload string) error {
		log.Infof("Received GitHub event: %s", payload)

		err := git.Pull(ctx)
		if err != nil {
			log.Errorf("Error pulling from git: %v", err)

			return fmt.Errorf("error pulling from git: %w", err)
		}

		log.Infof("Successfully pulled latest changes from git")

		return nil
	})
}
