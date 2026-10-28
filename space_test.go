package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDestinationSpaceExistingAndNew(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "library", "steamapps", "common")
	if e := os.MkdirAll(existing, 0755); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{filepath.Join(root, "library"), filepath.Join(root, "new-backup")} {
		if n, e := destinationSpace(path); e != nil || n == 0 {
			t.Fatalf("destination %s: %d %v", path, n, e)
		}
	}
	if _, e := destinationSpace(""); e == nil {
		t.Fatal("empty destination accepted")
	}
	f := filepath.Join(root, "not-a-directory")
	if e := os.WriteFile(f, []byte("x"), 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := destinationSpace(f); e == nil {
		t.Fatal("file used as destination")
	}
}
