package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"paru-overlay-updater/internal/cmd"
	"paru-overlay-updater/internal/git"
	"paru-overlay-updater/internal/github"
	"paru-overlay-updater/internal/overlay"
)

func main() {
	if err := run(context.Background(), os.Stdout, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, out io.Writer, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("unexpected argument(s): %v; this command takes no subcommands or flags", args)
	}

	cfg, err := configFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	err = cmd.CheckUpdates(ctx, cfg, out)
	return err
}

func configFromEnv(getenv func(string) string) (cmd.CheckConfig, error) {
	owner, repo, err := splitRepository(getenv("GITHUB_REPOSITORY"))
	if err != nil {
		return cmd.CheckConfig{}, err
	}
	baseBranch, err := requiredEnv(getenv, "GITHUB_REF_NAME")
	if err != nil {
		return cmd.CheckConfig{}, err
	}
	token, err := requiredEnv(getenv, "GITHUB_TOKEN")
	if err != nil {
		return cmd.CheckConfig{}, err
	}

	return cmd.CheckConfig{
		Cloner:     &overlay.GitCloner{},
		Git:        &git.ExecDriver{},
		GitHub:     github.NewClient(token, nil),
		RootDir:    ".",
		Owner:      owner,
		Repo:       repo,
		BaseBranch: baseBranch,
	}, nil
}

func requiredEnv(getenv func(string) string, key string) (string, error) {
	value := getenv(key)
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func splitRepository(value string) (string, string, error) {
	parts := strings.SplitN(value, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("GITHUB_REPOSITORY must be set as owner/repo")
	}
	return parts[0], parts[1], nil
}
