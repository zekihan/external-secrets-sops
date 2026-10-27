package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/zekihan/external-secrets-sops/internal/config"
	"github.com/zekihan/external-secrets-sops/internal/offline"
)

func runResolve(args []string, stdout, stderr io.Writer) int {
	return runResolveInput(args, os.Stdin, stdout, stderr)
}

func runResolveInput(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("resolve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	opts := offline.Options{Limits: config.Defaults().Limits}
	flags.StringVar(&opts.Input, "input", "", "dummy, sops or memory (stdin JSON documents)")
	flags.StringVar(
		&opts.Root,
		"root",
		"",
		"synthetic directory or repository-relative encrypted root",
	)
	flags.StringVar(&opts.Repo, "repo", "", "local repository (sops only)")
	flags.StringVar(&opts.Revision, "revision", "", "full commit hash (sops only)")
	flags.StringVar(&opts.Bundle, "bundle", "", "selected project/config/environment")
	projects := flags.String("projects", "", "comma-separated dependency projects")
	environments := flags.String("environments", "", "comma-separated dependency environments")
	flags.IntVar(
		&opts.Limits.DecryptWorkers,
		"decrypt-workers",
		opts.Limits.DecryptWorkers,
		"decryption workers",
	)
	flags.Int64Var(
		&opts.Limits.DocumentBytes,
		"document-bytes",
		opts.Limits.DocumentBytes,
		"maximum document bytes",
	)
	flags.Int64Var(
		&opts.Limits.SnapshotBytes,
		"snapshot-bytes",
		opts.Limits.SnapshotBytes,
		"maximum candidate bytes",
	)
	flags.IntVar(
		&opts.Limits.TemplateDepth,
		"template-depth",
		opts.Limits.TemplateDepth,
		"maximum reference depth",
	)
	flags.IntVar(
		&opts.Limits.TemplateEvaluations,
		"template-evaluations",
		opts.Limits.TemplateEvaluations,
		"maximum reference evaluations",
	)
	flags.Int64Var(
		&opts.Limits.TemplateValueBytes,
		"template-value-bytes",
		opts.Limits.TemplateValueBytes,
		"maximum resolved value bytes",
	)
	timeout := flags.Duration("timeout", config.Defaults().Refresh.Timeout, "resolution deadline")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(stdout)
			flags.PrintDefaults()
			return 0
		}
		_, _ = fmt.Fprintln(stderr, "invalid resolve arguments; use resolve --help")
		return 2
	}
	if flags.NArg() != 0 || *timeout <= 0 {
		_, _ = fmt.Fprintln(stderr, "invalid resolve arguments; use resolve --help")
		return 2
	}
	if opts.Input == "memory" {
		opts.Documents = stdin
	}
	opts.Projects = strings.Split(*projects, ",")
	opts.Environments = strings.Split(*environments, ",")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	values, err := offline.Resolve(ctx, opts)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	if err = json.NewEncoder(stdout).Encode(values); err != nil {
		_, _ = fmt.Fprintln(stderr, "cannot write resolved bundle")
		return 1
	}
	return 0
}
