package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"
)

// ChangeCallback is invoked after every successful snapshot replacement.
// Downstream layers (notably the routing-table builder in `routing`)
// register a callback via Watcher.OnChange to rebuild their derived state
// whenever KV entries change.
type ChangeCallback func()

// Watcher mirrors a handler_registry JetStream KV bucket into a Store,
// keeping it up-to-date via jetstream.KeyValue.Watch().
//
// Lifecycle:
//  1. Start performs a full initial scan (blocking until the first
//     snapshot is loaded), then spawns a background goroutine that
//     consumes the watch channel and applies deltas.
//  2. OnChange registers a callback invoked after every successful
//     Store.Replace. Multiple callbacks may be registered; they are
//     invoked in registration order on the watcher goroutine.
//  3. Stop cancels the watch goroutine's context and waits for it to
//     exit cleanly.
//
// The Watcher is the ONLY writer to the Store. All other components are
// readers, which means the Store's atomic semantics are enforced by
// construction rather than by convention.
type Watcher struct {
	kv        jetstream.KeyValue
	store     *Store
	logger    zerolog.Logger
	callbacks []ChangeCallback
	cbMu      sync.RWMutex

	cancel   context.CancelFunc
	done     chan struct{}
	stopOnce sync.Once
}

// NewWatcher constructs a Watcher for the given KV bucket and store.
// The provided logger is cloned with a "component" field so watcher logs
// can be filtered from the rest of the gateway output.
func NewWatcher(kv jetstream.KeyValue, store *Store, logger zerolog.Logger) *Watcher {
	return &Watcher{
		kv:     kv,
		store:  store,
		logger: logger.With().Str("component", "registry.Watcher").Logger(),
		done:   make(chan struct{}),
	}
}

// OnChange registers a callback invoked after every successful Store
// replacement. Multiple callbacks may be registered; they run on the
// watcher goroutine in registration order.
func (w *Watcher) OnChange(cb ChangeCallback) {
	w.cbMu.Lock()
	defer w.cbMu.Unlock()
	w.callbacks = append(w.callbacks, cb)
}

// Start performs the initial full-scan load and spawns the background
// watch loop. Returns once the initial snapshot has been published to the
// store — the gateway is ready to route requests by the time this call
// returns.
//
// The watch goroutine runs until Stop is called, automatically restarting
// the underlying JetStream watcher with a short backoff if the NATS
// connection drops or the watch channel closes unexpectedly.
func (w *Watcher) Start(ctx context.Context) error {
	if err := w.initialLoad(ctx); err != nil {
		return fmt.Errorf("registry watcher initial load: %w", err)
	}

	watchCtx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel

	go w.watchLoop(watchCtx)
	return nil
}

// Stop cancels the watch loop's context and waits for the goroutine to
// exit. Safe to call multiple times and before Start (the second and
// later calls are no-ops).
func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		if w.cancel == nil {
			return
		}
		w.cancel()
		<-w.done
	})
}

func (w *Watcher) initialLoad(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	keys, err := w.kv.Keys(ctx)
	if err != nil {
		if errors.Is(err, jetstream.ErrNoKeysFound) {
			w.store.Replace(&Snapshot{Entries: map[string]HandlerEntry{}})
			w.fireCallbacks()
			return nil
		}
		return fmt.Errorf("list keys: %w", err)
	}

	entries := make(map[string]HandlerEntry, len(keys))
	for _, key := range keys {
		kve, err := w.kv.Get(ctx, key)
		if err != nil {
			w.logger.Warn().Err(err).Str("key", key).Msg("skipping key on initial load")
			continue
		}
		entry, err := decodeEntry(kve.Value())
		if err != nil {
			w.logger.Warn().Err(err).Str("key", key).Msg("skipping malformed KV value on initial load")
			continue
		}
		entries[key] = entry
	}

	w.store.Replace(&Snapshot{Entries: entries})
	w.fireCallbacks()
	return nil
}

func (w *Watcher) watchLoop(ctx context.Context) {
	defer close(w.done)

	for {
		if err := w.runWatch(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			w.logger.Error().Err(err).Msg("watch loop error; restarting after backoff")
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}
}

func (w *Watcher) runWatch(ctx context.Context) error {
	watcher, err := w.kv.WatchAll(ctx, jetstream.IgnoreDeletes())
	if err != nil {
		return fmt.Errorf("kv WatchAll: %w", err)
	}
	defer func() { _ = watcher.Stop() }()

	// nats.go's JetStream KV watcher sends a nil entry once the initial
	// replay of existing entries completes. We do not discard the replay
	// entries: initialLoad may have run at T0 while a concurrent writer
	// pushed a new value at T1, before WatchAll subscribed at T2. Letting
	// the replay go through applyDelta guarantees eventual consistency —
	// the worst case is a handful of redundant snapshot swaps on startup,
	// which is cheap and race-free because Store.Replace is atomic.
	for {
		select {
		case <-ctx.Done():
			return nil
		case kve, ok := <-watcher.Updates():
			if !ok {
				return fmt.Errorf("watch updates channel closed")
			}
			if kve == nil {
				// End-of-initial-replay marker. Nothing to apply, and we
				// stay subscribed for subsequent live updates.
				continue
			}
			w.applyDelta(kve)
		}
	}
}

func (w *Watcher) applyDelta(kve jetstream.KeyValueEntry) {
	current := w.store.Get()
	key := kve.Key()

	next := make(map[string]HandlerEntry, len(current.Entries)+1)
	for k, v := range current.Entries {
		next[k] = v
	}

	switch kve.Operation() {
	case jetstream.KeyValuePut:
		entry, err := decodeEntry(kve.Value())
		if err != nil {
			w.logger.Warn().Err(err).Str("key", key).Msg("skipping malformed KV update")
			return
		}
		next[key] = entry
	case jetstream.KeyValueDelete, jetstream.KeyValuePurge:
		// Delete and Purge are suppressed by jetstream.IgnoreDeletes() on
		// the watch subscription above, so in steady state this branch is
		// never taken. Keeping it here as defense-in-depth: if the watch
		// is ever reconfigured to deliver deletions, the store will
		// immediately start reflecting them without another code change.
		delete(next, key)
	default:
		// Unknown operation — ignore it instead of swapping in an
		// unchanged snapshot and firing spurious callbacks.
		return
	}

	w.store.Replace(&Snapshot{Entries: next})
	w.fireCallbacks()
}

func (w *Watcher) fireCallbacks() {
	w.cbMu.RLock()
	defer w.cbMu.RUnlock()
	for _, cb := range w.callbacks {
		cb()
	}
}

func decodeEntry(raw []byte) (HandlerEntry, error) {
	var entry HandlerEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return HandlerEntry{}, fmt.Errorf("decode handler entry: %w", err)
	}
	return entry, nil
}
