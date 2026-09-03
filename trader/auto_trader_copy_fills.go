package trader

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"nofx/events"
	"nofx/kernel"
	"nofx/market"
	hlprovider "nofx/provider/hyperliquid"
	"nofx/store"
)

const (
	copyMinHLNotionalUSD = 10.0
	copyDriftGracePeriod = 90 * time.Second
	copyMarginOverhead   = 1.01
	copyTakerFeeRate     = 0.001
)

func requiredCopyMarginUSD(notional float64, leverage int) float64 {
	if leverage <= 0 {
		leverage = 10
	}
	return notional * (copyMarginOverhead/float64(leverage) + copyTakerFeeRate)
}

// isCopyWatchOnly is L3 + paused + dry_run: leader stream on, no trades, no skip spam.
func isCopyWatchOnly(cfg *store.CopyStrategyConfig) bool {
	if cfg == nil {
		return false
	}
	return cfg.CopyLayer >= 3 && cfg.CopyPaused && cfg.DryRun
}

// suppressCopyNoiseAlerts is true when the bot is not taking new opens (paused / L3 waitlist).
func suppressCopyNoiseAlerts(cfg *store.CopyStrategyConfig) bool {
	if cfg == nil {
		return false
	}
	return cfg.CopyPaused || cfg.CopyLayer >= 3 || isCopyWatchOnly(cfg)
}

func classifyCopySkipCategory(reason string) string {
	r := strings.ToLower(reason)
	switch {
	case strings.Contains(r, "below minimum"), strings.Contains(r, "margin"), strings.Contains(r, "insufficient"):
		return "margin"
	case strings.Contains(r, "max positions"), strings.Contains(r, "max_positions"):
		return "max_positions"
	case strings.Contains(r, "blocked"):
		return "blocked"
	case strings.Contains(r, "already has"):
		return "already_open"
	default:
		return "other"
	}
}

// IsCopyFillsMode returns true when copy trading mirrors leader fills in real time.
func (at *AutoTrader) IsCopyFillsMode() bool {
	if !at.IsCopyStrategy() || at.config.StrategyConfig.CopyConfig == nil {
		return false
	}
	mode := strings.ToLower(strings.TrimSpace(at.config.StrategyConfig.CopyConfig.CopyMode))
	return mode != "snapshot"
}

func (at *AutoTrader) startCopyLeaderWatcher() {
	if at.copyWatcher != nil {
		at.logWarnf("[Copy] leader watcher already running; skipping duplicate start")
		return
	}
	cfg := at.config.StrategyConfig.CopyConfig
	if cfg == nil || cfg.LeaderAddress == "" {
		return
	}
	testnet := at.config.HyperliquidTestnet
	at.copyWatcher = NewCopyLeaderWatcher(cfg.LeaderAddress, testnet, at.logInfof, at.logErrorf)
	at.copyWatcher.OnFillDropped = func(fill hlprovider.LeaderFill) {
		// Paused / L3 waitlist bots keep the leader stream but must not spam Telegram.
		if cc := at.config.StrategyConfig.CopyConfig; suppressCopyNoiseAlerts(cc) {
			at.logInfof("[Copy] paused drop (no alert) %s tid=%d $%.0f", fill.Symbol, fill.Tid, fill.NotionalUSD)
			return
		}
		at.emitCopyFillDroppedAlert(fill)
	}
	at.copyWatcher.Start()
	at.copyWg.Add(1)
	go func() {
		defer at.copyWg.Done()
		for fill := range at.copyWatcher.Fills() {
			if err := at.handleLeaderFill(fill); err != nil {
				at.logErrorf("[Copy] FILL mirror error: %v", err)
			}
		}
	}()
}

func (at *AutoTrader) stopCopyLeaderWatcher() {
	if at.copyWatcher != nil {
		at.copyWatcher.Stop()
		at.copyWatcher = nil
	}
	at.copyWg.Wait()
}

func (at *AutoTrader) runCopyFillsLoop(reconcileInterval time.Duration) error {
	if reconcileInterval <= 0 {
		reconcileInterval = 60 * time.Second
	}
	reconcileTicker := time.NewTicker(reconcileInterval)
	defer reconcileTicker.Stop()

	for {
		at.isRunningMutex.RLock()
		running := at.isRunning
		at.isRunningMutex.RUnlock()
		if !running {
			break
		}

		select {
		case <-at.stopMonitorCh:
			at.stopCopyLeaderWatcher()
			return nil
		case <-reconcileTicker.C:
			if err := at.RunCopyDriftReconcile(); err != nil {
				at.logErrorf("❌ Copy drift reconcile failed: %v", err)
			}
		}
	}
	return nil
}

// RunCopyDriftReconcile corrects persistent position drift without re-firing immediate opens/closes.
func (at *AutoTrader) RunCopyDriftReconcile() error {
	at.copyMu.Lock()
	defer at.copyMu.Unlock()

	at.isRunningMutex.RLock()
	running := at.isRunning
	at.isRunningMutex.RUnlock()
	if !running {
		return nil
	}
	if err := at.reloadStrategyConfigIfChanged(); err != nil {
		return err
	}
	if !at.IsCopyStrategy() || at.exchange != "hyperliquid" {
		return nil
	}
	cfg := at.config.StrategyConfig.CopyConfig
	if cfg == nil || cfg.LeaderAddress == "" {
		return nil
	}
	cfg.Normalize()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	leader, err := hlprovider.FetchAccountStateAll(ctx, cfg.LeaderAddress)
	if err != nil {
		return fmt.Errorf("fetch leader state: %w", err)
	}

	followerEquity, availableBalance, err := at.copyFollowerBalances()
	if err != nil {
		return err
	}

	positions, err := at.trader.GetPositions()
	if err != nil {
		return fmt.Errorf("fetch follower positions: %w", err)
	}

	maxPositions := cfg.MaxPositions
	if maxPositions <= 0 {
		maxPositions = 2
	}

	targets := buildCopyTargets(cfg, leader, followerEquity, availableBalance)
	followerMap := mapFollowerPositions(positions)
	closes, opens := diffCopyLegs(targets, followerMap, countAllWalletLegs(positions), at.copyWalletSlots(), maxPositions)
	if !cfg.CopyOnStart {
		opens = nil // new-fills-only: reconcile closes drift but never opens existing leader legs
	}

	now := time.Now()
	if at.copyDriftSince == nil {
		at.copyDriftSince = make(map[copyLegKey]time.Time)
	}
	if at.copyExtraSince == nil {
		at.copyExtraSince = make(map[copyLegKey]time.Time)
	}

	execOpens, execCloses, nextDrift, nextExtra := filterDriftCopyLegs(opens, closes, at.copyDriftSince, at.copyExtraSince, now, copyDriftGracePeriod)
	at.copyDriftSince = nextDrift
	at.copyExtraSince = nextExtra

	if len(execOpens) == 0 && len(execCloses) == 0 {
		return nil
	}

	at.logInfof("[Copy] drift reconcile: opens=%d closes=%d (grace=%v)", len(execOpens), len(execCloses), copyDriftGracePeriod)

	record := &store.DecisionRecord{
		TraderID:     at.id,
		CycleNumber:  at.cycleNumber + 1,
		Timestamp:    now.UTC(),
		SystemPrompt: fmt.Sprintf("copy_drift_reconcile leader=%s dry_run=%v", cfg.LeaderAddress, cfg.DryRun),
		Success:      true,
	}
	at.executeCopyLegActions(cfg, execCloses, execOpens, record)
	at.cycleNumber = record.CycleNumber
	if at.store != nil {
		if err := at.store.Decision().LogDecision(record); err != nil {
			at.logErrorf("[Copy] Failed to save drift reconcile record: %v", err)
		}
	}
	return nil
}

func (at *AutoTrader) handleLeaderFill(fill hlprovider.LeaderFill) error {
	at.copyMu.Lock()
	defer at.copyMu.Unlock()

	at.isRunningMutex.RLock()
	running := at.isRunning
	at.isRunningMutex.RUnlock()
	if !running {
		return nil
	}
	if err := at.reloadStrategyConfigIfChanged(); err != nil {
		return err
	}
	if !at.IsCopyStrategy() || at.exchange != "hyperliquid" {
		return fmt.Errorf("copy trading requires Hyperliquid exchange")
	}
	cfg := at.config.StrategyConfig.CopyConfig
	if cfg == nil {
		return fmt.Errorf("copy configuration not found")
	}
	cfg.Normalize()

	// Paused / L3 / watch-only: keep leader stream but do not trade or alert.
	if suppressCopyNoiseAlerts(cfg) {
		return nil
	}

	// HIP-3 deployer perps (@156, etc.) must not get a USDT suffix — skip silently.
	if strings.HasPrefix(strings.TrimSpace(fill.Coin), "@") {
		at.logInfof("[Copy] FILL skip HIP-3 perp %s tid=%d", fill.Coin, fill.Tid)
		return nil
	}

	if fill.NotionalUSD < cfg.MinLeaderFillUSD {
		return nil
	}
	if isCopySymbolBlocked(cfg, fill.Coin, fill.Symbol) {
		at.logInfof("[Copy] FILL skip blocked symbol %s tid=%d", fill.Symbol, fill.Tid)
		return nil
	}

	action := fill.Action
	if cfg.Inverse {
		action = flipCopyAction(action)
	}

	symbol := market.Normalize(fill.Symbol)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	leader, err := hlprovider.FetchAccountStateAll(ctx, cfg.LeaderAddress)
	if err != nil {
		return fmt.Errorf("fetch leader equity: %w", err)
	}
	followerEquity, availableBalance, err := at.copyFollowerBalances()
	if err != nil {
		return err
	}

	decision := &kernel.Decision{
		Symbol:     symbol,
		Action:     action,
		Confidence: 100,
		Reasoning: fmt.Sprintf("[Copy] FILL mirror leader %s %s $%.2f tid=%d",
			fill.Coin, fill.Action, fill.NotionalUSD, fill.Tid),
	}

	switch {
	case strings.HasPrefix(action, "open_"):
		if cfg.CopyPaused || cfg.CopyLayer >= 3 {
			at.logInfof("[Copy] FILL skip open %s: copy_paused (L%d)", symbol, cfg.CopyLayer)
			if !isCopyWatchOnly(cfg) {
				at.emitCopyPausedSkipAlert(fill, symbol, action)
			}
			return nil
		}
		if positions, posErr := at.trader.GetPositions(); posErr == nil && followerHasSide(positions, symbol, action) {
			at.logInfof("[Copy] FILL skip open %s: follower already has position", symbol)
			at.emitCopySkipAlert(fill, symbol, action, "follower already has position", "already_open", availableBalance)
			at.enqueueOverflowOpen(fill, symbol, action, "already_open")
			return nil
		}
		notional, skipReason := ComputeCopyNotionalUSD(cfg, fill.NotionalUSD, leader.Equity, followerEquity, availableBalance)
		if skipReason != "" {
			cat := classifyCopySkipCategory(skipReason)
			at.logInfof("[Copy] FILL skip open %s: %s", symbol, skipReason)
			at.emitCopySkipAlert(fill, symbol, action, skipReason, cat, availableBalance)
			at.enqueueOverflowOpen(fill, symbol, action, cat)
			if cat == "margin" {
				at.maybeEmitLiquidationRisk()
			}
			return nil
		}
		lev := cfg.MaxLeverage
		if lev <= 0 {
			lev = 10
		}
		reqMargin := requiredCopyMarginUSD(notional, lev)
		positions, posErr := at.trader.GetPositions()
		walletSlots := at.copyWalletSlots()
		openLegCount := 0
		if posErr == nil {
			openLegCount = countAllWalletLegs(positions)
		}
		hasFreeSlot := openLegCount < walletSlots
		hasMargin := reqMargin <= availableBalance
		layer := at.copyLayer()

		switch layer {
		case 1:
			if !hasFreeSlot || !hasMargin {
				if posErr != nil {
					reason := fmt.Sprintf("positions unavailable: %v", posErr)
					at.logInfof("[Copy] FILL skip open %s: %s", symbol, reason)
					at.emitCopySkipAlert(fill, symbol, action, reason, "other", availableBalance)
					return nil
				}
				evicted, evictErr := at.evictL2ForL1Open(ctx, fill, symbol, action, positions, availableBalance, notional, lev)
				if evictErr != nil {
					return evictErr
				}
				if !evicted {
					reason := fmt.Sprintf("L1 needs slot/margin (%d/%d legs, margin $%.2f avail)", openLegCount, walletSlots, availableBalance)
					at.logInfof("[Copy] FILL skip open %s: %s", symbol, reason)
					at.emitCopySkipAlert(fill, symbol, action, reason, "max_positions", availableBalance)
					at.enqueueOverflowOpen(fill, symbol, action, "max_positions")
					return nil
				}
				followerEquity, availableBalance, err = at.copyFollowerBalances()
				if err != nil {
					return err
				}
				if reqMargin > availableBalance {
					reason := fmt.Sprintf("insufficient margin after eviction: need $%.2f, available $%.2f", reqMargin, availableBalance)
					at.logInfof("[Copy] FILL skip open %s: %s", symbol, reason)
					at.emitCopySkipAlert(fill, symbol, action, reason, "margin", availableBalance)
					at.enqueueOverflowOpen(fill, symbol, action, "margin")
					return nil
				}
			}
		default: // L2 and legacy bots
			if !hasFreeSlot {
				reason := fmt.Sprintf("wallet slots full (%d/%d)", openLegCount, walletSlots)
				at.logInfof("[Copy] FILL skip open %s: %s", symbol, reason)
				at.emitCopySkipAlert(fill, symbol, action, reason, "max_positions", availableBalance)
				at.enqueueOverflowOpen(fill, symbol, action, "max_positions")
				return nil
			}
			if !hasMargin {
				reason := fmt.Sprintf("insufficient margin: need $%.2f, available $%.2f", reqMargin, availableBalance)
				at.logInfof("[Copy] FILL skip open %s: %s", symbol, reason)
				at.emitCopySkipAlert(fill, symbol, action, reason, "margin", availableBalance)
				at.enqueueOverflowOpen(fill, symbol, action, "margin")
				at.maybeEmitLiquidationRisk()
				return nil
			}
		}
		decision.PositionSizeUSD = notional
		decision.Leverage = lev
		if price, priceErr := at.trader.GetMarketPrice(symbol); priceErr == nil && price > 0 {
			applyCopyProtectivePrices(decision, price, cfg)
		}
	case strings.HasPrefix(action, "close_"):
		if cfg.CopyPaused || cfg.CopyLayer >= 3 {
			at.logInfof("[Copy] FILL skip close %s: copy_paused (L%d)", symbol, cfg.CopyLayer)
			return nil
		}
		if at.copyActionCoolingDown(symbol, action, time.Now()) {
			at.logInfof("[Copy] FILL skip close %s %s: cooling down after transient HL error", symbol, action)
			return nil
		}
		closeQty, skipReason := computeCopyCloseQty(cfg, fill, leader.Equity, followerEquity, at.trader, symbol, action)
		if skipReason != "" {
			at.logInfof("[Copy] FILL skip close %s: %s", symbol, skipReason)
			at.enqueueOverflowClose(fill, symbol, action)
			return nil
		}
		decision.Quantity = closeQty
		at.emitCopyLeaderRuleAlert(symbol, action, "Hyperliquid", "leader close fill")
		at.enqueueOverflowClose(fill, symbol, action)
	default:
		return nil
	}

	msg := fmt.Sprintf("[Copy] FILL mirror %s %s tid=%d %s",
		map[bool]string{true: "DRY-RUN", false: "EXEC"}[cfg.DryRun], action, fill.Tid, decision.Reasoning)
	at.logInfof(msg)

	actionRecord := store.DecisionAction{
		Action:    action,
		Symbol:    symbol,
		Leverage:  decision.Leverage,
		Reasoning: decision.Reasoning,
		Timestamp: time.Now().UTC(),
		Success:   true,
	}
	if cfg.DryRun {
		return nil
	}
	if err := at.executeCopyDecisionWithRetry(decision, &actionRecord); err != nil {
		if isTransientCopyError(err) {
			at.markCopyTransientFailure(symbol, action, time.Now())
			at.logInfof("[Copy] FILL mirror transient error %s %s: %v", symbol, action, err)
		} else {
			at.emitCopyFailAlert(fill, symbol, action, err)
		}
		return err
	}
	if at.copyTransientUntil != nil {
		delete(at.copyTransientUntil, copyTransientKey(symbol, action))
	}
	if strings.HasPrefix(action, "open_") {
		at.maybeEmitLiquidationRisk()
		if cfg.OverflowParallel {
			at.enqueueOverflowOpen(fill, symbol, action, "parallel")
		}
	}
	// Open/close Telegram alerts come from TradeEvent after fill — no duplicate mirror alert.
	return nil
}

func (at *AutoTrader) emitCopySkipAlert(fill hlprovider.LeaderFill, symbol, action, reason, category string, availableBalance float64) {
	leader := ""
	if at.config.StrategyConfig.CopyConfig != nil {
		leader = at.config.StrategyConfig.CopyConfig.LeaderAddress
	}
	msg := fmt.Sprintf("Leader %s → %s %s skipped (%s). Available $%.2f",
		shortCopyAddr(leader), symbol, action, reason, availableBalance)
	// One skip alert per issue category per bot (e.g. margin), at most every 30 min in Telegram.
	dedupeKey := fmt.Sprintf("%s:%s:%s", at.id, events.AlertCopySkipped, category)
	events.EmitSystemAlert(events.SystemAlertEvent{
		TraderID:   at.id,
		TraderName: at.name,
		Type:       events.AlertCopySkipped,
		Message:    msg,
		DedupeKey:  dedupeKey,
	})
}

func (at *AutoTrader) emitCopyPausedSkipAlert(fill hlprovider.LeaderFill, symbol, action string) {
	leader := ""
	layer := 3
	if at.config.StrategyConfig.CopyConfig != nil {
		leader = at.config.StrategyConfig.CopyConfig.LeaderAddress
		layer = at.config.StrategyConfig.CopyConfig.CopyLayer
	}
	msg := fmt.Sprintf("Leader %s → %s %s skipped (L%d PAUSED — no new opens)", shortCopyAddr(leader), symbol, action, layer)
	dedupeKey := fmt.Sprintf("%s:%s:paused", at.id, events.AlertCopyPaused)
	events.EmitSystemAlert(events.SystemAlertEvent{
		TraderID:   at.id,
		TraderName: at.name,
		Type:       events.AlertCopyPaused,
		Message:    msg,
		DedupeKey:  dedupeKey,
	})
}

func (at *AutoTrader) emitCopyFillDroppedAlert(fill hlprovider.LeaderFill) {
	if at.config.StrategyConfig != nil && suppressCopyNoiseAlerts(at.config.StrategyConfig.CopyConfig) {
		return
	}
	leader := ""
	if at.config.StrategyConfig.CopyConfig != nil {
		leader = at.config.StrategyConfig.CopyConfig.LeaderAddress
	}
	msg := fmt.Sprintf("Leader %s fill dropped (channel full): %s tid=%d $%.0f",
		shortCopyAddr(leader), fill.Symbol, fill.Tid, fill.NotionalUSD)
	dedupeKey := fmt.Sprintf("%s:%s:fill_dropped", at.id, events.AlertCopyFailed)
	events.EmitSystemAlert(events.SystemAlertEvent{
		TraderID:   at.id,
		TraderName: at.name,
		Type:       events.AlertCopyFailed,
		Message:    msg,
		DedupeKey:  dedupeKey,
	})
}

func shortCopyAddr(a string) string {
	a = strings.TrimSpace(a)
	if len(a) <= 12 {
		return a
	}
	return a[:6] + "..." + a[len(a)-4:]
}

func computeCopyCloseQty(cfg *store.CopyStrategyConfig, fill hlprovider.LeaderFill, leaderEquity, followerEquity float64, tr Trader, symbol, action string) (float64, string) {
	scale := 1.0
	if cfg.SizeMode == "proportional" {
		if leaderEquity <= 0 {
			return 0, "leader equity unavailable"
		}
		scale = (followerEquity / leaderEquity) * cfg.CopyRatio
	} else if cfg.NotionalUSD > 0 && fill.NotionalUSD > 0 {
		scale = cfg.NotionalUSD / fill.NotionalUSD
	}
	qty := fill.Size * scale
	if qty <= 0 {
		return 0, "scaled qty is zero"
	}
	if tr == nil {
		return qty, ""
	}

	positions, err := tr.GetPositions()
	if err != nil {
		return qty, ""
	}
	wantSide := "long"
	if action == "close_short" {
		wantSide = "short"
	}
	var held float64
	for _, pos := range positions {
		ps, _ := pos["symbol"].(string)
		side, _ := pos["side"].(string)
		if market.Normalize(ps) != symbol || strings.ToLower(side) != wantSide {
			continue
		}
		if amt, ok := pos["positionAmt"].(float64); ok {
			held = amt
			if held < 0 {
				held = -held
			}
		}
		break
	}
	if held <= 0 {
		return 0, "no follower position to close"
	}

	// Close entire leg when leader closes full size or scaled qty exceeds held.
	if qty >= held*0.995 || qty > held {
		return 0, ""
	}

	if formatted, err := tr.FormatQuantity(symbol, qty); err == nil {
		if parsed, err := strconv.ParseFloat(formatted, 64); err == nil && parsed > 0 {
			qty = parsed
		}
	}
	if qty >= held*0.995 || qty > held {
		return 0, ""
	}

	refPrice := fill.Price
	if refPrice <= 0 {
		refPrice = 1
	}
	// Dust partial closes leave orphan legs — close all instead of skipping.
	if qty*refPrice < copyMinHLNotionalUSD {
		return 0, ""
	}
	if qty <= 0 {
		return 0, ""
	}
	return qty, ""
}

func (at *AutoTrader) copyFollowerBalances() (equity, available float64, err error) {
	balance, err := at.trader.GetBalance()
	if err != nil {
		if eq, avail, ok := at.copyFollowerBalancesFallback(err); ok {
			return eq, avail, nil
		}
		return 0, 0, fmt.Errorf("fetch follower balance: %w", err)
	}
	if v, ok := balance["totalWalletBalance"].(float64); ok {
		equity = v
	} else if v, ok := balance["totalEquity"].(float64); ok {
		equity = v
	}
	available = equity
	if v, ok := balance["availableBalance"].(float64); ok && v > 0 {
		available = v
	}
	return equity, available, nil
}

// copyFollowerBalancesFallback derives equity and available margin from the perp
// margin summary. GetBalance refuses to report a balance while unified Spot hold
// is unavailable and margin is in use, which is exactly the state in which drift
// reconcile most needs to run, so a degraded number beats no number at all.
func (at *AutoTrader) copyFollowerBalancesFallback(cause error) (equity, available float64, ok bool) {
	summarizer, hasSummary := at.trader.(interface {
		GetPerpAccountSummary() (float64, float64, error)
	})
	if !hasSummary {
		return 0, 0, false
	}
	accountValue, marginUsed, err := summarizer.GetPerpAccountSummary()
	if err != nil || accountValue <= 0 {
		return 0, 0, false
	}
	available = accountValue - marginUsed
	if available < 0 {
		available = 0
	}
	at.logInfof("[Copy] follower balance fallback: equity $%.2f available $%.2f (GetBalance unavailable: %v)",
		accountValue, available, cause)
	return accountValue, available, true
}

func flipCopyAction(action string) string {
	switch action {
	case "open_long":
		return "open_short"
	case "open_short":
		return "open_long"
	case "close_long":
		return "close_short"
	case "close_short":
		return "close_long"
	default:
		return action
	}
}

func followerHasSide(positions []map[string]interface{}, symbol, action string) bool {
	wantSide := "long"
	if action == "open_short" || action == "close_short" {
		wantSide = "short"
	}
	for _, pos := range positions {
		ps, _ := pos["symbol"].(string)
		side, _ := pos["side"].(string)
		if market.Normalize(ps) == market.Normalize(symbol) && strings.ToLower(side) == wantSide {
			return true
		}
	}
	return false
}

func followerHasAnyPosition(positions []map[string]interface{}, symbol string) bool {
	want := market.Normalize(symbol)
	for _, pos := range positions {
		ps, _ := pos["symbol"].(string)
		if market.Normalize(ps) == want {
			return true
		}
	}
	return false
}
