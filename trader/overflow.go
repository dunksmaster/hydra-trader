package trader

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nofx/events"
	"nofx/kernel"
	"nofx/market"
	hlprovider "nofx/provider/hyperliquid"
	"nofx/store"
	"nofx/trader/bitget"
)

var (
	overflowLookupMu sync.RWMutex
	overflowLookup   func(string) *AutoTrader
)

// SetOverflowTargetLookup registers how copy bots find the Bitget overflow trader.
func SetOverflowTargetLookup(fn func(string) *AutoTrader) {
	overflowLookupMu.Lock()
	overflowLookup = fn
	overflowLookupMu.Unlock()
}

func lookupOverflowTrader(id string) *AutoTrader {
	overflowLookupMu.RLock()
	fn := overflowLookup
	overflowLookupMu.RUnlock()
	if fn == nil || id == "" {
		return nil
	}
	return fn(id)
}

func shouldOverflowCategory(cfg *store.CopyStrategyConfig, category string) bool {
	if cfg == nil || !cfg.OverflowEnabled || cfg.OverflowTraderID == "" {
		return false
	}
	if category == "parallel" {
		return cfg.OverflowParallel
	}
	if category != "already_open" && category != "max_positions" && category != "margin" {
		return false
	}
	if len(cfg.OverflowOnSkip) == 0 {
		return true
	}
	for _, allowed := range cfg.OverflowOnSkip {
		if strings.EqualFold(strings.TrimSpace(allowed), category) {
			return true
		}
	}
	return false
}

func overflowSideFromAction(action string) string {
	if strings.Contains(action, "short") {
		return "short"
	}
	return "long"
}

func overflowMaxPositions(cfg *store.CopyStrategyConfig) int {
	if cfg != nil && cfg.OverflowMaxPositions > 0 {
		return cfg.OverflowMaxPositions
	}
	return 10
}

func overflowNotionalUSD(cfg *store.CopyStrategyConfig) float64 {
	if cfg != nil && cfg.OverflowNotionalUSD > 0 {
		return cfg.OverflowNotionalUSD
	}
	if cfg != nil && cfg.NotionalUSD > 0 {
		return cfg.NotionalUSD
	}
	return 50
}

func (at *AutoTrader) enqueueOverflowOpen(fill hlprovider.LeaderFill, symbol, action, category string) {
	cfg := at.config.StrategyConfig.CopyConfig
	if !shouldOverflowCategory(cfg, category) || !strings.HasPrefix(action, "open_") {
		return
	}
	go func() {
		if err := at.tryOverflowOpen(fill, symbol, action, category); err != nil {
			at.logErrorf("[Copy overflow] open %s %s: %v", symbol, action, err)
		}
	}()
}

func (at *AutoTrader) enqueueOverflowClose(fill hlprovider.LeaderFill, symbol, action string) {
	cfg := at.config.StrategyConfig.CopyConfig
	if cfg == nil || !cfg.OverflowEnabled || cfg.OverflowTraderID == "" {
		return
	}
	if !strings.HasPrefix(action, "close_") {
		return
	}
	go func() {
		if err := at.tryOverflowClose(fill, symbol, action); err != nil {
			at.logErrorf("[Copy overflow] close %s %s: %v", symbol, action, err)
		}
	}()
}

func (at *AutoTrader) tryOverflowOpen(fill hlprovider.LeaderFill, symbol, action, category string) error {
	cfg := at.config.StrategyConfig.CopyConfig
	if cfg == nil || cfg.DryRun {
		return nil
	}
	target := lookupOverflowTrader(cfg.OverflowTraderID)
	if target == nil || target.trader == nil {
		at.emitOverflowSkip(symbol, action, "overflow trader not loaded")
		return fmt.Errorf("overflow trader %s not loaded", cfg.OverflowTraderID)
	}

	mapped := symbol
	if bg, ok := target.trader.(*bitget.BitgetTrader); ok {
		resolved, found := bg.ResolveHyperliquidSymbol(symbol)
		if !found {
			at.emitOverflowSkip(symbol, action, "symbol not listed on Bitget")
			return nil
		}
		mapped = resolved
	}

	side := overflowSideFromAction(action)
	if at.store != nil {
		if existing, err := at.store.CopyOverflow().FindOpen(target.id, cfg.LeaderAddress, mapped, side); err == nil && existing != nil {
			held, _ := overflowExchangeHeld(target.trader, mapped, side)
			if held > 0 {
				at.logInfof("[Copy overflow] skip %s %s: already overflowed for this leader", mapped, side)
				return nil
			}
			at.logInfof("[Copy overflow] reconcile stale leg %s %s (ledger open, exchange flat)", mapped, side)
			_ = at.store.CopyOverflow().MarkClosed(existing.ID)
		}
	}

	positions, err := target.trader.GetPositions()
	if err != nil {
		return fmt.Errorf("overflow positions: %w", err)
	}
	if followerHasAnyPosition(positions, mapped) {
		at.emitOverflowSkip(mapped, action, "Bitget already has position")
		return nil
	}
	if max := overflowMaxPositions(cfg); len(positions) >= max {
		at.emitOverflowSkip(mapped, action, fmt.Sprintf("overflow max_positions=%d reached", max))
		return nil
	}

	equity, available, err := target.copyFollowerBalances()
	if err != nil {
		return err
	}
	_ = equity
	notional := overflowNotionalUSD(cfg)
	lev := cfg.MaxLeverage
	if lev <= 0 {
		lev = 10
	}
	if req := requiredCopyMarginUSD(notional, lev); req > available {
		at.emitOverflowSkip(mapped, action, fmt.Sprintf("insufficient Bitget margin: need $%.2f, available $%.2f", req, available))
		target.maybeEmitLiquidationRisk()
		return nil
	}

	decision := &kernel.Decision{
		Symbol:          mapped,
		Action:          action,
		PositionSizeUSD: notional,
		Leverage:        lev,
		Confidence:      100,
		Reasoning: fmt.Sprintf("[Copy overflow] HL skipped (%s); open on %s leader %s tid=%d",
			category, target.name, shortCopyAddr(cfg.LeaderAddress), fill.Tid),
	}
	if price, priceErr := target.trader.GetMarketPrice(mapped); priceErr == nil && price > 0 {
		applyCopyProtectivePrices(decision, price, cfg)
	}

	record := store.DecisionAction{
		Action:    action,
		Symbol:    mapped,
		Leverage:  lev,
		Reasoning: decision.Reasoning,
		Timestamp: time.Now().UTC(),
		Success:   true,
	}
	if err := target.executeOverflowDecision(decision, &record); err != nil {
		if !isTransientCopyError(err) {
			at.emitCopyFailAlert(fill, mapped, action, err)
		}
		return err
	}
	held, confirmed := waitOverflowFillConfirm(target.trader, mapped, side)
	if !confirmed || held <= 0 {
		fillErr := fmt.Errorf("Bitget fill not confirmed for %s %s (held=%.8f)", mapped, side, held)
		at.logErrorf("[Copy overflow] %v", fillErr)
		at.emitCopyFailAlert(fill, mapped, action, fillErr)
		return fillErr
	}
	record.Quantity = held
	if at.store != nil {
		_ = at.store.CopyOverflow().InsertOpen(store.CopyOverflowLeg{
			SourceTraderID:   at.id,
			OverflowTraderID: target.id,
			LeaderAddress:    cfg.LeaderAddress,
			Symbol:           mapped,
			Side:             side,
			Quantity:         held,
			OpenTid:          fill.Tid,
		})
	}
	at.emitOverflowOpened(mapped, action, category, target.name)
	target.maybeEmitLiquidationRisk()
	return nil
}

func (at *AutoTrader) tryOverflowClose(fill hlprovider.LeaderFill, symbol, action string) error {
	cfg := at.config.StrategyConfig.CopyConfig
	if cfg == nil || cfg.DryRun {
		return nil
	}
	target := lookupOverflowTrader(cfg.OverflowTraderID)
	if target == nil || target.trader == nil || at.store == nil {
		return nil
	}
	mapped := symbol
	if bg, ok := target.trader.(*bitget.BitgetTrader); ok {
		if resolved, found := bg.ResolveHyperliquidSymbol(symbol); found {
			mapped = resolved
		}
	}
	side := overflowSideFromAction(action)
	row, err := at.store.CopyOverflow().FindOpen(target.id, cfg.LeaderAddress, mapped, side)
	if err != nil {
		return err
	}
	if row == nil {
		at.logInfof("[Copy overflow] skip close %s %s: no tracked overflow leg", mapped, side)
		return nil
	}

	if leaderOpen, err := leaderHasOpenLeg(cfg.LeaderAddress, mapped, side); err != nil {
		at.logInfof("[Copy overflow] leader leg check failed for %s %s: %v", mapped, side, err)
	} else if leaderOpen {
		at.logInfof("[Copy overflow] skip close %s %s: leader still has leg open (partial close)", mapped, side)
		return nil
	}

	held, markPrice := overflowExchangeHeld(target.trader, mapped, side)
	closeQty := overflowCloseQty(row, held, markPrice, cfg)
	if closeQty <= 0 {
		at.logInfof("[Copy overflow] skip close %s %s: flat on Bitget (held=%.8f)", mapped, side, held)
		return at.store.CopyOverflow().MarkClosed(row.ID)
	}

	decision := &kernel.Decision{
		Symbol:     mapped,
		Action:     action,
		Quantity:   closeQty,
		Confidence: 100,
		Reasoning:  fmt.Sprintf("[Copy overflow] leader close %s tid=%d qty=%.8f", shortCopyAddr(cfg.LeaderAddress), fill.Tid, closeQty),
	}
	record := store.DecisionAction{
		Action:    action,
		Symbol:    mapped,
		Quantity:  closeQty,
		Reasoning: decision.Reasoning,
		Timestamp: time.Now().UTC(),
		Success:   true,
	}
	venue := target.name
	if venue == "" {
		venue = "Bitget"
	}
	at.emitCopyLeaderOverflowCloseAlert(mapped, side, venue)
	if err := target.executeOverflowDecision(decision, &record); err != nil {
		return err
	}
	at.emitOverflowClosed(mapped, action, closeQty, target.name)
	if err := at.store.CopyOverflow().MarkClosed(row.ID); err != nil {
		return err
	}
	return nil
}

func leaderHasOpenLeg(leaderAddress, symbol, side string) (bool, error) {
	leaderAddress = strings.ToLower(strings.TrimSpace(leaderAddress))
	if leaderAddress == "" {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	state, err := hlprovider.FetchAccountStateAll(ctx, leaderAddress)
	if err != nil {
		return false, err
	}
	want := market.Normalize(symbol)
	wantSide := normalizeOverflowSide(side)
	for _, leg := range state.Legs {
		if market.Normalize(leg.Symbol) != want {
			continue
		}
		if strings.EqualFold(leg.Side, wantSide) && leg.Size != 0 {
			return true, nil
		}
	}
	return false, nil
}

func normalizeOverflowSide(side string) string {
	s := strings.ToLower(strings.TrimSpace(side))
	if s == "short" {
		return "short"
	}
	return "long"
}

// overflowPositionReader is the minimal exchange surface overflow fill checks need.
type overflowPositionReader interface {
	GetPositions() ([]map[string]interface{}, error)
}

// waitOverflowFillConfirm polls the overflow venue until a position appears or retries exhaust.
func waitOverflowFillConfirm(tr overflowPositionReader, symbol, side string) (held float64, ok bool) {
	const attempts = 8
	for i := 0; i < attempts; i++ {
		if i > 0 {
			time.Sleep(500 * time.Millisecond)
		}
		held, _ = overflowExchangeHeld(tr, symbol, side)
		if held > 0 {
			return held, true
		}
	}
	return 0, false
}

func overflowExchangeHeld(tr overflowPositionReader, symbol, side string) (held, markPrice float64) {
	if tr == nil {
		return 0, 0
	}
	positions, err := tr.GetPositions()
	if err != nil {
		return 0, 0
	}
	want := market.Normalize(symbol)
	wantSide := normalizeOverflowSide(side)
	for _, pos := range positions {
		ps, _ := pos["symbol"].(string)
		if market.Normalize(ps) != want {
			continue
		}
		posSide, _ := pos["side"].(string)
		if !strings.EqualFold(posSide, wantSide) {
			continue
		}
		if amt, ok := pos["positionAmt"].(float64); ok {
			held = amt
			if held < 0 {
				held = -held
			}
		}
		markPrice = floatFromPos(pos, "markPrice", "mark_price")
		break
	}
	return held, markPrice
}

// overflowCloseQty returns how much of a Bitget overflow leg to close. Legacy
// full-mirror rows in the positions table are ignored; qty is capped to the
// tracked overflow leg and overflow notional when quantity was not recorded.
func overflowCloseQty(row *store.CopyOverflowLeg, held, markPrice float64, cfg *store.CopyStrategyConfig) float64 {
	if held <= 0 {
		return 0
	}
	if row != nil && row.Quantity > 0 {
		qty := row.Quantity
		if qty > held {
			qty = held
		}
		return qty
	}
	notional := overflowNotionalUSD(cfg)
	if markPrice > 0 && notional > 0 {
		est := notional / markPrice * 1.15
		if est > 0 && est < held {
			return est
		}
	}
	return held
}

func (at *AutoTrader) emitOverflowClosed(symbol, action string, qty float64, venueName string) {
	events.EmitSystemAlert(events.SystemAlertEvent{
		TraderID:   at.id,
		TraderName: at.name,
		Type:       events.AlertCopyOverflow,
		Message:    fmt.Sprintf("Leader closed → closed %s %s qty=%.4f on %s", symbol, action, qty, venueName),
		DedupeKey:  fmt.Sprintf("%s:%s:close:%s:%s", at.id, events.AlertCopyOverflow, symbol, action),
	})
}
func (at *AutoTrader) executeOverflowDecision(decision *kernel.Decision, record *store.DecisionAction) error {
	atomic.AddInt32(&at.overflowExecDepth, 1)
	defer atomic.AddInt32(&at.overflowExecDepth, -1)
	return at.executeDecisionWithRecord(decision, record)
}

func (at *AutoTrader) isOverflowExec() bool {
	return at != nil && atomic.LoadInt32(&at.overflowExecDepth) > 0
}

func (at *AutoTrader) overflowOwnedSide(symbol string) (string, bool) {
	if at == nil || at.id == "" {
		return "", false
	}
	if atomic.LoadInt32(&at.overflowExecDepth) > 0 {
		return "", false
	}
	sides, err := at.overflowOpenSideMap()
	if err != nil || len(sides) == 0 {
		return "", false
	}
	sym := market.Normalize(symbol)
	if side, ok := sides[sym]; ok {
		return side, true
	}
	for k, side := range sides {
		if market.Normalize(k) == sym {
			return side, true
		}
	}
	return "", false
}

// blockAIOnOverflowLeg drops AI close/flip of overflow-owned positions.
func (at *AutoTrader) blockAIOnOverflowLeg(symbol, action string) error {
	if at.IsCopyStrategy() {
		return nil
	}
	owned, ok := at.overflowOwnedSide(symbol)
	if !ok {
		return nil
	}
	if strings.HasPrefix(action, "close_") {
		closeSide := overflowSideFromAction(action)
		if closeSide == owned {
			return fmt.Errorf("overflow-owned %s %s: AI cannot close (leader exit only)", symbol, owned)
		}
	}
	if strings.HasPrefix(action, "open_") {
		openSide := overflowSideFromAction(action)
		if openSide != owned {
			return fmt.Errorf("overflow-owned %s %s: AI cannot flip", symbol, owned)
		}
	}
	return nil
}

func (at *AutoTrader) overflowOpenSideMap() (map[string]string, error) {
	if at == nil {
		return nil, nil
	}
	if at.overflowOpenSides != nil {
		return at.overflowOpenSides, nil
	}
	if at.store == nil || at.id == "" {
		return nil, nil
	}
	return at.store.CopyOverflow().OpenSides(at.id)
}

func (at *AutoTrader) countAIOwnedPositions(positions []map[string]interface{}) int {
	if at == nil {
		return len(positions)
	}
	sides, err := at.overflowOpenSideMap()
	if err != nil || len(sides) == 0 {
		return len(positions)
	}
	owned := map[string]string{}
	for k, v := range sides {
		owned[market.Normalize(k)] = v
	}
	n := 0
	for _, pos := range positions {
		sym, _ := pos["symbol"].(string)
		side, _ := pos["side"].(string)
		if ownedSide, ok := owned[market.Normalize(sym)]; ok && strings.EqualFold(ownedSide, side) {
			continue
		}
		n++
	}
	return n
}

func (at *AutoTrader) emitOverflowOpened(symbol, action, category, venueName string) {
	events.EmitSystemAlert(events.SystemAlertEvent{
		TraderID:   at.id,
		TraderName: at.name,
		Type:       events.AlertCopyOverflow,
		Message:    fmt.Sprintf("HL skipped %s %s (%s) → opened on %s", symbol, action, category, venueName),
		DedupeKey:  fmt.Sprintf("%s:%s:%s:%s", at.id, events.AlertCopyOverflow, symbol, action),
	})
}

func (at *AutoTrader) emitOverflowSkip(symbol, action, reason string) {
	at.logInfof("[Copy overflow] skip %s %s: %s", symbol, action, reason)
	events.EmitSystemAlert(events.SystemAlertEvent{
		TraderID:   at.id,
		TraderName: at.name,
		Type:       events.AlertCopySkipped,
		Message:    fmt.Sprintf("Overflow skip %s %s (%s)", symbol, action, reason),
		DedupeKey:  fmt.Sprintf("%s:%s:overflow:%s", at.id, events.AlertCopySkipped, reasonCategory(reason)),
	})
}

const (
	liquidationMarginUsedPct = 80.0
	liquidationDistancePct   = 8.0
)

func (at *AutoTrader) maybeEmitLiquidationRisk() {
	if at == nil || at.trader == nil {
		return
	}
	equity, available, err := at.copyFollowerBalances()
	if err != nil {
		return
	}
	positions, err := at.trader.GetPositions()
	if err != nil {
		return
	}
	reason := liquidationRiskReason(equity, available, positions)
	if reason == "" {
		return
	}
	venue := at.exchange
	if at.name != "" {
		venue = at.name
	}
	events.EmitSystemAlert(events.SystemAlertEvent{
		TraderID:   at.id,
		TraderName: at.name,
		Type:       events.AlertLiquidationRisk,
		Message:    fmt.Sprintf("%s %s. Add funds to this wallet.", venue, reason),
		DedupeKey:  fmt.Sprintf("%s:%s", at.id, events.AlertLiquidationRisk),
	})
}

func liquidationRiskReason(equity, available float64, positions []map[string]interface{}) string {
	if equity > 0 {
		used := equity - available
		if used < 0 {
			used = 0
		}
		pct := used / equity * 100
		if pct >= liquidationMarginUsedPct {
			return fmt.Sprintf("margin used %.0f%% (available $%.2f)", pct, available)
		}
	}
	for _, pos := range positions {
		mark := floatFromPos(pos, "markPrice", "mark_price")
		liq := floatFromPos(pos, "liquidationPrice", "liquidation_price")
		if mark <= 0 || liq <= 0 {
			continue
		}
		dist := (mark - liq) / mark
		if dist < 0 {
			dist = -dist
		}
		if dist*100 <= liquidationDistancePct {
			sym, _ := pos["symbol"].(string)
			return fmt.Sprintf("%s within %.0f%% of liquidation", sym, dist*100)
		}
	}
	return ""
}

func floatFromPos(pos map[string]interface{}, keys ...string) float64 {
	for _, k := range keys {
		switch v := pos[k].(type) {
		case float64:
			return v
		}
	}
	return 0
}

func reasonCategory(reason string) string {
	r := strings.ToLower(reason)
	switch {
	case strings.Contains(r, "margin"):
		return "margin"
	case strings.Contains(r, "listed"):
		return "unlisted"
	case strings.Contains(r, "already"):
		return "already_open"
	default:
		return "other"
	}
}
