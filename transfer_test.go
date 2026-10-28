package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(root, "source", "steamapps")
	dst := filepath.Join(root, "backup", "steamapps")
	game := filepath.Join(src, "common", "Test Game")
	if e := os.MkdirAll(filepath.Join(game, "empty"), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(game, "data.bin"), []byte("original game data"), 0644); e != nil {
		t.Fatal(e)
	}
	manifest := `"AppState" { "appid" "12345" "name" "Test Game" "installdir" "Test Game" "SizeOnDisk" "18" }`
	if e := os.WriteFile(filepath.Join(src, "appmanifest_12345.acf"), []byte(manifest), 0644); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(filepath.Dir(dst), 0755); e != nil {
		t.Fatal(e)
	}
	return src, dst, game
}

func TestMoveManagerRequiresVerifiedCopyAndDeletesOnlyFixture(t *testing.T) {
	src, dst, game := fixture(t)
	m := &Manager{}
	if e := m.removeSources(); e == nil {
		t.Fatal("removal allowed without verified copy")
	}
	if e := m.start(Request{Source: src, Target: dst, IDs: []string{"12345"}, Full: true}); e != nil {
		t.Fatal(e)
	}
	waitManager := func(want string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			m.mu.Lock()
			state, running, msg := m.status.State, m.running, m.status.Message
			m.mu.Unlock()
			if !running {
				if state != want {
					t.Fatalf("state %s: %s", state, msg)
				}
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("manager timed out")
	}
	waitManager("done")
	if _, e := os.Stat(filepath.Join(game, "data.bin")); e != nil {
		t.Fatal("source removed without confirmation", e)
	}
	if e := m.removeSources(); e != nil {
		t.Fatal(e)
	}
	waitManager("done")
	if _, e := os.Stat(game); !os.IsNotExist(e) {
		t.Fatal("fixture source remains", e)
	}
	if _, e := os.Stat(filepath.Join(dst, "common", "Test Game", "data.bin")); e != nil {
		t.Fatal("destination missing", e)
	}
	if _, e := os.Stat(filepath.Join(dst, "appmanifest_12345.acf")); e != nil {
		t.Fatal("destination manifest missing", e)
	}
}

func TestMoveRetainsChangedSource(t *testing.T) {
	for _, mutateManifest := range []bool{false, true} {
		t.Run(fmt.Sprintf("manifest=%t", mutateManifest), func(t *testing.T) {
			src, dst, game := fixture(t)
			plans, _, err := plan(Request{Source: src, Target: dst, IDs: []string{"12345"}, Full: true})
			if err != nil {
				t.Fatal(err)
			}
			m := &Manager{}
			if err = transfer(context.Background(), plans[0], true, m); err != nil {
				t.Fatal(err)
			}
			if mutateManifest {
				err = os.WriteFile(plans[0].Manifest, []byte("changed manifest"), 0644)
			} else {
				err = os.WriteFile(filepath.Join(game, "new.dat"), []byte("new"), 0644)
			}
			if err != nil {
				t.Fatal(err)
			}
			m.completed = plans
			m.status.CanDelete = true
			if err = m.removeSources(); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				m.mu.Lock()
				running, state := m.running, m.status.State
				m.mu.Unlock()
				if !running {
					if state != "error" {
						t.Fatalf("expected error, got %s", state)
					}
					if _, err = os.Stat(game); err != nil {
						t.Fatal("source was removed", err)
					}
					if _, err = os.Stat(plans[0].Manifest); err != nil {
						t.Fatal("manifest was removed", err)
					}
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("manager timed out")
		})
	}
}
func TestTransferAndCollision(t *testing.T) {
	src, dst, game := fixture(t)
	req := Request{Source: src, Target: dst, IDs: []string{"12345"}, Full: true}
	plans, _, e := plan(req)
	if e != nil {
		t.Fatal(e)
	}
	m := &Manager{}
	if e = transfer(context.Background(), plans[0], true, m); e != nil {
		t.Fatal(e)
	}
	if e = verify(context.Background(), plans[0], plans[0].Dest, true); e != nil {
		t.Fatal(e)
	}
	a, e := hashFile(plans[0].Manifest)
	if e != nil {
		t.Fatal(e)
	}
	b, e := hashFile(plans[0].DestManifest)
	if e != nil || a != b {
		t.Fatal("manifest differs", e)
	}
	if _, e = os.Stat(filepath.Join(game, "data.bin")); e != nil {
		t.Fatal("source lost", e)
	}
	if _, _, e = plan(req); e == nil {
		t.Fatal("expected collision rejection")
	}
	if e = os.WriteFile(filepath.Join(plans[0].Dest, "data.bin"), []byte("different game dat"), 0644); e != nil {
		t.Fatal(e)
	}
	if e = verify(context.Background(), plans[0], plans[0].Dest, true); e == nil {
		t.Fatal("expected content mismatch")
	}
}
func TestSourceChangeAndCancellation(t *testing.T) {
	src, dst, game := fixture(t)
	plans, _, e := plan(Request{Source: src, Target: dst, IDs: []string{"12345"}})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(game, "new.dat"), []byte("new"), 0644); e != nil {
		t.Fatal(e)
	}
	if e = transfer(context.Background(), plans[0], false, &Manager{}); e == nil || !strings.Contains(e.Error(), "source") {
		t.Fatalf("expected changed source error, got %v", e)
	}
	if _, e = os.Stat(plans[0].DestManifest); !os.IsNotExist(e) {
		t.Fatal("manifest must not be published")
	}
	if _, e = os.Stat(filepath.Join(game, "new.dat")); e != nil {
		t.Fatal("source lost", e)
	}
	src2, dst2, game2 := fixture(t)
	p, _, e := plan(Request{Source: src2, Target: dst2, IDs: []string{"12345"}})
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e = transfer(ctx, p[0], true, &Manager{}); e == nil {
		t.Fatal("expected cancellation")
	}
	if _, e = os.Stat(filepath.Join(game2, "data.bin")); e != nil {
		t.Fatal("source lost", e)
	}
	if _, e = os.Stat(p[0].DestManifest); !os.IsNotExist(e) {
		t.Fatal("cancelled transfer published manifest")
	}
}
func TestManifestAndOverlap(t *testing.T) {
	src, _, _ := fixture(t)
	g, e := scan(src)
	if e != nil || len(g) != 1 || g[0].ID != "12345" || !g[0].Ready {
		t.Fatalf("scan: %+v %v", g, e)
	}
	if _, _, e = plan(Request{Source: src, Target: src, IDs: []string{"12345"}}); e == nil {
		t.Fatal("same library accepted")
	}
	bad := filepath.Join(src, "appmanifest_99.acf")
	if e = os.WriteFile(bad, []byte(fmt.Sprintf(`"appid" "99" "name" "bad" "installdir" "%s"`, `..\outside`)), 0644); e != nil {
		t.Fatal(e)
	}
	g, e = scan(src)
	if e != nil || len(g) != 1 {
		t.Fatalf("unsafe directory accepted: %+v %v", g, e)
	}
}
