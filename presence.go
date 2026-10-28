package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

type Presence struct {
	Source string `json:"source"`
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Target string `json:"target"`
}
type targetIndex struct {
	root      string
	manifests map[string]os.DirEntry
	folders   map[string]bool
}

var numericID = regexp.MustCompile(`^[0-9]+$`)

func folderKey(s string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(s)
	}
	return s
}

func indexTarget(path string) (targetIndex, error) {
	p, e := appsPath(path)
	if e != nil {
		return targetIndex{}, e
	}
	t := targetIndex{root: p, manifests: map[string]os.DirEntry{}, folders: map[string]bool{}}
	entries, e := os.ReadDir(p)
	if os.IsNotExist(e) {
		base, err := existingBase(p)
		if err != nil {
			return t, err
		}
		if _, err = os.ReadDir(base); err != nil {
			return t, err
		}
		return t, nil
	}
	if e != nil {
		return t, e
	}
	for _, entry := range entries {
		name := entry.Name()
		if runtime.GOOS == "windows" {
			name = strings.ToLower(name)
		}
		if m := manifestRE.FindStringSubmatch(name); m != nil {
			t.manifests[m[1]] = entry
		}
	}
	entries, e = os.ReadDir(filepath.Join(p, "common"))
	if e != nil && !os.IsNotExist(e) {
		return t, e
	}
	for _, entry := range entries {
		t.folders[folderKey(entry.Name())] = true
	}
	return t, nil
}

func checkPresence(games []Game, targets []string) ([]Presence, error) {
	if len(games) > 10000 || len(targets) > 1000 {
		return nil, fmt.Errorf("too many libraries or games")
	}
	indexes := make([]targetIndex, 0, len(targets))
	for _, p := range targets {
		t, e := indexTarget(p)
		if e != nil {
			return nil, fmt.Errorf("cannot inspect %s: %w", p, e)
		}
		indexes = append(indexes, t)
	}
	result := []Presence{}
	for _, g := range games {
		if !numericID.MatchString(g.ID) || !safeDir(g.Dir) || g.Library == "" {
			return nil, fmt.Errorf("invalid game identifier or folder")
		}
		var found *Presence
		for _, t := range indexes {
			if entry, ok := t.manifests[g.ID]; ok {
				kind := "manifest"
				b, e := os.ReadFile(filepath.Join(t.root, entry.Name()))
				if e == nil {
					m := pairs(b)
					if m["appid"] == g.ID && safeDir(m["installdir"]) && t.folders[folderKey(m["installdir"])] {
						if s, err := os.Stat(filepath.Join(t.root, "common", m["installdir"])); err == nil && s.IsDir() {
							kind = "installed"
						}
					}
				}
				item := Presence{g.Library, g.ID, kind, t.root}
				if found == nil || kind == "installed" {
					found = &item
				}
				if kind == "installed" {
					break
				}
			} else if t.folders[folderKey(g.Dir)] && found == nil {
				item := Presence{g.Library, g.ID, "folder", t.root}
				found = &item
			}
		}
		if found != nil {
			result = append(result, *found)
		}
	}
	return result, nil
}
