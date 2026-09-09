package browser

import (
	"context"
	"sync"
)

// A controller holds the same gate as managed browser commands. Taking control
// never races an in-flight command; later agent commands wait until release.
// Gates are reference counted so finished sessions leave no permanent entries.
type liveGate struct {
	token chan struct{}
	refs  int
}

var liveGates = struct {
	sync.Mutex
	entries map[string]*liveGate
}{entries: map[string]*liveGate{}}

func retainLiveGate(session string) (*liveGate, func()) {
	liveGates.Lock()
	gate := liveGates.entries[session]
	if gate == nil {
		gate = &liveGate{token: make(chan struct{}, 1)}
		gate.token <- struct{}{}
		liveGates.entries[session] = gate
	}
	gate.refs++
	liveGates.Unlock()
	return gate, func() {
		liveGates.Lock()
		defer liveGates.Unlock()
		gate.refs--
		if gate.refs == 0 {
			delete(liveGates.entries, session)
		}
	}
}

func AcquireBrowserAutomation(ctx context.Context, session string) (func(), error) {
	gate, drop := retainLiveGate(session)
	select {
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	case <-gate.token:
		var once sync.Once
		return func() { once.Do(func() { gate.token <- struct{}{}; drop() }) }, nil
	}
}

func TryTakeBrowserControl(session string) (func(), bool) {
	gate, drop := retainLiveGate(session)
	select {
	case <-gate.token:
		var once sync.Once
		return func() { once.Do(func() { gate.token <- struct{}{}; drop() }) }, true
	default:
		drop()
		return nil, false
	}
}
