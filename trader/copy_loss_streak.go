package trader

import (
	"fmt"
	"strings"
	"sync"

	"nofx/events"
	"nofx/logger"
	"nofx/store"
)

var (
	copyLossGuardStore *store.Store
	copyLossStrategyMu sync.Map // strategyID -> *sync.Mutex
)

// InitCopyLossGuard watches completed copy closes and auto-pauses bots that hit
// their pause_loss_streak threshold (e.g. Alpha 6859 at 5 losses in a row).
func InitCopyLossGuard(st *store.Store) {
	copyLossGuardStore = st
	events.AddTradeListener(handleCopyLossStreakTrade)
}

func copyLossStrategyLock(strategyID string) *sync.Mutex {
	if v, ok := copyLossStrategyMu.Load(strategyID); ok {
		return v.(*sync.Mutex)
	}
	mu := &sync.Mutex{}
	actual, _ := copyLossStrategyMu.LoadOrStore(strategyID, mu)
	return actual.(*sync.Mutex)
}

func handleCopyLossStreakTrade(e events.TradeEvent) {
	if copyLossGuardStore == nil || e.TraderID == "" {
		return
	}
	if e.PartialClose || !strings.HasPrefix(e.Action, "close_") {
		return
	}

	tr, err := copyLossGuardStore.Trader().GetByID(e.TraderID)
	if err != nil || tr == nil || strings.TrimSpace(tr.StrategyID) == "" {
		return
	}

	strategy, err := copyLossGuardStore.Strategy().GetByIDAny(tr.StrategyID)
	if err != nil || strategy == nil {
		return
	}
	cfg, err := strategy.ParseConfig()
	if err != nil || cfg == nil || cfg.CopyConfig == nil {
		return
	}
	if strings.TrimSpace(cfg.StrategyType) != "" && cfg.StrategyType != "copy_trading" {
		return
	}
	if cfg.CopyConfig.PauseLossStreak <= 0 {
		return
	}

	mu := copyLossStrategyLock(tr.StrategyID)
	mu.Lock()
	defer mu.Unlock()

	strategy, err = copyLossGuardStore.Strategy().GetByIDAny(tr.StrategyID)
	if err != nil || strategy == nil {
		return
	}
	cfg, err = strategy.ParseConfig()
	if err != nil || cfg == nil || cfg.CopyConfig == nil {
		return
	}

	pauseNow, changed := applyCopyLossClose(cfg.CopyConfig, e.RealizedPnL)
	if !changed {
		return
	}

	if err := strategy.SetConfig(cfg); err != nil {
		logger.Infof("[CopyLossGuard] %s: failed to serialize config: %v", tr.Name, err)
		return
	}
	if err := copyLossGuardStore.Strategy().Update(strategy); err != nil {
		logger.Infof("[CopyLossGuard] %s: failed to persist config: %v", tr.Name, err)
		return
	}

	if pauseNow {
		cc := cfg.CopyConfig
		leader := shortLeaderAddr(cc.LeaderAddress)
		msg := fmt.Sprintf(
			"%s auto-paused after %d losing copies in a row (limit %d). New opens stopped; closes still mirror leader %s. Unpause manually when ready.",
			tr.Name, cc.LossStreak, cc.PauseLossStreak, leader,
		)
		logger.Infof("[CopyLossGuard] %s", msg)
		events.EmitSystemAlert(events.SystemAlertEvent{
			TraderID:   tr.ID,
			TraderName: tr.Name,
			Type:       events.AlertCopyLossPause,
			Message:    msg,
			DedupeKey:  fmt.Sprintf("%s:%s:pause:%d", tr.ID, events.AlertCopyLossPause, cc.LossStreak),
		})
	}
}

// applyCopyLossClose updates streak counters on a completed copy close.
// Breakeven (PnL <= 0) counts as a loss. Returns pauseNow when the bot crosses
// pause_loss_streak and changed when config should be persisted.
func applyCopyLossClose(cc *store.CopyStrategyConfig, realizedPnL float64) (pauseNow, changed bool) {
	if cc == nil || cc.PauseLossStreak <= 0 {
		return false, false
	}
	prevStreak := cc.LossStreak
	if realizedPnL > 0 {
		cc.LossStreak = 0
	} else {
		cc.LossStreak++
	}
	if cc.LossStreak >= cc.PauseLossStreak && !cc.CopyPaused {
		cc.CopyPaused = true
		cc.CopyLayer = 3
		return true, true
	}
	return false, cc.LossStreak != prevStreak
}

func shortLeaderAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if len(addr) <= 12 {
		return addr
	}
	return addr[:6] + "…" + addr[len(addr)-4:]
}
