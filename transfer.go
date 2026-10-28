package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Entry struct {
	Rel  string
	Dir  bool
	Size int64
	Mod  time.Time
	Mode fs.FileMode
}
type Plan struct {
	Game                                 Game
	Source, Dest, Manifest, DestManifest string
	Entries                              []Entry
	Bytes                                int64
	ManifestHash                         string
}
type Request struct {
	Source string          `json:"source"`
	Target string          `json:"target"`
	IDs    []string        `json:"ids"`
	Full   bool            `json:"full"`
	Games  []GameSelection `json:"games"`
}
type GameSelection struct {
	Source string `json:"source"`
	ID     string `json:"id"`
}
type Status struct {
	State     string `json:"state"`
	Phase     string `json:"phase"`
	Message   string `json:"message"`
	Total     int64  `json:"total"`
	Done      int64  `json:"done"`
	Current   string `json:"current"`
	Completed int    `json:"completed"`
	CanDelete bool   `json:"canDelete"`
}
type Manager struct {
	mu        sync.Mutex
	status    Status
	cancel    context.CancelFunc
	completed []Plan
	running   bool
}

func (m *Manager) update(f func(*Status)) { m.mu.Lock(); defer m.mu.Unlock(); f(&m.status) }
func (m *Manager) snapshot() Status       { m.mu.Lock(); defer m.mu.Unlock(); return m.status }
func randomID() string {
	b := make([]byte, 12)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func hashFile(p string) (string, error) {
	return hashFileContext(context.Background(), p)
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
func hashFileContext(ctx context.Context, p string) (string, error) {
	f, e := os.Open(p)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, contextReader{ctx, f}); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func inventory(root string) ([]Entry, int64, error) {
	return inventoryContext(context.Background(), root)
}
func inventoryContext(ctx context.Context, root string) ([]Entry, int64, error) {
	var out []Entry
	var size int64
	e := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err != nil {
			return err
		}
		info, e := os.Lstat(path)
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("unsupported link or special file: %s", path)
		}
		if path == root {
			return nil
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		out = append(out, Entry{rel, info.IsDir(), info.Size(), info.ModTime(), info.Mode().Perm()})
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, size, e
}
func selectedSize(ctx context.Context, games []GameSelection) (int64, error) {
	cache := map[string]map[string]Game{}
	seen := map[string]bool{}
	var total int64
	for _, g := range games {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		p, e := appsPath(g.Source)
		if e != nil {
			return 0, e
		}
		key := p + "\x00" + g.ID
		if seen[key] {
			return 0, fmt.Errorf("duplicate selection")
		}
		seen[key] = true
		if cache[p] == nil {
			entries, e := scan(p)
			if e != nil {
				return 0, e
			}
			cache[p] = map[string]Game{}
			for _, v := range entries {
				cache[p][v.ID] = v
			}
		}
		game, ok := cache[p][g.ID]
		if !ok || !game.Ready {
			return 0, fmt.Errorf("game %s is missing or invalid", g.ID)
		}
		_, size, e := inventoryContext(ctx, filepath.Join(p, "common", game.Dir))
		if e != nil {
			return 0, e
		}
		manifest, e := os.Lstat(filepath.Join(p, "appmanifest_"+g.ID+".acf"))
		if e != nil {
			return 0, e
		}
		if !manifest.Mode().IsRegular() {
			return 0, fmt.Errorf("manifest is not a regular file")
		}
		total += size + manifest.Size()
	}
	return total, nil
}
func sameInventory(a, b []Entry, source bool) error {
	if len(a) != len(b) {
		return fmt.Errorf("entry count differs: %d and %d", len(a), len(b))
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.Rel != y.Rel || x.Dir != y.Dir || (!x.Dir && x.Size != y.Size) || (source && !x.Dir && !x.Mod.Equal(y.Mod)) {
			return fmt.Errorf("entry changed or differs: %s", x.Rel)
		}
	}
	return nil
}
func isWithin(p, root string) bool {
	r, e := filepath.Rel(root, p)
	return e == nil && (r == "." || r != ".." && !strings.HasPrefix(r, ".."+string(os.PathSeparator)))
}
func existingBase(p string) (string, error) {
	for {
		s, e := os.Stat(p)
		if e == nil {
			if !s.IsDir() {
				return "", fmt.Errorf("not a directory: %s", p)
			}
			return p, nil
		}
		if !os.IsNotExist(e) {
			return "", e
		}
		next := filepath.Dir(p)
		if next == p {
			return "", e
		}
		p = next
	}
}
func plan(req Request) ([]Plan, uint64, error) {
	if len(req.Games) == 0 {
		return planSingle(req)
	}
	groups := map[string][]string{}
	for _, game := range req.Games {
		groups[game.Source] = append(groups[game.Source], game.ID)
	}
	paths := make([]string, 0, len(groups))
	for p := range groups {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var all []Plan
	var free uint64
	var total uint64
	destinations := map[string]bool{}
	for i, source := range paths {
		p, available, err := planSingle(Request{Source: source, Target: req.Target, IDs: groups[source], Full: req.Full})
		if err != nil {
			return nil, available, err
		}
		if i == 0 || available < free {
			free = available
		}
		for _, item := range p {
			for _, dest := range []string{item.Dest, item.DestManifest} {
				key := strings.ToLower(filepath.Clean(dest))
				if destinations[key] {
					return nil, free, fmt.Errorf("selected games collide at destination: %s", dest)
				}
				destinations[key] = true
			}
			total += uint64(item.Bytes)
			all = append(all, item)
		}
	}
	if total > free || free-total < 1<<30 {
		return nil, free, fmt.Errorf("not enough free space: need %d bytes plus 1 GiB reserve, available %d", total, free)
	}
	return all, free, nil
}
func planSingle(req Request) ([]Plan, uint64, error) {
	if len(req.IDs) == 0 {
		return nil, 0, fmt.Errorf("select at least one game")
	}
	source, e := appsPath(req.Source)
	if e != nil {
		return nil, 0, e
	}
	target, e := appsPath(req.Target)
	if e != nil {
		return nil, 0, e
	}
	source, e = filepath.EvalSymlinks(source)
	if e != nil {
		return nil, 0, e
	}
	base, e := existingBase(target)
	if e != nil {
		return nil, 0, e
	}
	realBase, e := filepath.EvalSymlinks(base)
	if e != nil {
		return nil, 0, e
	}
	tail, e := filepath.Rel(base, target)
	if e != nil {
		return nil, 0, e
	}
	target = filepath.Join(realBase, tail)
	for _, common := range []string{filepath.Join(source, "common"), filepath.Join(target, "common")} {
		if info, err := os.Lstat(common); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
			return nil, 0, fmt.Errorf("common folder is a link or not a directory: %s", common)
		} else if err != nil && !os.IsNotExist(err) {
			return nil, 0, err
		}
	}
	if isWithin(target, source) || isWithin(source, target) {
		return nil, 0, fmt.Errorf("source and target libraries overlap")
	}
	games, e := scan(source)
	if e != nil {
		return nil, 0, e
	}
	byID := map[string]Game{}
	for _, g := range games {
		byID[g.ID] = g
	}
	seen := map[string]bool{}
	var plans []Plan
	var total uint64
	for _, id := range req.IDs {
		if seen[id] {
			return nil, 0, fmt.Errorf("duplicate selection")
		}
		seen[id] = true
		g, ok := byID[id]
		if !ok || !g.Ready {
			return nil, 0, fmt.Errorf("game %s is missing or invalid", id)
		}
		src := filepath.Join(source, "common", g.Dir)
		dst := filepath.Join(target, "common", g.Dir)
		mf := filepath.Join(source, "appmanifest_"+g.ID+".acf")
		dm := filepath.Join(target, "appmanifest_"+g.ID+".acf")
		for _, p := range []string{dst, dm} {
			if _, e := os.Lstat(p); e == nil {
				return nil, 0, fmt.Errorf("target already exists: %s", p)
			} else if !os.IsNotExist(e) {
				return nil, 0, e
			}
		}
		entries, bytes, e := inventory(src)
		if e != nil {
			return nil, 0, e
		}
		mh, e := hashFile(mf)
		if e != nil {
			return nil, 0, e
		}
		s, e := os.Stat(mf)
		if e != nil {
			return nil, 0, e
		}
		bytes += s.Size()
		total += uint64(bytes)
		plans = append(plans, Plan{g, src, dst, mf, dm, entries, bytes, mh})
	}
	commonBase, e := existingBase(filepath.Join(target, "common"))
	if e != nil {
		return nil, 0, e
	}
	fat, e := isFAT32(commonBase)
	if e != nil {
		return nil, 0, fmt.Errorf("cannot identify destination filesystem: %w", e)
	}
	if fat {
		for _, p := range plans {
			for _, entry := range p.Entries {
				if !entry.Dir && uint64(entry.Size) > 0xffffffff {
					return nil, 0, fmt.Errorf("FAT32 cannot store a file of 4 GiB or more: %s", entry.Rel)
				}
			}
		}
	}
	free, e := freeBytes(commonBase)
	if e != nil {
		return nil, 0, e
	}
	manifestFree, e := freeBytes(base)
	if e != nil {
		return nil, 0, e
	}
	if manifestFree < free {
		free = manifestFree
	}
	if total > free || free-total < 1<<30 {
		return nil, free, fmt.Errorf("not enough free space: need %d bytes plus 1 GiB reserve, available %d", total, free)
	}
	return plans, free, nil
}
func copyFile(ctx context.Context, src, dst string, m *Manager) error {
	in, e := os.Open(src)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	buf := make([]byte, 4*1024*1024)
	for {
		if e = ctx.Err(); e != nil {
			break
		}
		var n int
		n, e = in.Read(buf)
		if n > 0 {
			var w int
			w, e = out.Write(buf[:n])
			m.update(func(s *Status) { s.Done += int64(w) })
			if e != nil {
				break
			}
			if w != n {
				e = io.ErrShortWrite
				break
			}
		}
		if e == io.EOF {
			e = nil
			break
		}
		if e != nil {
			break
		}
	}
	if e == nil {
		e = out.Sync()
	}
	if ce := out.Close(); e == nil {
		e = ce
	}
	return e
}
func verify(ctx context.Context, p Plan, dest string, full bool) error {
	before, _, e := inventory(p.Source)
	if e != nil {
		return e
	}
	if e = sameInventory(p.Entries, before, true); e != nil {
		return fmt.Errorf("source: %w", e)
	}
	dst, _, e := inventory(dest)
	if e != nil {
		return e
	}
	if e = sameInventory(p.Entries, dst, false); e != nil {
		return fmt.Errorf("destination: %w", e)
	}
	if full {
		for _, v := range p.Entries {
			if v.Dir {
				continue
			}
			a, e := hashFileContext(ctx, filepath.Join(p.Source, v.Rel))
			if e != nil {
				return e
			}
			b, e := hashFileContext(ctx, filepath.Join(dest, v.Rel))
			if e != nil {
				return e
			}
			if a != b {
				return fmt.Errorf("content mismatch: %s", v.Rel)
			}
		}
	}
	after, _, e := inventory(p.Source)
	if e != nil {
		return e
	}
	if e = sameInventory(p.Entries, after, true); e != nil {
		return fmt.Errorf("source changed: %w", e)
	}
	h, e := hashFile(p.Manifest)
	if e != nil {
		return e
	}
	if h != p.ManifestHash {
		return fmt.Errorf("source manifest changed")
	}
	return nil
}
func transfer(ctx context.Context, p Plan, full bool, m *Manager) error {
	common := filepath.Dir(p.Dest)
	if e := os.MkdirAll(common, 0755); e != nil {
		return e
	}
	if _, e := os.Lstat(p.DestManifest); e == nil {
		return fmt.Errorf("target manifest exists")
	} else if !os.IsNotExist(e) {
		return e
	}
	stage := filepath.Join(common, ".steamcopy-"+p.Game.ID+"-"+randomID()+".partial")
	if e := os.Mkdir(stage, 0700); e != nil {
		return e
	}
	for _, v := range p.Entries {
		dst := filepath.Join(stage, v.Rel)
		if v.Dir {
			if e := os.Mkdir(dst, 0755); e != nil {
				return e
			}
			continue
		}
		m.update(func(s *Status) { s.Current = p.Game.Name + " / " + v.Rel })
		if e := copyFile(ctx, filepath.Join(p.Source, v.Rel), dst, m); e != nil {
			return fmt.Errorf("copy failed; partial retained at %s: %w", stage, e)
		}
		if e := os.Chmod(dst, v.Mode); e != nil {
			return fmt.Errorf("setting file permissions: %w", e)
		}
	}
	m.update(func(s *Status) { s.Phase = "verifying"; s.Message = "Verifying " + p.Game.Name })
	if e := verify(ctx, p, stage, full); e != nil {
		return fmt.Errorf("verification failed; partial retained at %s: %w", stage, e)
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := os.Mkdir(p.Dest, 0755); e != nil {
		return fmt.Errorf("cannot reserve destination: %w", e)
	}
	children, e := os.ReadDir(stage)
	if e != nil {
		return e
	}
	for _, child := range children {
		if e = os.Rename(filepath.Join(stage, child.Name()), filepath.Join(p.Dest, child.Name())); e != nil {
			return fmt.Errorf("could not finalize; partial retained at %s: %w", stage, e)
		}
	}
	if e = os.Remove(stage); e != nil {
		return e
	}
	if e = copyFile(ctx, p.Manifest, p.DestManifest, m); e != nil {
		return fmt.Errorf("game copied but manifest could not be finalized: %w", e)
	}
	h, e := hashFile(p.DestManifest)
	if e != nil || h != p.ManifestHash {
		return fmt.Errorf("destination manifest verification failed")
	}
	return nil
}
func (m *Manager) start(req Request) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return fmt.Errorf("a transfer is already running")
	}
	m.running = true
	m.status = Status{State: "preparing", Phase: "preparing", Message: "Checking files and available space"}
	m.completed = nil
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.mu.Unlock()
	go func() {
		defer func() { m.mu.Lock(); m.running = false; m.cancel = nil; m.mu.Unlock() }()
		plans, _, e := plan(req)
		if e != nil {
			m.update(func(s *Status) { s.State = "error"; s.Message = e.Error() })
			return
		}
		var total int64
		for _, p := range plans {
			total += p.Bytes
		}
		m.update(func(s *Status) { s.State = "running"; s.Phase = "copying"; s.Total = total; s.Message = "Copying" })
		for _, p := range plans {
			m.update(func(s *Status) { s.Phase = "copying"; s.Message = "Copying " + p.Game.Name })
			if e = transfer(ctx, p, req.Full, m); e != nil {
				m.update(func(s *Status) { s.State = "error"; s.Message = e.Error() })
				return
			}
			m.mu.Lock()
			m.completed = append(m.completed, p)
			m.status.Completed++
			m.mu.Unlock()
		}
		m.update(func(s *Status) {
			s.State = "done"
			s.Phase = "complete"
			s.Message = "All selected games copied and verified"
			s.CanDelete = req.Full
		})
	}()
	return nil
}
func (m *Manager) removeSources() error {
	m.mu.Lock()
	if m.running || !m.status.CanDelete || len(m.completed) == 0 {
		m.mu.Unlock()
		return fmt.Errorf("no fully verified transfer available")
	}
	m.running = true
	plans := append([]Plan(nil), m.completed...)
	m.status.CanDelete = false
	m.status.State = "deleting"
	m.status.Phase = "removing"
	m.status.Message = "Rechecking source and destination before removal"
	m.mu.Unlock()
	go func() {
		defer func() { m.mu.Lock(); m.running = false; m.mu.Unlock() }()
		for _, p := range plans {
			if e := verify(context.Background(), p, p.Dest, true); e != nil {
				m.update(func(s *Status) { s.State = "error"; s.Message = "Source retained: " + e.Error() })
				return
			}
			h, e := hashFile(p.DestManifest)
			if e != nil || h != p.ManifestHash {
				m.update(func(s *Status) { s.State = "error"; s.Message = "Source retained: destination manifest differs" })
				return
			}
		}
		for _, p := range plans {
			// Recheck immediately before removing each game: Steam may still be active.
			if e := verify(context.Background(), p, p.Dest, true); e != nil {
				m.update(func(s *Status) { s.State = "error"; s.Message = "Source retained: " + e.Error() })
				return
			}
			h, e := hashFile(p.DestManifest)
			if e != nil || h != p.ManifestHash {
				m.update(func(s *Status) { s.State = "error"; s.Message = "Source retained: destination manifest differs" })
				return
			}
			if e := os.RemoveAll(p.Source); e != nil {
				m.update(func(s *Status) { s.State = "error"; s.Message = "Source removal incomplete: " + e.Error() })
				return
			}
			if e := os.Remove(p.Manifest); e != nil {
				m.update(func(s *Status) {
					s.State = "error"
					s.Message = "Game folder removed but source manifest remains: " + e.Error()
				})
				return
			}
		}
		m.update(func(s *Status) {
			s.State = "done"
			s.Phase = "complete"
			s.Message = "Move completed: verified source games and manifests removed"
		})
	}()
	return nil
}
