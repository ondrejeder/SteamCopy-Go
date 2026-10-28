package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//go:embed web/*
var assets embed.FS

type Server struct {
	token   string
	manager Manager
	windows *windowTracker
}

func respond(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, e error) {
	w.WriteHeader(http.StatusBadRequest)
	respond(w, map[string]string{"error": e.Error()})
}
func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
}
func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "POST" || r.Header.Get("X-SteamCopy-Token") != s.token {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	switch r.URL.Path {
	case "/api/window-open", "/api/window-alive", "/api/window-close":
		var q struct {
			ID     string `json:"id"`
			Hidden bool   `json:"hidden"`
		}
		if e := decode(r, &q); e != nil || len(q.ID) < 16 || len(q.ID) > 128 {
			http.Error(w, "invalid window", http.StatusBadRequest)
			return
		}
		now := time.Now()
		switch r.URL.Path {
		case "/api/window-open":
			s.windows.open(q.ID, now)
		case "/api/window-alive":
			s.windows.alive(q.ID, q.Hidden, now)
		case "/api/window-close":
			s.windows.close(q.ID, now)
		}
		respond(w, map[string]bool{"ok": true})
	case "/api/discover":
		respond(w, discover())
	case "/api/discovery":
		respond(w, discoverClassified())
	case "/api/destination-space":
		var q struct {
			Path string `json:"path"`
		}
		if e := decode(r, &q); e != nil {
			fail(w, e)
			return
		}
		free, e := destinationSpace(q.Path)
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, map[string]uint64{"free": free})
	case "/api/presence":
		var q struct {
			Games   []Game   `json:"games"`
			Targets []string `json:"targets"`
		}
		if e := decode(r, &q); e != nil {
			fail(w, e)
			return
		}
		found, e := checkPresence(q.Games, q.Targets)
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, found)
	case "/api/selection-size":
		var q Request
		if e := decode(r, &q); e != nil {
			fail(w, e)
			return
		}
		total, e := selectedSize(r.Context(), q.Games)
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, map[string]int64{"bytes": total})
	case "/api/library":
		var q struct {
			Path string `json:"path"`
		}
		if e := decode(r, &q); e != nil {
			fail(w, e)
			return
		}
		p, e := appsPath(q.Path)
		if e != nil {
			fail(w, e)
			return
		}
		games, e := scan(p)
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, map[string]any{"path": p, "games": games})
	case "/api/browse":
		var q struct {
			Path string `json:"path"`
		}
		if e := decode(r, &q); e != nil {
			fail(w, e)
			return
		}
		if q.Path == "" {
			respond(w, map[string]any{"roots": roots()})
			return
		}
		path, e := filepath.Abs(q.Path)
		if e != nil {
			fail(w, e)
			return
		}
		items, e := os.ReadDir(path)
		if e != nil {
			fail(w, e)
			return
		}
		dirs := []string{}
		for _, d := range items {
			if d.IsDir() && d.Type()&os.ModeSymlink == 0 {
				dirs = append(dirs, d.Name())
			}
		}
		sort.Strings(dirs)
		respond(w, map[string]any{"path": path, "parent": filepath.Dir(path), "dirs": dirs})
	case "/api/scan":
		var q struct {
			Path string `json:"path"`
		}
		if e := decode(r, &q); e != nil {
			fail(w, e)
			return
		}
		g, e := scan(q.Path)
		if e != nil {
			fail(w, e)
			return
		}
		respond(w, g)
	case "/api/preflight":
		var q Request
		if e := decode(r, &q); e != nil {
			fail(w, e)
			return
		}
		p, free, e := plan(q)
		if e != nil {
			fail(w, e)
			return
		}
		var total int64
		for _, g := range p {
			total += g.Bytes
		}
		respond(w, map[string]any{"bytes": total, "free": free, "games": len(p), "reserve": uint64(1 << 30)})
	case "/api/start":
		var q Request
		if e := decode(r, &q); e != nil {
			fail(w, e)
			return
		}
		if e := s.manager.start(q); e != nil {
			fail(w, e)
			return
		}
		respond(w, map[string]bool{"ok": true})
	case "/api/status":
		respond(w, s.manager.snapshot())
	case "/api/cancel":
		s.manager.mu.Lock()
		if s.manager.cancel != nil {
			s.manager.cancel()
		}
		s.manager.mu.Unlock()
		respond(w, map[string]bool{"ok": true})
	case "/api/remove-sources":
		if e := s.manager.removeSources(); e != nil {
			fail(w, e)
			return
		}
		respond(w, map[string]bool{"ok": true})
	default:
		http.NotFound(w, r)
	}
}
func main() {
	ln, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		log.Fatal(e)
	}
	srv := &Server{token: randomID(), windows: newWindowTracker()}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", srv.api)
	sub, _ := fs.Sub(assets, "web")
	static := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			b, e := assets.ReadFile("web/index.html")
			if e != nil {
				http.Error(w, "missing UI", 500)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'")
			_, _ = w.Write([]byte(strings.ReplaceAll(string(b), "__TOKEN__", srv.token)))
			return
		}
		static.ServeHTTP(w, r)
	})
	address := ln.Addr().String()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != address || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "http://"+address) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
	httpServer := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for now := range ticker.C {
			if srv.windows.shouldClose(now) && srv.manager.cancelForExit() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = httpServer.Shutdown(ctx)
				cancel()
				return
			}
		}
	}()
	url := "http://" + ln.Addr().String() + "/"
	fmt.Println("SteamCopy Go:", url)
	if os.Getenv("STEAMCOPY_NO_BROWSER") != "1" {
		openBrowser(url)
	}
	if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
