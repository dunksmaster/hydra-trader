package trader

import (
	"fmt"
	"strings"

	"nofx/kernel"
)

// injectHardTakeProfits applies the strategy's margin-based profit lock. The
// value is intentionally strategy-scoped: zero disables it, so unrelated
// traders are never silently forced onto Autopilot's profit-taking policy.
func (at *AutoTrader) injectHardTakeProfits(decisions []kernel.Decision, ctx *kernel.Context) []kernel.Decision {
	if at == nil || at.config.StrategyConfig == nil || ctx == nil || len(ctx.Positions) == 0 {
		return decisions
	}
	threshold := at.config.StrategyConfig.RiskControl.HardTakeProfitMarginPct
	if threshold <= 0 {
		return decisions
	}

	forced := make([]kernel.Decision, 0, 2)
	closing := make(map[string]bool)
	for _, pos := range ctx.Positions {
		if pos.UnrealizedPnLPct < threshold {
			continue
		}
		action := "close_long"
		if strings.EqualFold(pos.Side, "short") {
			action = "close_short"
		}
		base := hardTPBase(pos.Symbol)
		closing[base] = true
		forced = append(forced, kernel.Decision{
			Symbol:     pos.Symbol,
			Action:     action,
			Confidence: 100,
			Reasoning: fmt.Sprintf(
				"code-enforced take-profit: unrealized %.1f%% >= %.1f%%, close to lock profit and free a slot",
				pos.UnrealizedPnLPct, threshold,
			),
		})
	}
	if len(forced) == 0 {
		return decisions
	}

	kept := make([]kernel.Decision, 0, len(decisions))
	for _, d := range decisions {
		if closing[hardTPBase(d.Symbol)] {
			continue
		}
		kept = append(kept, d)
	}
	return sortDecisionsByPriority(append(forced, kept...))
}

func hardTPBase(symbol string) string {
	s := strings.ToUpper(strings.TrimSpace(symbol))
	s = strings.TrimPrefix(s, "XYZ:")
	s = strings.TrimSuffix(s, "USDT")
	s = strings.TrimSuffix(s, "USDC")
	return strings.Trim(s, "-_")
}
