package trader

import (
	"fmt"
	"nofx/kernel"
	"nofx/market"
	"nofx/store"
	"strings"
	"time"
)

const (
	// Exit gates, validated by decision replay (2026-07-26, 4154 cycles,
	// 3-fold robustness): gates beat no-gates by 34 pts and the old rigid
	// 4h/8h by 16 pts of worst-fold score; the searched optimum sits at these
	// values. Thresholds are PRICE-move percentages (leverage-independent).
	autopilotMinHoldDuration        = 90 * time.Minute
	autopilotNoiseCloseHoldDuration = 3 * time.Hour
	// Re-entering a just-closed symbol was a consistent loss source: the
	// replay's top-20 configs cluster tightly at ~4h.
	autopilotReentryCooldown      = 4 * time.Hour
	autopilotMaxOpensPerHour      = 3
	autopilotMaxOpensPerCycle     = 2
	earlyCloseStopLossBypassPct   = -3.0
	earlyCloseTakeProfitBypassPct = 8.0
	noiseCloseLossFloorPct        = -2.0
	noiseCloseProfitCeilingPct    = 3.0
)

// positionPricePnLPct converts the margin-based UnrealizedPnLPct reported for
// a position into the underlying price-move percentage.
func positionPricePnLPct(pos *kernel.PositionInfo) float64 {
	if pos == nil {
		return 0
	}
	if pos.Leverage > 1 {
		return pos.UnrealizedPnLPct / float64(pos.Leverage)
	}
	return pos.UnrealizedPnLPct
}

func isOpenAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "open_long", "open_short":
		return true
	default:
		return false
	}
}

func isCloseAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "close_long", "close_short":
		return true
	default:
		return false
	}
}

func closeActionSide(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "close_long":
		return "long"
	case "close_short":
		return "short"
	default:
		return ""
	}
}

func openActionSide(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "open_long":
		return "long"
	case "open_short":
		return "short"
	default:
		return ""
	}
}

func normalizedDecisionSymbol(symbol string) string {
	return market.Normalize(strings.TrimSpace(symbol))
}

func (at *AutoTrader) tradeThrottleReason(decision kernel.Decision, ctx *kernel.Context, opensQueuedThisCycle int) string {
	if ctx == nil {
		return ""
	}

	switch {
	case isOpenAction(decision.Action):
		return at.openThrottleReason(decision, ctx, opensQueuedThisCycle)
	case isCloseAction(decision.Action):
		return at.closeThrottleReason(decision, ctx)
	default:
		return ""
	}
}

func (at *AutoTrader) openThrottleReason(decision kernel.Decision, ctx *kernel.Context, opensQueuedThisCycle int) string {
	symbol := normalizedDecisionSymbol(decision.Symbol)
	if symbol == "" {
		return ""
	}

	if opensQueuedThisCycle >= autopilotMaxOpensPerCycle {
		return fmt.Sprintf("trade throttle: only %d new position may be opened per cycle", autopilotMaxOpensPerCycle)
	}

	if pos := findAnyContextPosition(ctx, symbol); pos != nil {
		return fmt.Sprintf("trade throttle: %s already has an open %s position; manage or close it before opening another side", symbol, pos.Side)
	}

	openCount, err := at.countRecentOpenOrders(time.Now().Add(-1 * time.Hour))
	if err != nil {
		at.logWarnf("⚠️ Trade throttle could not read recent open orders: %v", err)
	} else if openCount >= autopilotMaxOpensPerHour {
		return fmt.Sprintf("trade throttle: %d open order already executed in the last hour; max is %d", openCount, autopilotMaxOpensPerHour)
	}

	if !at.usesVergexSignalPolicy() {
		cooldown := at.reentryCooldown()
		if order := at.findRecentCloseOrder(symbol, time.Now().Add(-cooldown)); order != nil {
			age := time.Since(time.UnixMilli(order.CreatedAt))
			remaining := cooldown - age
			if remaining < 0 {
				remaining = 0
			}
			return fmt.Sprintf("trade throttle: %s was closed %s ago; wait %s before re-entry", symbol, roundDuration(age), roundDuration(remaining))
		}
	}

	return ""
}

type closeGates struct {
	minHold, noiseHold                        time.Duration
	slBypass, tpBypass, noiseFloor, noiseCeil float64
}

func (at *AutoTrader) reentryCooldown() time.Duration {
	if at != nil && strings.EqualFold(at.exchange, "hyperliquid") {
		return 30 * time.Minute
	}
	return autopilotReentryCooldown
}

// closeGates uses the fast book on Hyperliquid and Bitget so $2–$10 dollar
// targets can actually close. NFI and other venues stay on the slow replay book.
func (at *AutoTrader) closeGates() closeGates {
	if at != nil && (strings.EqualFold(at.exchange, "hyperliquid") || strings.EqualFold(at.exchange, "bitget")) {
		return closeGates{
			minHold:    20 * time.Minute,
			noiseHold:  40 * time.Minute,
			slBypass:   -1.2,
			tpBypass:   1.2,
			noiseFloor: -0.8,
			noiseCeil:  0.8,
		}
	}
	return closeGates{
		minHold:    autopilotMinHoldDuration,
		noiseHold:  autopilotNoiseCloseHoldDuration,
		slBypass:   earlyCloseStopLossBypassPct,
		tpBypass:   earlyCloseTakeProfitBypassPct,
		noiseFloor: noiseCloseLossFloorPct,
		noiseCeil:  noiseCloseProfitCeilingPct,
	}
}

func isCodeEnforcedExit(reasoning string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(reasoning)), "code-enforced")
}

func (at *AutoTrader) blockAICloseOnLoss() bool {
	if at == nil || at.config.StrategyConfig == nil {
		return false
	}
	return at.config.StrategyConfig.RiskControl.BlockAICloseOnLoss
}

func (at *AutoTrader) closeThrottleReason(decision kernel.Decision, ctx *kernel.Context) string {
	// Vergex positions are closed by the direction state machine. A changed or
	// vanished board signal must exit immediately, independent of hold duration.
	if at.usesVergexSignalPolicy() {
		return ""
	}
	symbol := normalizedDecisionSymbol(decision.Symbol)
	side := closeActionSide(decision.Action)
	if symbol == "" || side == "" {
		return ""
	}
	gates := at.closeGates()

	pos := findContextPosition(ctx, symbol, side)
	pnlPct := 0.0
	entryTime := int64(0)
	if pos != nil {
		pnlPct = positionPricePnLPct(pos)
		entryTime = pos.UpdateTime
	}

	if isCodeEnforcedExit(decision.Reasoning) {
		return ""
	}

	if at.blockAICloseOnLoss() && pos != nil && pos.UnrealizedPnLPct < 0 {
		return fmt.Sprintf(
			"trade throttle: %s %s is underwater (%.1f%% margin PnL); AI cannot close losers — wait for code-enforced SL/TP only",
			symbol, side, pos.UnrealizedPnLPct,
		)
	}

	if order := at.findRecentOpenOrder(symbol, side, time.Now().Add(-gates.noiseHold)); order != nil && order.CreatedAt > entryTime {
		entryTime = order.CreatedAt
	}
	if entryTime <= 0 {
		return ""
	}

	heldFor := time.Since(time.UnixMilli(entryTime))
	if heldFor < 0 {
		heldFor = 0
	}
	if heldFor >= gates.minHold {
		if heldFor >= gates.noiseHold ||
			pnlPct <= gates.noiseFloor ||
			pnlPct >= gates.noiseCeil {
			return ""
		}

		remaining := gates.noiseHold - heldFor
		return fmt.Sprintf(
			"trade throttle: %s %s has been held for %s with price PnL %.2f%%; it is still inside the noise band %.1f%% to %.1f%%, so wait about %s before a flat/small close",
			symbol,
			side,
			roundDuration(heldFor),
			pnlPct,
			gates.noiseFloor,
			gates.noiseCeil,
			roundDuration(remaining),
		)
	}

	// Do not block true risk exits or unusually strong take-profit exits.
	if pnlPct <= gates.slBypass || pnlPct >= gates.tpBypass {
		return ""
	}

	remaining := gates.minHold - heldFor
	return fmt.Sprintf(
		"trade throttle: %s %s has only been held for %s with price PnL %.2f%%; min AI-managed hold is %s unless price loss <= %.1f%% or price profit >= %.1f%%",
		symbol,
		side,
		roundDuration(heldFor),
		pnlPct,
		roundDuration(gates.minHold),
		gates.slBypass,
		gates.tpBypass,
	) + fmt.Sprintf("; wait about %s", roundDuration(remaining))
}

func findContextPosition(ctx *kernel.Context, symbol string, side string) *kernel.PositionInfo {
	if ctx == nil {
		return nil
	}
	for i := range ctx.Positions {
		pos := &ctx.Positions[i]
		if normalizedDecisionSymbol(pos.Symbol) == symbol && strings.EqualFold(pos.Side, side) {
			return pos
		}
	}
	return nil
}

func findAnyContextPosition(ctx *kernel.Context, symbol string) *kernel.PositionInfo {
	if ctx == nil {
		return nil
	}
	for i := range ctx.Positions {
		pos := &ctx.Positions[i]
		if normalizedDecisionSymbol(pos.Symbol) == symbol {
			return pos
		}
	}
	return nil
}

func (at *AutoTrader) recentOrders(limit int) ([]*store.TraderOrder, error) {
	if at == nil || at.store == nil {
		return nil, nil
	}
	return at.store.Order().GetTraderOrders(at.id, limit)
}

func (at *AutoTrader) countRecentOpenOrders(since time.Time) (int, error) {
	orders, err := at.recentOrders(100)
	if err != nil {
		return 0, err
	}
	sinceMs := since.UTC().UnixMilli()
	// Bitget market opens often split into several FILLED rows at the same ms.
	// Count one logical open per symbol+side per minute, not per partial fill.
	seen := make(map[string]struct{})
	for _, order := range orders {
		if order == nil || order.CreatedAt < sinceMs || isCanceledOrder(order) {
			continue
		}
		if !isOpenAction(order.OrderAction) {
			continue
		}
		minute := order.CreatedAt / (60 * 1000)
		key := normalizedDecisionSymbol(order.Symbol) + "|" + strings.ToLower(strings.TrimSpace(order.OrderAction)) + "|" + fmt.Sprint(minute)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
	}
	return len(seen), nil
}

func (at *AutoTrader) findRecentCloseOrder(symbol string, since time.Time) *store.TraderOrder {
	orders, err := at.recentOrders(100)
	if err != nil {
		at.logWarnf("⚠️ Trade throttle could not read recent close orders: %v", err)
		return nil
	}
	sinceMs := since.UTC().UnixMilli()
	for _, order := range orders {
		if order == nil || order.CreatedAt < sinceMs || isCanceledOrder(order) {
			continue
		}
		if normalizedDecisionSymbol(order.Symbol) == symbol && isCloseAction(order.OrderAction) {
			return order
		}
	}
	return nil
}

func (at *AutoTrader) findRecentOpenOrder(symbol string, side string, since time.Time) *store.TraderOrder {
	orders, err := at.recentOrders(100)
	if err != nil {
		at.logWarnf("⚠️ Trade throttle could not read recent open orders: %v", err)
		return nil
	}
	sinceMs := since.UTC().UnixMilli()
	for _, order := range orders {
		if order == nil || order.CreatedAt < sinceMs || isCanceledOrder(order) {
			continue
		}
		if normalizedDecisionSymbol(order.Symbol) == symbol &&
			strings.EqualFold(openActionSide(order.OrderAction), side) {
			return order
		}
	}
	return nil
}

func isCanceledOrder(order *store.TraderOrder) bool {
	status := strings.ToUpper(strings.TrimSpace(order.Status))
	return status == "CANCELED" || status == "CANCELLED" || status == "REJECTED" || status == "EXPIRED"
}

func roundDuration(d time.Duration) string {
	if d < time.Minute {
		return "0m"
	}
	return d.Round(time.Minute).String()
}
