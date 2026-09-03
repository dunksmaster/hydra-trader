package trader

import (
	"context"
	"fmt"
	"sync"
	"time"

	hl "github.com/sonirico/go-hyperliquid"
	hlprovider "nofx/provider/hyperliquid"
)

const copyFillSeenMax = 5000

// CopyLeaderWatcher streams deduplicated leader fills over a WebSocket.
type CopyLeaderWatcher struct {
	leader   string
	testnet  bool
	fills    chan hlprovider.LeaderFill
	seen     map[int64]struct{}
	seenMu   sync.Mutex
	seenRing []int64
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	logInfo  func(string, ...interface{})
	logError func(string, ...interface{})
	// OnFillDropped is called when the fill channel is full and a live fill is dropped.
	OnFillDropped func(hlprovider.LeaderFill)
}

// NewCopyLeaderWatcher creates a watcher; call Start to connect.
func NewCopyLeaderWatcher(leader string, testnet bool, logInfo, logError func(string, ...interface{})) *CopyLeaderWatcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &CopyLeaderWatcher{
		leader:   leader,
		testnet:  testnet,
		fills:    make(chan hlprovider.LeaderFill, 256),
		seen:     make(map[int64]struct{}, 256),
		ctx:      ctx,
		cancel:   cancel,
		logInfo:  logInfo,
		logError: logError,
	}
}

// Fills returns the channel of live leader fills (after initial snapshot bootstrap).
func (w *CopyLeaderWatcher) Fills() <-chan hlprovider.LeaderFill {
	return w.fills
}

// Start connects to Hyperliquid userFills and reconnects with backoff on failure.
func (w *CopyLeaderWatcher) Start() {
	w.wg.Add(1)
	go w.run()
}

// Stop closes the watcher and waits for the goroutine to exit.
func (w *CopyLeaderWatcher) Stop() {
	w.cancel()
	w.wg.Wait()
}

func (w *CopyLeaderWatcher) run() {
	defer w.wg.Done()
	backoff := time.Second
	for {
		if w.ctx.Err() != nil {
			return
		}
		err := w.connectOnce()
		if w.ctx.Err() != nil {
			return
		}
		if err != nil {
			w.logError("[Copy] leader WS error: %v; reconnect in %v", err, backoff)
			select {
			case <-w.ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
	}
}

func (w *CopyLeaderWatcher) connectOnce() error {
	baseURL := hl.MainnetAPIURL
	if w.testnet {
		baseURL = hl.TestnetAPIURL
	}
	ws := hl.NewWebsocketClient(baseURL)
	if err := ws.Connect(w.ctx); err != nil {
		return fmt.Errorf("ws connect: %w", err)
	}
	defer ws.Close()

	snapshotDone := false
	connDone := make(chan struct{})
	var connOnce sync.Once
	closeConnDone := func() {
		connOnce.Do(func() { close(connDone) })
	}

	_, err := ws.OrderFills(hl.OrderFillsSubscriptionParams{User: w.leader}, func(msg hl.WsOrderFills, cbErr error) {
		if cbErr != nil {
			w.logError("[Copy] userFills callback: %v", cbErr)
			closeConnDone()
			return
		}
		if msg.IsSnapshot {
			for _, f := range msg.Fills {
				w.markSeen(f.Tid)
			}
			snapshotDone = true
			w.logInfo("[Copy] leader WS snapshot: %d fills seeded (dedupe only)", len(msg.Fills))
			return
		}
		if !snapshotDone {
			return
		}
		for _, f := range msg.Fills {
			if w.alreadySeen(f.Tid) {
				continue
			}
			w.markSeen(f.Tid)
			fill, parseErr := hlprovider.ParseLeaderFill(
				f.Coin, f.Dir, f.Side, f.Px, f.Sz, f.ClosedPnl, f.Hash, f.Tid, f.Time,
			)
			if parseErr != nil {
				w.logError("[Copy] skip fill tid=%d: %v", f.Tid, parseErr)
				continue
			}
			select {
			case w.fills <- *fill:
			case <-w.ctx.Done():
				closeConnDone()
				return
			default:
				w.logError("[Copy] fill channel full, dropping tid=%d %s", fill.Tid, fill.Symbol)
				if w.OnFillDropped != nil {
					w.OnFillDropped(*fill)
				}
			}
		}
	})
	if err != nil {
		return fmt.Errorf("subscribe userFills: %w", err)
	}

	w.logInfo("[Copy] leader WS connected for %s", shortAddr(w.leader))
	select {
	case <-w.ctx.Done():
		return nil
	case <-connDone:
		return fmt.Errorf("websocket closed")
	}
}

func (w *CopyLeaderWatcher) alreadySeen(tid int64) bool {
	w.seenMu.Lock()
	defer w.seenMu.Unlock()
	_, ok := w.seen[tid]
	return ok
}

func (w *CopyLeaderWatcher) markSeen(tid int64) {
	if tid == 0 {
		return
	}
	w.seenMu.Lock()
	defer w.seenMu.Unlock()
	if _, ok := w.seen[tid]; ok {
		return
	}
	w.seen[tid] = struct{}{}
	w.seenRing = append(w.seenRing, tid)
	if len(w.seenRing) > copyFillSeenMax {
		oldest := w.seenRing[0]
		w.seenRing = w.seenRing[1:]
		delete(w.seen, oldest)
	}
}
