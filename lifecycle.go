package main

import (
	"sync"
	"time"
)

// A closed window is given time to reconnect on reload. Hidden tabs are not
// expired: browsers can suspend them for an arbitrarily long time.
type windowState struct {
	last   time.Time
	hidden bool
}
type windowTracker struct {
	mu         sync.Mutex
	windows    map[string]windowState
	closed     map[string]bool
	seen       bool
	emptySince time.Time
	created    time.Time
}

func newWindowTracker() *windowTracker {
	return &windowTracker{windows: map[string]windowState{}, closed: map[string]bool{}, created: time.Now()}
}
func (t *windowTracker) open(id string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed[id] {
		return
	}
	t.seen = true
	t.windows[id] = windowState{last: now}
	t.emptySince = time.Time{}
}
func (t *windowTracker) alive(id string, hidden bool, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.windows[id]; ok && !t.closed[id] {
		t.windows[id] = windowState{last: now, hidden: hidden}
	}
}
func (t *windowTracker) close(id string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.seen {
		return
	}
	if _, ok := t.windows[id]; !ok {
		return
	}
	t.closed[id] = true
	delete(t.windows, id)
	if len(t.windows) == 0 {
		t.emptySince = now
	}
}
func (t *windowTracker) shouldClose(now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, state := range t.windows {
		if !state.hidden && now.Sub(state.last) > 90*time.Second {
			delete(t.windows, id)
			t.closed[id] = true
		}
	}
	if !t.seen {
		return now.Sub(t.created) >= 2*time.Minute
	}
	if len(t.windows) > 0 {
		return false
	}
	if t.emptySince.IsZero() {
		t.emptySince = now
		return false
	}
	return now.Sub(t.emptySince) >= 4*time.Second
}
func (m *Manager) cancelForExit() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return true
	}
	if m.cancel != nil {
		m.cancel()
	}
	return false // source deletion cannot be interrupted safely
}
