package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallPreCommitWritesExecutableScript(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := installPreCommit(dir, false, &out); err != nil {
		t.Fatalf("installPreCommit: %v", err)
	}
	path := filepath.Join(dir, "pre-commit")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("hook should be executable, got mode %v", info.Mode())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "make check") {
		t.Errorf("hook should invoke `make check`, got:\n%s", body)
	}
}

func TestInstallPreCommitRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "pre-commit")
	if err := os.WriteFile(existing, []byte("#!/bin/sh\necho existing\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := installPreCommit(dir, false, &out)
	if err == nil {
		t.Fatal("expected error when pre-commit already exists")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error should mention --force, got: %v", err)
	}
	body, _ := os.ReadFile(existing)
	if !strings.Contains(string(body), "echo existing") {
		t.Errorf("existing hook was overwritten: %s", body)
	}
}

func TestInstallPreCommitForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "pre-commit")
	if err := os.WriteFile(existing, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := installPreCommit(dir, true, &out); err != nil {
		t.Fatalf("installPreCommit with --force: %v", err)
	}
	body, _ := os.ReadFile(existing)
	if !strings.Contains(string(body), "make check") {
		t.Errorf("--force should have replaced the hook, got:\n%s", body)
	}
}

func TestInstallPreCommitCreatesMissingHooksDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hooks")
	var out bytes.Buffer
	if err := installPreCommit(dir, false, &out); err != nil {
		t.Fatalf("installPreCommit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pre-commit")); err != nil {
		t.Errorf("expected hook in newly-created dir: %v", err)
	}
}
