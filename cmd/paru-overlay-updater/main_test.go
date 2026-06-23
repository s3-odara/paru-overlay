package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestRun_RejectsArguments(t *testing.T) {
	err := run(context.Background(), io.Discard, []string{"extra"})
	if err == nil {
		t.Fatal("expected error for any argument")
	}
	if !strings.Contains(err.Error(), "no subcommands or flags") {
		t.Fatalf("error should mention simplified CLI, got: %v", err)
	}
}

func TestConfigFromEnv(t *testing.T) {
	env := map[string]string{
		"GITHUB_REPOSITORY": "owner/repo",
		"GITHUB_REF_NAME":   "main",
		"GITHUB_TOKEN":      "token",
	}
	cfg, err := configFromEnv(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("configFromEnv: %v", err)
	}
	if cfg.RootDir != "." || cfg.Owner != "owner" || cfg.Repo != "repo" || cfg.BaseBranch != "main" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.Cloner == nil || cfg.Git == nil || cfg.GitHub == nil {
		t.Fatalf("expected production dependencies to be configured: %+v", cfg)
	}
}

func TestConfigFromEnv_MissingRequiredEnv(t *testing.T) {
	env := map[string]string{
		"GITHUB_REPOSITORY": "owner/repo",
		"GITHUB_REF_NAME":   "main",
	}
	_, err := configFromEnv(func(key string) string { return env[key] })
	if err == nil {
		t.Fatal("expected missing token error")
	}
	if !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("error should mention missing GITHUB_TOKEN, got: %v", err)
	}
}

func TestConfigFromEnv_InvalidRepository(t *testing.T) {
	_, err := configFromEnv(func(key string) string {
		if key == "GITHUB_REPOSITORY" {
			return "not-owner-slash-repo"
		}
		return ""
	})
	if err == nil {
		t.Fatal("expected invalid repository error")
	}
	if !strings.Contains(err.Error(), "GITHUB_REPOSITORY") {
		t.Fatalf("error should mention GITHUB_REPOSITORY, got: %v", err)
	}
}

func TestSplitRepository(t *testing.T) {
	owner, repo, err := splitRepository("octo/example")
	if err != nil {
		t.Fatalf("splitRepository: %v", err)
	}
	if owner != "octo" || repo != "example" {
		t.Fatalf("got %q/%q", owner, repo)
	}
}
