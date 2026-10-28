package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestClassifyRegisteredAndExternalLibraries(t *testing.T) {
	root := t.TempDir()
	local := filepath.Join(root, "internal", "steamapps")
	externalRoot := filepath.Join(root, "usb")
	external := filepath.Join(externalRoot, "steamapps")
	backup := filepath.Join(root, "backup", "steamapps")
	for _, p := range []string{local, external, backup} {
		if e := os.MkdirAll(p, 0755); e != nil {
			t.Fatal(e)
		}
	}
	d := classifyLibraries([]string{local, external}, []string{local, external, backup}, []string{externalRoot})
	if len(d.Local) != 1 || d.Local[0] != local || len(d.External) != 2 {
		t.Fatalf("wrong grouping: %+v", d)
	}
}
func TestSelectedSizeAndCancellation(t *testing.T) {
	src, _, game := fixture(t)
	payload, e := os.Stat(filepath.Join(game, "data.bin"))
	if e != nil {
		t.Fatal(e)
	}
	manifest, e := os.Stat(filepath.Join(src, "appmanifest_12345.acf"))
	if e != nil {
		t.Fatal(e)
	}
	selection := []GameSelection{{Source: src, ID: "12345"}}
	n, e := selectedSize(context.Background(), selection)
	if e != nil || n != payload.Size()+manifest.Size() {
		t.Fatalf("wrong size: %d %v", n, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = selectedSize(ctx, selection); e == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestDiscoverBackupAtVolumeRoot(t *testing.T) {
	volume := t.TempDir()
	backup := filepath.Join(volume, "Steam Backup", "steamapps")
	if err := os.MkdirAll(filepath.Join(backup, "common"), 0755); err != nil {
		t.Fatal(err)
	}
	found := discoverPaths(nil, []string{volume})
	if len(found) != 1 || found[0] != backup {
		t.Fatalf("backup not discovered: %v", found)
	}
}
func TestCopyFromMultipleLibraries(t *testing.T) {
	src1, dst, _ := fixture(t)
	src2, _, game2 := fixture(t)
	if e := os.Rename(game2, filepath.Join(src2, "common", "Second Game")); e != nil {
		t.Fatal(e)
	}
	if e := os.Remove(filepath.Join(src2, "appmanifest_12345.acf")); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(src2, "appmanifest_54321.acf"), []byte(`"appid" "54321" "name" "Second Game" "installdir" "Second Game"`), 0644); e != nil {
		t.Fatal(e)
	}
	plans, _, e := plan(Request{Target: dst, Games: []GameSelection{{Source: src1, ID: "12345"}, {Source: src2, ID: "54321"}}, Full: true})
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range plans {
		if e = transfer(context.Background(), p, true, &Manager{}); e != nil {
			t.Fatal(e)
		}
		if e = verify(context.Background(), p, p.Dest, true); e != nil {
			t.Fatal(e)
		}
		if _, e = os.Stat(p.DestManifest); e != nil {
			t.Fatal(e)
		}
		if _, e = os.Stat(p.Source); e != nil {
			t.Fatal("source missing", e)
		}
	}
}
func TestMultiLibraryPlanAndDuplicateProtection(t *testing.T) {
	src1, dst, _ := fixture(t)
	src2, _, game2 := fixture(t)
	if e := os.Rename(game2, filepath.Join(src2, "common", "Second Game")); e != nil {
		t.Fatal(e)
	}
	if e := os.Remove(filepath.Join(src2, "appmanifest_12345.acf")); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(src2, "appmanifest_54321.acf"), []byte(`"AppState" { "appid" "54321" "name" "Second Game" "installdir" "Second Game" }`), 0644); e != nil {
		t.Fatal(e)
	}
	req := Request{Target: dst, Games: []GameSelection{{Source: src1, ID: "12345"}, {Source: src2, ID: "54321"}}}
	plans, _, e := plan(req)
	if e != nil || len(plans) != 2 {
		t.Fatalf("expected two games: %v %v", plans, e)
	}
	req.Games = append(req.Games, req.Games[0])
	if _, _, e = plan(req); e == nil {
		t.Fatal("duplicate selection accepted")
	}
	req.Games = []GameSelection{{Source: src1, ID: "12345"}, {Source: src2, ID: "54321"}}
	if e = os.Rename(filepath.Join(src2, "common", "Second Game"), filepath.Join(src2, "common", "Test Game")); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(src2, "appmanifest_54321.acf"), []byte(`"appid" "54321" "name" "Second Game" "installdir" "Test Game"`), 0644); e != nil {
		t.Fatal(e)
	}
	if _, _, e = plan(req); e == nil {
		t.Fatal("target folder collision accepted")
	}
}
