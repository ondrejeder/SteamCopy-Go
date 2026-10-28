package main

import (
	"context"
	"testing"
	"time"
)

func TestWindowCloseReloadAndMultipleWindows(t *testing.T) {
	t0 := time.Unix(100, 0)
	v := newWindowTracker()
	v.open("window-a", t0)
	v.open("window-b", t0)
	v.close("window-a", t0)
	if v.shouldClose(t0.Add(10 * time.Second)) {
		t.Fatal("closed while another window is open")
	}
	v.close("window-b", t0)
	if v.shouldClose(t0.Add(3 * time.Second)) {
		t.Fatal("reload grace period not respected")
	}
	v.open("reload", t0.Add(3*time.Second))
	if v.shouldClose(t0.Add(10 * time.Second)) {
		t.Fatal("closed after reload")
	}
	v.close("reload", t0.Add(10*time.Second))
	if !v.shouldClose(t0.Add(15 * time.Second)) {
		t.Fatal("did not close after final window")
	}
}
func TestHiddenWindowAndVisibleTimeout(t *testing.T) {
	t0 := time.Unix(100, 0)
	v := newWindowTracker()
	v.open("window-a", t0)
	v.alive("window-a", true, t0)
	if v.shouldClose(t0.Add(10 * time.Minute)) {
		t.Fatal("suspended hidden window was expired")
	}
	v.alive("window-a", false, t0.Add(10*time.Minute))
	if v.shouldClose(t0.Add(10*time.Minute + 91*time.Second)) {
		t.Fatal("missing timeout grace")
	}
	if !v.shouldClose(t0.Add(10*time.Minute + 96*time.Second)) {
		t.Fatal("stale visible window not cleaned up")
	}
}
func TestCloseCancelsTransferAndWaitsForDeletion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{running: true, cancel: cancel}
	if m.cancelForExit() {
		t.Fatal("cannot exit during transfer")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("transfer was not cancelled")
	}
	m.cancel = nil
	if m.cancelForExit() {
		t.Fatal("cannot exit during source deletion")
	}
	m.running = false
	if !m.cancelForExit() {
		t.Fatal("cannot exit after work completed")
	}
}
