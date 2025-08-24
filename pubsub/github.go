package pubsub

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"

	"github.com/zekihan/external-secrets-sops/git"
)

const githubEventsChannel = "github-events"

func PublishGithubEvent(ctx context.Context, message string) error {
	err := getPubSuber().Publish(ctx, githubEventsChannel, message)
	if err != nil {
		return fmt.Errorf("error publishing GitHub event: %w", err)
	}

	return nil
}

func ListenGithubEvents() context.CancelFunc {
	return getPubSuber().Listen(githubEventsChannel, func(ctx context.Context, payload string) error {
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
