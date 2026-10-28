package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

type Game struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Dir     string `json:"dir"`
	Library string `json:"library"`
	Size    uint64 `json:"size"`
	Ready   bool   `json:"ready"`
}

var pairRE = regexp.MustCompile(`"((?:\\.|[^"\\])*)"\s*"((?:\\.|[^"\\])*)"`)
var manifestRE = regexp.MustCompile(`^appmanifest_([0-9]+)\.acf$`)

func unescape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, `\\`, `\`), `\"`, `"`)
}
func pairs(data []byte) map[string]string {
	m := map[string]string{}
	for _, p := range pairRE.FindAllStringSubmatch(string(data), -1) {
		k := strings.ToLower(unescape(p[1]))
		if _, ok := m[k]; !ok {
			m[k] = unescape(p[2])
		}
	}
	return m
}
func appsPath(p string) (string, error) {
	p = strings.Trim(strings.TrimSpace(p), `"`)
	if p == "" {
		return "", fmt.Errorf("choose a library folder")
	}
	p, e := filepath.Abs(p)
	if e != nil {
		return "", e
	}
	if strings.EqualFold(filepath.Base(p), "steamapps") {
		return filepath.Clean(p), nil
	}
	return filepath.Join(p, "steamapps"), nil
}
func safeDir(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\`) && !filepath.IsAbs(s)
}
func scan(path string) ([]Game, error) {
	p, e := appsPath(path)
	if e != nil {
		return nil, e
	}
	files, e := os.ReadDir(p)
	if e != nil {
		return nil, e
	}
	games := []Game{}
	for _, f := range files {
		match := manifestRE.FindStringSubmatch(f.Name())
		if match == nil || f.IsDir() {
			continue
		}
		b, e := os.ReadFile(filepath.Join(p, f.Name()))
		if e != nil {
			continue
		}
		m := pairs(b)
		if m["appid"] != match[1] || !safeDir(m["installdir"]) {
			continue
		}
		size, _ := strconv.ParseUint(m["sizeondisk"], 10, 64)
		info, e := os.Lstat(filepath.Join(p, "common", m["installdir"]))
		ready := e == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
		games = append(games, Game{match[1], m["name"], m["installdir"], p, size, ready})
	}
	sort.Slice(games, func(i, j int) bool { return games[i].Name < games[j].Name })
	return games, nil
}
func discover() []string {
	return discoverPaths(steamRoots(), mountedVolumes())
}

type Discovery struct {
	Local    []string `json:"local"`
	External []string `json:"external"`
	All      []string `json:"all"`
}

func classifyLibraries(registered, all, externalRoots []string) Discovery {
	result := Discovery{Local: []string{}, External: []string{}, All: all}
	known := map[string]bool{}
	key := func(s string) string {
		if p, e := filepath.EvalSymlinks(s); e == nil {
			s = p
		}
		s = filepath.Clean(s)
		if runtime.GOOS == "windows" {
			s = strings.ToLower(s)
		}
		return s
	}
	for _, p := range registered {
		known[key(p)] = true
	}
	for _, p := range all {
		external := false
		for _, root := range externalRoots {
			if isWithin(key(p), key(root)) {
				external = true
				break
			}
		}
		if external || !known[key(p)] {
			result.External = append(result.External, p)
		} else {
			result.Local = append(result.Local, p)
		}
	}
	return result
}
func discoverClassified() Discovery {
	return classifyLibraries(discoverPaths(steamRoots(), nil), discover(), externalVolumes())
}
func discoverPaths(roots, volumes []string) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(path string) {
		p, e := appsPath(path)
		if e != nil {
			return
		}
		p = filepath.Clean(p)
		s, e := os.Stat(p)
		key := p
		if canonical, err := filepath.EvalSymlinks(p); err == nil {
			key = canonical
		}
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if e == nil && s.IsDir() && !seen[key] {
			seen[key] = true
			out = append(out, p)
		}
	}
	for _, r := range roots {
		add(r)
		b, e := os.ReadFile(filepath.Join(r, "steamapps", "libraryfolders.vdf"))
		if e != nil {
			continue
		}
		for _, m := range pairRE.FindAllStringSubmatch(string(b), -1) {
			if strings.EqualFold(m[1], "path") {
				add(unescape(m[2]))
			}
		}
	}
	// Search the root and one directory level of each local volume. Do not
	// recursively traverse game files or follow junctions/symlinks.
	for _, volume := range volumes {
		candidates := []string{volume}
		entries, err := os.ReadDir(volume)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			candidate := filepath.Join(volume, entry.Name())
			info, err := os.Lstat(candidate)
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			candidates = append(candidates, candidate)
		}
		for _, candidate := range candidates {
			apps, err := appsPath(candidate)
			if err != nil {
				continue
			}
			if info, err := os.Stat(filepath.Join(apps, "common")); err == nil && info.IsDir() {
				add(candidate)
				continue
			}
			if matches, _ := filepath.Glob(filepath.Join(apps, "appmanifest_*.acf")); len(matches) > 0 {
				add(candidate)
			}
		}
	}
	sort.Strings(out)
	return out
}
