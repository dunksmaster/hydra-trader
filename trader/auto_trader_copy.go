package trader

import (
	"context"

	"fmt"

	"nofx/kernel"

	"nofx/logger"

	"nofx/market"

	hlprovider "nofx/provider/hyperliquid"

	"nofx/store"

	"sort"

	"strings"

	"time"
)

type copyLegKey struct {
	Symbol string

	Side string
}

type copyLeg struct {
	Symbol string

	Side string

	NotionalUSD float64

	LeaderNotionalUSD float64

	Leverage int

	Reason string
}

const (
	copyFailsafeStopPct = 50.0

	copyFailsafeTakePct = 200.0
)

// RunCopyCycle mirrors a leader Hyperliquid wallet onto the follower's account.
func (at *AutoTrader) RunCopyCycle() error {
	at.copyMu.Lock()
	defer at.copyMu.Unlock()

	return at.runCopyCycleLocked()
}

func (at *AutoTrader) runCopyCycleLocked() error {
	at.isRunningMutex.RLock()
	running := at.isRunning
	at.isRunningMutex.RUnlock()

	if !running {

		at.logInfof("[Copy] Trader is stopped, aborting copy cycle")

		return nil

	}

	if err := at.reloadStrategyConfigIfChanged(); err != nil {

		return err

	}

	if !at.IsCopyStrategy() {

		return fmt.Errorf("copy configuration not found")

	}

	if at.exchange != "hyperliquid" {

		return fmt.Errorf("copy trading requires Hyperliquid exchange")

	}

	cfg := at.config.StrategyConfig.CopyConfig

	if cfg == nil {

		return fmt.Errorf("copy configuration not found")

	}

	cfg.Normalize()

	if cfg.LeaderAddress == "" {

		return fmt.Errorf("copy leader_address is required")

	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	defer cancel()

	leader, err := hlprovider.FetchAccountStateAll(ctx, cfg.LeaderAddress)

	if err != nil {

		return fmt.Errorf("fetch leader state: %w", err)

	}

	balance, err := at.trader.GetBalance()

	if err != nil {

		return fmt.Errorf("fetch follower balance: %w", err)

	}

	followerEquity := 0.0

	if v, ok := balance["totalWalletBalance"].(float64); ok {

		followerEquity = v

	} else if v, ok := balance["totalEquity"].(float64); ok {

		followerEquity = v

	}

	availableBalance := followerEquity

	if v, ok := balance["availableBalance"].(float64); ok && v > 0 {

		availableBalance = v

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

	walletLegs := countAllWalletLegs(positions)

	walletSlots := at.copyWalletSlots()

	closes, opens := diffCopyLegs(targets, followerMap, walletLegs, walletSlots, maxPositions)

	at.logInfof("[Copy] leader=%s equity=%.2f legs=%d follower equity=%.2f available=%.2f positions=%d wallet_legs=%d/%d max_pos=%d dry_run=%v",

		shortAddr(cfg.LeaderAddress), leader.Equity, len(leader.Legs), followerEquity, availableBalance, len(positions), walletLegs, walletSlots, maxPositions, cfg.DryRun)

	record := &store.DecisionRecord{

		TraderID: at.id,

		CycleNumber: at.cycleNumber + 1,

		Timestamp: time.Now().UTC(),

		SystemPrompt: fmt.Sprintf("copy_trading leader=%s size_mode=%s dry_run=%v max_positions=%d",

			cfg.LeaderAddress, cfg.SizeMode, cfg.DryRun, maxPositions),

		Success: true,
	}

	at.executeCopyLegActions(cfg, closes, opens, record)

	at.cycleNumber = record.CycleNumber

	if at.store != nil {

		if err := at.store.Decision().LogDecision(record); err != nil {

			logger.Warnf("[Copy] Failed to save decision record: %v", err)

		}

	}

	return nil

}

func buildCopyTargets(cfg *store.CopyStrategyConfig, leader *hlprovider.AccountState, followerEquity, availableBalance float64) map[copyLegKey]copyLeg {

	targets := make(map[copyLegKey]copyLeg)

	if leader == nil {

		return targets

	}

	for _, leg := range leader.Legs {

		if isCopySymbolBlocked(cfg, leg.Coin, leg.Symbol) {

			continue

		}

		symbol := market.Normalize(leg.Symbol)

		side := leg.Side

		if cfg.Inverse {

			if side == "long" {

				side = "short"

			} else {

				side = "long"

			}

		}

		notional, skipReason := computeCopyNotional(cfg, leg, leader.Equity, followerEquity, availableBalance)

		if skipReason != "" {

			logger.Infof("[Copy] skip %s %s: %s", symbol, side, skipReason)

			continue

		}

		lev := leg.Leverage

		if lev <= 0 {

			lev = cfg.MaxLeverage

		}

		if lev > cfg.MaxLeverage {

			lev = cfg.MaxLeverage

		}

		key := copyLegKey{Symbol: symbol, Side: side}

		targets[key] = copyLeg{

			Symbol: symbol,

			Side: side,

			NotionalUSD: notional,

			LeaderNotionalUSD: leg.NotionalUSD,

			Leverage: lev,

			Reason: fmt.Sprintf("mirror leader %s %s (leader notional $%.0f)", leg.Coin, leg.Side, leg.NotionalUSD),
		}

	}

	return targets

}

func computeCopyNotional(cfg *store.CopyStrategyConfig, leg hlprovider.AccountPosition, leaderEquity, followerEquity, availableBalance float64) (float64, string) {

	maxPositions := cfg.MaxPositions

	if maxPositions <= 0 {

		maxPositions = 2

	}

	maxLev := cfg.MaxLeverage

	if maxLev <= 0 {

		maxLev = 10

	}

	var notional float64

	switch cfg.SizeMode {

	case "proportional":

		if leaderEquity <= 0 {

			return 0, "leader equity unavailable"

		}

		ratio := followerEquity / leaderEquity

		notional = leg.NotionalUSD * ratio * cfg.CopyRatio

	default:

		notional = cfg.NotionalUSD

	}

	if cfg.MaxNotionalPct > 0 && followerEquity > 0 {

		cap := followerEquity * cfg.MaxNotionalPct / 100

		if notional > cap {

			notional = cap

		}

	}

	if availableBalance > 0 {

		slots := cfg.WalletCopySlots

		if slots <= 0 {

			slots = maxPositions

		}

		marginPerLeg := availableBalance / float64(slots)

		notionalCap := marginPerLeg * float64(maxLev) * 0.85

		if notional > notionalCap {

			notional = notionalCap

		}

	}

	if notional < cfg.MinNotionalUSD {

		return 0, fmt.Sprintf("notional %.2f below minimum %.2f", notional, cfg.MinNotionalUSD)

	}

	return notional, ""

}

func mapFollowerPositions(positions []map[string]interface{}) map[copyLegKey]copyLeg {

	out := make(map[copyLegKey]copyLeg)

	for _, pos := range positions {

		symbol, _ := pos["symbol"].(string)

		side, _ := pos["side"].(string)

		if symbol == "" || side == "" {

			continue

		}

		symbol = market.Normalize(symbol)

		key := copyLegKey{Symbol: symbol, Side: strings.ToLower(side)}

		notional := 0.0

		if mark, ok := pos["markPrice"].(float64); ok {

			qty := 0.0

			if q, ok := pos["positionAmt"].(float64); ok {

				qty = q

			}

			notional = mark * qty

		}

		out[key] = copyLeg{

			Symbol: symbol,

			Side: strings.ToLower(side),

			NotionalUSD: notional,
		}

	}

	return out

}

func copyLeaderSymbolsFromTargets(targets map[copyLegKey]copyLeg) map[string]bool {
	out := make(map[string]bool, len(targets))
	for key := range targets {
		out[key.Symbol] = true
	}
	return out
}

func copyLeaderSymbolsFromLegs(legs []hlprovider.AccountPosition) map[string]bool {
	out := make(map[string]bool, len(legs))
	for _, leg := range legs {
		out[market.Normalize(leg.Symbol)] = true
	}
	return out
}

func countFollowerLegsOnSymbols(follower map[copyLegKey]copyLeg, symbols map[string]bool) int {
	n := 0
	for key := range follower {
		if symbols[key.Symbol] {
			n++
		}
	}
	return n
}

func countExchangeLegsOnSymbols(positions []map[string]interface{}, symbols map[string]bool) int {
	n := 0
	for _, pos := range positions {
		sym, _ := pos["symbol"].(string)
		sym = market.Normalize(sym)
		if symbols[sym] {
			n++
		}
	}
	return n
}

// effectivePositionCountForRisk returns position count scoped to this copy leader's
// symbols when multiple copy bots share one Hyperliquid wallet.
func (at *AutoTrader) effectivePositionCountForRisk(positions []map[string]interface{}) int {
	if !at.IsCopyStrategy() || at.config.StrategyConfig.CopyConfig == nil {
		return at.countAIOwnedPositions(positions)
	}
	cfg := at.config.StrategyConfig.CopyConfig
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	leader, err := hlprovider.FetchAccountStateAll(ctx, cfg.LeaderAddress)
	if err != nil {
		return len(positions)
	}
	symbols := copyLeaderSymbolsFromLegs(leader.Legs)
	if len(symbols) == 0 {
		return 0
	}
	return countExchangeLegsOnSymbols(positions, symbols)
}

// diffCopyLegs plans closes/opens for one copy leader. walletLegs is the number
// of open legs across the whole shared exchange wallet and walletSlots its
// budget; that pair is the binding cap because every copy bot trades the same
// wallet. maxPositions stays as a secondary per-leader cap.
func diffCopyLegs(targets, follower map[copyLegKey]copyLeg, walletLegs, walletSlots, maxPositions int) (closes, opens []copyLeg) {

	if maxPositions <= 0 {

		maxPositions = 2

	}

	if walletSlots <= 0 {

		walletSlots = maxPositions

	}

	leaderSymbols := copyLeaderSymbolsFromTargets(targets)

	// Close follower legs on leader symbols that are absent from leader targets (symbol+side).
	// Ignore legs on other symbols — they may belong to another copy bot on the same wallet.

	for key, leg := range follower {

		if !leaderSymbols[key.Symbol] {

			continue

		}

		if _, ok := targets[key]; !ok {

			// A side flip is handled by the opposite-side pass below. Do not
			// enqueue the same close once as "closed" and again as "flipped".
			oppositeTarget := copyLegKey{Symbol: key.Symbol, Side: flipSide(key.Side)}
			if _, flipping := targets[oppositeTarget]; flipping {
				continue
			}

			closes = append(closes, copyLeg{

				Symbol: leg.Symbol,

				Side: leg.Side,

				Reason: "leader closed leg",
			})

		}

	}

	// Close opposite-side legs on the same symbol before opening the leader side.

	for key, target := range targets {

		opposite := copyLegKey{Symbol: key.Symbol, Side: flipSide(key.Side)}

		if opp, ok := follower[opposite]; ok {

			closes = append(closes, copyLeg{

				Symbol: opp.Symbol,

				Side: opp.Side,

				Reason: fmt.Sprintf("leader now %s on %s", target.Side, target.Symbol),
			})

		}

	}

	closingKeys := make(map[copyLegKey]bool, len(closes))

	for _, leg := range closes {

		closingKeys[copyLegKey{Symbol: leg.Symbol, Side: leg.Side}] = true

	}

	remaining := 0

	for key := range follower {

		if !leaderSymbols[key.Symbol] {

			continue

		}

		if !closingKeys[key] {

			remaining++

		}

	}

	var openCandidates []copyLeg

	for key, target := range targets {

		if _, ok := follower[key]; ok {

			continue

		}

		openCandidates = append(openCandidates, target)

	}

	sort.Slice(openCandidates, func(i, j int) bool {

		return openCandidates[i].LeaderNotionalUSD > openCandidates[j].LeaderNotionalUSD

	})

	// Legs this plan already closes free their wallet slot before any open runs
	// (executeCopyLegActions runs closes first), so credit them back.

	walletRemaining := walletLegs - len(closingKeys)

	if walletRemaining < 0 {

		walletRemaining = 0

	}

	for _, target := range openCandidates {

		if walletRemaining >= walletSlots {

			logger.Infof("[Copy] skip open %s %s: wallet slots full (%d/%d legs on shared wallet)", target.Symbol, target.Side, walletRemaining, walletSlots)

			continue

		}

		if remaining >= maxPositions {

			logger.Infof("[Copy] skip open %s %s: max positions (%d) reached", target.Symbol, target.Side, maxPositions)

			continue

		}

		opens = append(opens, target)

		remaining++

		walletRemaining++

	}

	sort.Slice(closes, func(i, j int) bool {

		if closes[i].Symbol == closes[j].Symbol {

			return closes[i].Side < closes[j].Side

		}

		return closes[i].Symbol < closes[j].Symbol

	})

	sort.Slice(opens, func(i, j int) bool {

		return opens[i].LeaderNotionalUSD > opens[j].LeaderNotionalUSD

	})

	return closes, opens

}

func isCopySymbolBlocked(cfg *store.CopyStrategyConfig, coin, symbol string) bool {

	coin = strings.ToUpper(strings.TrimSpace(coin))

	symbol = strings.ToUpper(strings.TrimSpace(symbol))

	for _, blocked := range cfg.SymbolBlocklist {

		b := strings.ToUpper(strings.TrimSpace(blocked))

		if b == "" {

			continue

		}

		if strings.HasPrefix(coin, b) || strings.HasPrefix(symbol, b) {

			return true

		}

	}

	return false

}

func flipSide(side string) string {

	if strings.EqualFold(side, "long") {

		return "short"

	}

	return "long"

}

func applyCopyProtectivePrices(decision *kernel.Decision, entry float64, cfg *store.CopyStrategyConfig) {

	if decision == nil || entry <= 0 || cfg == nil {

		return

	}

	long := decision.Action == "open_long"

	if cfg.ExitMode == "leader_only" {

		priceStop := copyFailsafeStopPct

		priceTake := copyFailsafeTakePct

		if long {

			decision.StopLoss = entry * (1 - priceStop/100)

			decision.TakeProfit = entry * (1 + priceTake/100)

		} else {

			decision.StopLoss = entry * (1 + priceStop/100)

			decision.TakeProfit = entry * (1 - priceTake/100)

		}

		return

	}

	marginPct := cfg.SafetyStopPct

	if marginPct <= 0 {

		marginPct = 15

	}

	lev := decision.Leverage

	if lev <= 0 {

		lev = cfg.MaxLeverage

	}

	pricePct := marginPct / float64(lev)

	wideTake := 100.0

	if long {

		decision.StopLoss = entry * (1 - pricePct/100)

		decision.TakeProfit = entry * (1 + wideTake/100)

	} else {

		decision.StopLoss = entry * (1 + pricePct/100)

		decision.TakeProfit = entry * (1 - wideTake/100)

	}

}

func shortAddr(addr string) string {

	addr = strings.TrimSpace(addr)

	if len(addr) <= 10 {

		return addr

	}

	return addr[:6] + "..." + addr[len(addr)-4:]

}

const copyTransientBackoff = 45 * time.Second

func copyTransientKey(symbol, action string) string {
	return strings.ToUpper(strings.TrimSpace(symbol)) + ":" + strings.ToLower(strings.TrimSpace(action))
}

func (at *AutoTrader) copyActionCoolingDown(symbol, action string, now time.Time) bool {
	if at.copyTransientUntil == nil {
		return false
	}
	until := at.copyTransientUntil[copyTransientKey(symbol, action)]
	if until.IsZero() || !now.Before(until) {
		delete(at.copyTransientUntil, copyTransientKey(symbol, action))
		return false
	}
	return true
}

func (at *AutoTrader) markCopyTransientFailure(symbol, action string, now time.Time) {
	if at.copyTransientUntil == nil {
		at.copyTransientUntil = make(map[string]time.Time)
	}
	at.copyTransientUntil[copyTransientKey(symbol, action)] = now.Add(copyTransientBackoff)
}

func (at *AutoTrader) executeCopyLegActions(cfg *store.CopyStrategyConfig, closes, opens []copyLeg, record *store.DecisionRecord) {
	runKind := func(decisions []copyLeg, kind string) {
		for _, leg := range decisions {
			var action string
			switch kind {
			case "close":
				if leg.Side == "long" {
					action = "close_long"
				} else {
					action = "close_short"
				}
			case "open":
				if leg.Side == "long" {
					action = "open_long"
				} else {
					action = "open_short"
				}
			default:
				continue
			}

			if at.copyActionCoolingDown(leg.Symbol, action, time.Now()) {
				at.logInfof("[Copy] backoff skip %s %s after transient Hyperliquid state error", leg.Symbol, action)
				continue
			}

			if kind == "open" {
				if positions, posErr := at.trader.GetPositions(); posErr == nil && followerHasSide(positions, leg.Symbol, action) {
					at.logInfof("[Copy] skip open %s %s: follower already has position", leg.Symbol, leg.Side)
					continue
				}
			}

			decision := &kernel.Decision{
				Symbol:          leg.Symbol,
				Action:          action,
				PositionSizeUSD: leg.NotionalUSD,
				Leverage:        leg.Leverage,
				Confidence:      100,
				Reasoning:       leg.Reason,
			}

			msg := fmt.Sprintf("[Copy] %s %s %s notional=%.2f lev=%dx — %s",
				map[bool]string{true: "DRY-RUN", false: "EXEC"}[cfg.DryRun],
				action, leg.Symbol, leg.NotionalUSD, leg.Leverage, leg.Reason)
			at.logInfof(msg)
			record.ExecutionLog = append(record.ExecutionLog, msg)

			actionRecord := store.DecisionAction{
				Action:    action,
				Symbol:    leg.Symbol,
				Leverage:  leg.Leverage,
				Reasoning: leg.Reason,
				Timestamp: time.Now().UTC(),
				Success:   true,
			}

			if cfg.DryRun {
				record.Decisions = append(record.Decisions, actionRecord)
				continue
			}

			if kind == "close" {
				at.emitCopyLeaderRuleAlert(leg.Symbol, action, "Hyperliquid", leg.Reason)
			}

			if strings.HasPrefix(action, "open_") {
				if price, priceErr := at.trader.GetMarketPrice(leg.Symbol); priceErr == nil && price > 0 {
					applyCopyProtectivePrices(decision, price, cfg)
				}
			}

			if err := at.executeCopyDecisionWithRetry(decision, &actionRecord); err != nil {
				actionRecord.Success = false
				actionRecord.Error = err.Error()
				record.Success = false
				at.logErrorf("[Copy] %s failed: %v", action, err)
				record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf("FAILED %s: %v", action, err))
				if isTransientCopyError(err) {
					at.markCopyTransientFailure(leg.Symbol, action, time.Now())
					at.logInfof("[Copy] transient HL error on %s %s — will retry next cycle (%s)", leg.Symbol, action, leg.Reason)
				} else {
					at.emitCopyFailAlertLeg(leg.Symbol, action, leg.Reason, err)
				}
			} else if at.copyTransientUntil != nil {
				delete(at.copyTransientUntil, copyTransientKey(leg.Symbol, action))
			}
			record.Decisions = append(record.Decisions, actionRecord)
		}
	}
	runKind(closes, "close")
	runKind(opens, "open")
}

// filterDriftCopyLegs returns only opens/closes that have persisted longer than grace.
func filterDriftCopyLegs(opens, closes []copyLeg, driftSince, extraSince map[copyLegKey]time.Time, now time.Time, grace time.Duration) (execOpens, execCloses []copyLeg, nextDrift, nextExtra map[copyLegKey]time.Time) {
	nextDrift = make(map[copyLegKey]time.Time, len(opens))
	for _, leg := range opens {
		key := copyLegKey{Symbol: leg.Symbol, Side: leg.Side}
		since, ok := driftSince[key]
		if !ok {
			since = now
		}
		nextDrift[key] = since
		if now.Sub(since) >= grace {
			execOpens = append(execOpens, leg)
		}
	}
	nextExtra = make(map[copyLegKey]time.Time, len(closes))
	for _, leg := range closes {
		key := copyLegKey{Symbol: leg.Symbol, Side: leg.Side}
		since, ok := extraSince[key]
		if !ok {
			since = now
		}
		nextExtra[key] = since
		if now.Sub(since) >= grace {
			execCloses = append(execCloses, leg)
		}
	}
	return execOpens, execCloses, nextDrift, nextExtra
}

func (at *AutoTrader) IsCopyStrategy() bool {

	if at.config.StrategyConfig == nil {

		return false

	}

	return at.config.StrategyConfig.StrategyType == "copy_trading" && at.config.StrategyConfig.CopyConfig != nil

}

// ComputeCopyNotionalUSD is exported for unit tests.

func ComputeCopyNotionalUSD(cfg *store.CopyStrategyConfig, legNotional, leaderEquity, followerEquity, availableBalance float64) (float64, string) {

	cfg.Normalize()

	leg := hlprovider.AccountPosition{NotionalUSD: legNotional}

	return computeCopyNotional(cfg, leg, leaderEquity, followerEquity, availableBalance)

}
