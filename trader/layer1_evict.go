package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"nofx/events"
	"nofx/kernel"
	"nofx/market"
	hlprovider "nofx/provider/hyperliquid"
	"nofx/store"
)

const (
	layer1EvictFrameDuration = 30 * time.Minute
	layer1RunnerProtectPct   = 3.0
	layer1NVIDIATimeout      = 45 * time.Second
)

type l2EvictCandidate struct {
	Trader      *AutoTrader
	Symbol      string
	Side        string
	LeaderAddr  string
	TraderName  string
	PnLPct      float64
	PnLUSD      float64
	HoldMinutes float64
	EntryPrice  float64
	MarkPrice   float64
	Size        float64
}

type layer1EvictDecision struct {
	Action   string `json:"action"` // close | wait | pick_other
	Symbol   string `json:"symbol,omitempty"`
	Side     string `json:"side,omitempty"`
	TraderID string `json:"trader_id,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

var (
	layer1NVIDIALastCall sync.Map // symbol -> time.Time
)

func (at *AutoTrader) evictL2ForL1Open(
	ctx context.Context,
	fill hlprovider.LeaderFill,
	symbol, action string,
	positions []map[string]interface{},
	availableBalance, notional float64,
	lev int,
) (bool, error) {
	candidates, err := at.buildL2EvictCandidates(ctx, positions, symbol, action)
	if err != nil {
		return false, err
	}
	if len(candidates) == 0 {
		at.logInfof("[Copy L1] no L2 legs available to evict for %s", symbol)
		return false, nil
	}

	chosen := at.pickL2EvictCandidate(ctx, fill, symbol, action, candidates, len(positions), availableBalance, notional, lev)
	if chosen == nil {
		at.logInfof("[Copy L1] NVIDIA/wait: keeping L2 book for %s", symbol)
		return false, nil
	}

	if err := at.closeL2EvictCandidate(ctx, chosen, fill, symbol); err != nil {
		return false, err
	}
	return true, nil
}

func (at *AutoTrader) buildL2EvictCandidates(
	ctx context.Context,
	positions []map[string]interface{},
	l1Symbol, l1Action string,
) ([]l2EvictCandidate, error) {
	posByKey := map[string]map[string]interface{}{}
	for _, pos := range positions {
		sym, _ := pos["symbol"].(string)
		side, _ := pos["side"].(string)
		sym = market.Normalize(sym)
		side = strings.ToLower(side)
		if sym == "" || side == "" {
			continue
		}
		amt := floatFromPos(pos, "positionAmt", "position_amt")
		if amt == 0 {
			continue
		}
		posByKey[sym+"_"+side] = pos
	}

	var out []l2EvictCandidate
	for _, sibling := range at.copySiblings() {
		if sibling == nil || sibling.id == at.id {
			continue
		}
		cfg := siblingCopyConfig(sibling)
		if cfg == nil || cfg.LeaderAddress == "" {
			continue
		}
		// Explicit layer only. siblingCopyLayer() defaults a missing copy_layer
		// to 2, which would make any strategy that lost its layer field — an L1
		// included — an eviction target.
		if cfg.CopyLayer != 2 {
			continue
		}
		leader, err := hlprovider.FetchAccountStateAll(ctx, cfg.LeaderAddress)
		if err != nil {
			continue
		}
		for _, leg := range leader.Legs {
			sym := market.Normalize(leg.Symbol)
			side := strings.ToLower(leg.Side)
			key := sym + "_" + side
			pos, ok := posByKey[key]
			if !ok {
				continue
			}
			mark := floatFromPos(pos, "markPrice", "mark_price")
			entry := floatFromPos(pos, "entryPrice", "entry_price")
			if entry <= 0 {
				entry = leg.EntryPrice
			}
			pnlPct := legPnLPct(side, entry, mark)
			pnlUSD := floatFromPos(pos, "unRealizedProfit", "unrealized_pnl", "unrealizedProfit")
			size := floatFromPos(pos, "positionAmt", "position_amt")
			if size < 0 {
				size = -size
			}
			out = append(out, l2EvictCandidate{
				Trader:      sibling,
				Symbol:      sym,
				Side:        side,
				LeaderAddr:  cfg.LeaderAddress,
				TraderName:  sibling.name,
				PnLPct:      pnlPct,
				PnLUSD:      pnlUSD,
				HoldMinutes: 0,
				EntryPrice:  entry,
				MarkPrice:   mark,
				Size:        size,
			})
		}
	}

	l1Side := "long"
	if strings.Contains(l1Action, "short") {
		l1Side = "short"
	}
	sortEvictCandidates(out, market.Normalize(l1Symbol), l1Side)
	return out, nil
}

func legPnLPct(side string, entry, mark float64) float64 {
	if entry <= 0 || mark <= 0 {
		return 0
	}
	if side == "short" {
		return (entry - mark) / entry * 100
	}
	return (mark - entry) / entry * 100
}

func legHoldMinutes(_ hlprovider.AccountPosition) float64 {
	return 0
}

func sortEvictCandidates(candidates []l2EvictCandidate, l1Symbol, l1Side string) {
	if len(candidates) <= 1 {
		return
	}
	type scored struct {
		c     l2EvictCandidate
		score float64
	}
	scoredList := make([]scored, len(candidates))
	for i, c := range candidates {
		scoredList[i] = scored{c: c, score: deterministicEvictScore(c, l1Symbol, l1Side, candidates)}
	}
	for i := 0; i < len(scoredList); i++ {
		for j := i + 1; j < len(scoredList); j++ {
			if scoredList[j].score < scoredList[i].score {
				scoredList[i], scoredList[j] = scoredList[j], scoredList[i]
			}
		}
	}
	for i := range candidates {
		candidates[i] = scoredList[i].c
	}
}

func deterministicEvictScore(c l2EvictCandidate, l1Symbol, l1Side string, all []l2EvictCandidate) float64 {
	if c.Symbol == l1Symbol && c.Side != l1Side {
		return 0
	}
	if c.PnLPct >= layer1RunnerProtectPct && hasSmallerWinnerOrTinyLoser(all, c) {
		return 900 + c.PnLPct
	}
	if c.PnLPct > 0 {
		return 100 - c.PnLPct
	}
	return 500 + math.Abs(c.PnLPct)
}

func hasSmallerWinnerOrTinyLoser(all []l2EvictCandidate, runner l2EvictCandidate) bool {
	for _, c := range all {
		if c.Symbol == runner.Symbol && c.Side == runner.Side {
			continue
		}
		if c.PnLPct > 0 && c.PnLPct < runner.PnLPct {
			return true
		}
		if c.PnLPct <= 0 && c.PnLPct > -1.0 {
			return true
		}
	}
	return false
}

func (at *AutoTrader) pickL2EvictCandidate(
	ctx context.Context,
	fill hlprovider.LeaderFill,
	l1Symbol, l1Action string,
	candidates []l2EvictCandidate,
	openLegs int,
	availableBalance, notional float64,
	lev int,
) *l2EvictCandidate {
	l1Symbol = market.Normalize(l1Symbol)
	// Rate limit per trader, not globally per symbol: four L1 bots share this
	// map and one bot's call must not silence the model for the others.
	rateKey := at.id + "|" + l1Symbol
	l1Side := "long"
	if strings.Contains(l1Action, "short") {
		l1Side = "short"
	}

	var decision *layer1EvictDecision
	rateLimited := false
	if last, ok := layer1NVIDIALastCall.Load(rateKey); ok {
		if t, ok := last.(time.Time); ok && time.Since(t) < layer1EvictFrameDuration {
			rateLimited = true
		}
	}
	if rateLimited {
		at.logInfof("[Copy L1] eviction rate-limited for %s — using deterministic rank", l1Symbol)
	} else {
		decision = at.callNVIDIAEvictDecision(ctx, fill, l1Symbol, l1Action, candidates, openLegs, availableBalance, notional, lev)
		layer1NVIDIALastCall.Store(rateKey, time.Now())
	}

	return resolveL2EvictCandidate(decision, candidates, l1Symbol, l1Side, func(msg string, args ...interface{}) {
		at.logInfof(msg, args...)
	})
}

// resolveL2EvictCandidate applies NVIDIA decision when explicit; falls back to
// deterministic rank on timeout, missing client, parse failure, rate limit, or
// unmatched close/pick_other. Explicit "wait" never evicts.
func resolveL2EvictCandidate(
	decision *layer1EvictDecision,
	candidates []l2EvictCandidate,
	l1Symbol, l1Side string,
	logf func(string, ...interface{}),
) *l2EvictCandidate {
	if len(candidates) == 0 {
		return nil
	}
	if decision != nil && decision.Action == "wait" {
		if logf != nil {
			logf("[Copy L1] NVIDIA wait for %s: %s", l1Symbol, decision.Reason)
		}
		return nil
	}
	chosen, reason := selectEvictCandidate(decision, candidates)
	if chosen != nil {
		return chosen
	}
	if decision != nil && logf != nil {
		logf("[Copy L1] model did not name a leg for %s: %s — using deterministic rank", l1Symbol, reason)
	} else if logf != nil {
		logf("[Copy L1] no model decision for %s — using deterministic rank", l1Symbol)
	}
	ranked := append([]l2EvictCandidate(nil), candidates...)
	sortEvictCandidates(ranked, l1Symbol, l1Side)
	return &ranked[0]
}

// selectEvictCandidate maps a model decision onto a live L2 leg. It fails closed:
// skipping one L1 entry costs an opportunity, closing a live leg on a garbled,
// empty or unrecognised response costs real money. Only an explicit close or
// pick_other that names a candidate we can actually match may evict.
func selectEvictCandidate(decision *layer1EvictDecision, candidates []l2EvictCandidate) (*l2EvictCandidate, string) {
	if decision == nil {
		return nil, "no usable model decision"
	}
	if decision.Action != "close" && decision.Action != "pick_other" {
		return nil, fmt.Sprintf("action %q is not an eviction instruction", decision.Action)
	}
	if decision.TraderID == "" && decision.Symbol == "" && decision.Side == "" {
		return nil, fmt.Sprintf("action %q named no position to close", decision.Action)
	}
	for i := range candidates {
		c := &candidates[i]
		if decision.TraderID != "" && (c.Trader == nil || c.Trader.id != decision.TraderID) {
			continue
		}
		if decision.Symbol != "" && market.Normalize(decision.Symbol) != c.Symbol {
			continue
		}
		if decision.Side != "" && strings.ToLower(decision.Side) != c.Side {
			continue
		}
		return c, ""
	}
	return nil, fmt.Sprintf("named position trader=%q %s/%s matches no L2 candidate",
		decision.TraderID, decision.Symbol, decision.Side)
}

// SelectEvictCandidateForTest exposes fail-closed decision matching for unit tests.
func SelectEvictCandidateForTest(decision *layer1EvictDecision, candidates []l2EvictCandidate) (*l2EvictCandidate, string) {
	return selectEvictCandidate(decision, candidates)
}

func (at *AutoTrader) callNVIDIAEvictDecision(
	ctx context.Context,
	fill hlprovider.LeaderFill,
	l1Symbol, l1Action string,
	candidates []l2EvictCandidate,
	openLegs int,
	availableBalance, notional float64,
	lev int,
) *layer1EvictDecision {
	if at.mcpClient == nil {
		return nil
	}
	frame := buildLayer1EvictFrame(fill, l1Symbol, l1Action, candidates, openLegs, availableBalance, notional, lev)
	systemPrompt := `You are a copy-trading eviction advisor. L1 (priority) needs a slot on a shared Hyperliquid wallet.
Respond with JSON only: {"action":"close"|"wait"|"pick_other","symbol":"...","side":"long|short","trader_id":"...","reason":"..."}
Rules: never close L1 legs. Prefer closing same-coin opposite side, then bank small winners, avoid cutting large runners (+3%) if alternatives exist. "wait" if L1 entry is weak.`

	callCtx, cancel := context.WithTimeout(ctx, layer1NVIDIATimeout)
	defer cancel()

	type result struct {
		resp string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		resp, err := at.mcpClient.CallWithMessages(systemPrompt, frame)
		ch <- result{resp: resp, err: err}
	}()

	var resp string
	select {
	case <-callCtx.Done():
		at.logInfof("[Copy L1] NVIDIA eviction timeout for %s — using deterministic rank", l1Symbol)
		return nil
	case r := <-ch:
		if r.err != nil {
			at.logInfof("[Copy L1] NVIDIA eviction error for %s: %v", l1Symbol, r.err)
			return nil
		}
		resp = r.resp
	}

	return parseLayer1EvictDecision(resp)
}

func buildLayer1EvictFrame(
	fill hlprovider.LeaderFill,
	l1Symbol, l1Action string,
	candidates []l2EvictCandidate,
	openLegs int,
	availableBalance, notional float64,
	lev int,
) string {
	type candJSON struct {
		TraderID    string  `json:"trader_id"`
		TraderName  string  `json:"trader_name"`
		Leader      string  `json:"leader"`
		Symbol      string  `json:"symbol"`
		Side        string  `json:"side"`
		PnLPct      float64 `json:"pnl_pct"`
		PnLUSD      float64 `json:"pnl_usd"`
		HoldMinutes float64 `json:"hold_minutes"`
	}
	cands := make([]candJSON, len(candidates))
	for i, c := range candidates {
		cands[i] = candJSON{
			TraderID:    c.Trader.id,
			TraderName:  c.TraderName,
			Leader:      shortCopyAddr(c.LeaderAddr),
			Symbol:      c.Symbol,
			Side:        c.Side,
			PnLPct:      c.PnLPct,
			PnLUSD:      c.PnLUSD,
			HoldMinutes: c.HoldMinutes,
		}
	}
	payload := map[string]any{
		"frame_minutes":      30,
		"l1_symbol":          l1Symbol,
		"l1_action":          l1Action,
		"l1_leader_fill_usd": fill.NotionalUSD,
		"l1_tid":             fill.Tid,
		"open_legs":          openLegs,
		"wallet_slots":       5,
		"available_margin":   availableBalance,
		"required_notional":  notional,
		"leverage":           lev,
		"l2_candidates":      cands,
	}
	data, _ := json.Marshal(payload)
	return string(data)
}

func parseLayer1EvictDecision(raw string) *layer1EvictDecision {
	raw = strings.TrimSpace(raw)
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		raw = raw[start : end+1]
	}
	var d layer1EvictDecision
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return nil
	}
	d.Action = strings.ToLower(strings.TrimSpace(d.Action))
	return &d
}

func (at *AutoTrader) closeL2EvictCandidate(ctx context.Context, c *l2EvictCandidate, fill hlprovider.LeaderFill, l1Symbol string) error {
	if c == nil || c.Trader == nil {
		return fmt.Errorf("nil eviction candidate")
	}
	closeAction := "close_long"
	if c.Side == "short" {
		closeAction = "close_short"
	}
	decision := &kernel.Decision{
		Symbol:     c.Symbol,
		Action:     closeAction,
		Confidence: 100,
		Reasoning: fmt.Sprintf("[Copy L1] evict L2 %s %s (PnL %+.2f%%) for L1 %s tid=%d",
			c.TraderName, c.Symbol, c.PnLPct, shortCopyAddr(at.config.StrategyConfig.CopyConfig.LeaderAddress), fill.Tid),
	}
	record := store.DecisionAction{
		Action:    closeAction,
		Symbol:    c.Symbol,
		Reasoning: decision.Reasoning,
		Timestamp: time.Now().UTC(),
		Success:   true,
	}
	if err := c.Trader.executeDecisionWithRecord(decision, &record); err != nil {
		return err
	}
	pnlLabel := fmt.Sprintf("%+.2f%%", c.PnLPct)
	if c.PnLUSD != 0 {
		pnlLabel = fmt.Sprintf("%+.2f USD", c.PnLUSD)
	}
	msg := fmt.Sprintf("Closed L2 %s %s (%s) after 30m review to follow L1 %s on %s",
		c.Symbol, c.TraderName, pnlLabel, shortCopyAddr(at.config.StrategyConfig.CopyConfig.LeaderAddress), l1Symbol)
	events.EmitSystemAlert(events.SystemAlertEvent{
		TraderID:   at.id,
		TraderName: at.name,
		Type:       events.AlertCopyL2Evicted,
		Message:    msg,
		DedupeKey:  fmt.Sprintf("%s:%s:%s:%s", at.id, events.AlertCopyL2Evicted, c.Symbol, closeAction),
	})
	return nil
}

// PickL2EvictCandidateForTest exposes deterministic eviction ranking for unit tests.
func PickL2EvictCandidateForTest(candidates []l2EvictCandidate, l1Symbol, l1Side string) *l2EvictCandidate {
	if len(candidates) == 0 {
		return nil
	}
	sortEvictCandidates(candidates, l1Symbol, l1Side)
	return &candidates[0]
}

// DeterministicEvictScoreForTest exposes rank scoring for unit tests.
func DeterministicEvictScoreForTest(c l2EvictCandidate, l1Symbol, l1Side string, all []l2EvictCandidate) float64 {
	return deterministicEvictScore(c, l1Symbol, l1Side, all)
}
