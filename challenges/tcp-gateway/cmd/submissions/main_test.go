package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func makeSubmission(t *testing.T, root, name string, launcher bool) {
	t.Helper()
	path := filepath.Join(root, "submissions", "tcp-gateway", name)
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if launcher {
		if err := os.WriteFile(filepath.Join(path, "run.sh"), []byte("#!/usr/bin/env bash\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	makeSubmission(t, root, "alice", true)
	makeSubmission(t, root, "bob", true)
	got, err := discover(root, []string{
		"README.md", "submissions/tcp-gateway/.gitkeep",
		"submissions/tcp-gateway/bob/run.sh",
		"submissions/tcp-gateway/alice/main.go",
		"submissions/tcp-gateway/alice/deleted.go",
		"submissions/tcp-gateway/removed/run.sh",
		"submissions/other/carol/run.sh",
	})
	want := []string{"submissions/tcp-gateway/alice", "submissions/tcp-gateway/bob"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, %v; want %v", got, err, want)
	}
}

func TestDiscoverRejectsInvalidSubmissions(t *testing.T) {
	for _, name := range []string{"missing-launcher", "bad name", ".."} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if name != ".." {
				makeSubmission(t, root, name, false)
			}
			if _, err := discover(root, []string{"submissions/tcp-gateway/" + name + "/main.go"}); err == nil {
				t.Fatal("accepted invalid submission")
			}
		})
	}
}

func TestDiscoverNoChanges(t *testing.T) {
	got, err := discover(t.TempDir(), []string{"README.md"})
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty matrix must use [] rather than null: %v, %v", got, err)
	}
}
