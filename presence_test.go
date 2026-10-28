package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPresenceAcrossLibrariesAndPartialCollisions(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "one", "steamapps")
	second := filepath.Join(root, "two", "steamapps")
	for _, p := range []string{filepath.Join(first, "common"), filepath.Join(second, "common", "Different install name"), filepath.Join(first, "common", "Folder only")} {
		if e := os.MkdirAll(p, 0755); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.WriteFile(filepath.Join(second, "appmanifest_1.acf"), []byte(`"appid" "1" "installdir" "Different install name"`), 0644); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(first, "appmanifest_2.acf"), []byte("invalid manifest"), 0644); e != nil {
		t.Fatal(e)
	}
	games := []Game{{ID: "1", Dir: "Game One", Library: "source"}, {ID: "2", Dir: "Game Two", Library: "source"}, {ID: "3", Dir: "Folder only", Library: "source"}, {ID: "4", Dir: "Missing", Library: "source"}}
	found, e := checkPresence(games, []string{first, second})
	if e != nil {
		t.Fatal(e)
	}
	if len(found) != 3 {
		t.Fatalf("got %+v", found)
	}
	if found[0].Kind != "installed" || found[0].Target != second || found[1].Kind != "manifest" || found[2].Kind != "folder" {
		t.Fatalf("unexpected statuses: %+v", found)
	}
	// A new backup location is valid and contains no existing games.
	found, e = checkPresence(games, []string{filepath.Join(root, "new-backup")})
	if e != nil || len(found) != 0 {
		t.Fatalf("new target: %+v, %v", found, e)
	}
}
func TestPresenceRejectsInvalidPathsAndGameNames(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if e := os.WriteFile(file, []byte("x"), 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := checkPresence([]Game{{ID: "1", Dir: "Game", Library: "source"}}, []string{file}); e == nil {
		t.Fatal("file target accepted")
	}
	if _, e := checkPresence([]Game{{ID: "../1", Dir: "Game", Library: "source"}}, []string{root}); e == nil {
		t.Fatal("unsafe id accepted")
	}
	if _, e := checkPresence([]Game{{ID: "1", Dir: "../escape", Library: "source"}}, []string{root}); e == nil {
		t.Fatal("unsafe directory accepted")
	}
}
