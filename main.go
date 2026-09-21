package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/zekihan/external-secrets-sops/internal/app"
	"github.com/zekihan/external-secrets-sops/internal/config"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
func run(args []string, stdout, stderr io.Writer) int {
	command := "serve"
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		command = args[0]
		args = args[1:]
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	filename := flags.String("config", "config.yaml", "configuration file")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		return 2
	}
	switch command {
	case "version":
		_, _ = fmt.Fprintf(
			stdout,
			"external-secrets-sops %s (%s, %s)\n",
			version,
			commit,
			buildDate,
		)
		return 0
	case "serve", "check-config":
	default:
		_, _ = fmt.Fprintln(
			stderr,
			"usage: external-secrets-sops [serve|check-config|version] [--config path]",
		)
		return 2
	}
	cfg, creds, err := config.Load(*filename, os.Getenv)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	if command == "check-config" {
		_, _ = fmt.Fprintln(stdout, "Configuration valid")
		return 0
	}
	var level slog.Level
	if err = level.UnmarshalText([]byte(cfg.Server.LogLevel)); err != nil {
		return 1
	}
	logger := slog.New(slog.NewJSONHandler(stderr, &slog.HandlerOptions{Level: level}))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err = app.Run(ctx, cfg, creds, logger); err != nil {
		logger.ErrorContext(ctx, "Application failed", slog.Any("error", err))
		return 1
	}
	return 0
}
