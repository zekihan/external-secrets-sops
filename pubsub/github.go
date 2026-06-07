package pubsub

import (
	"context"
	"fmt"
	"log/slog"

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
		slog.InfoContext(ctx, "Received GitHub event, pulling latest changes from git")
		slog.DebugContext(ctx, "GitHub event payload", slog.String("payload", payload))

		err := git.Pull(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "Error pulling from git", slog.Any("error", err))

			return fmt.Errorf("error pulling from git: %w", err)
		}

		slog.InfoContext(ctx, "Successfully pulled from git")

		return nil
	})
}
