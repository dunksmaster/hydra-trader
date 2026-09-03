package trader

import (
	"fmt"
	"strings"

	"nofx/kernel"
)

// injectHardExits applies the strategy's code-enforced margin exits: the profit
// lock and the loss cut. Both values are strategy-scoped and zero disables
// them, so unrelated traders are never silently forced onto Autopilot's policy.
//
// The loss cut is what keeps the book's payoff ratio above 1:1. Without it the
// profit lock caps every winner while losers stay open until the model asks to
// close, and the hold gates then block small-loss closes as noise, so average
// loss grows past average win no matter how good entries are.
func (at *AutoTrader) injectHardExits(decisions []kernel.Decision, ctx *kernel.Context) []kernel.Decision {
	if at == nil || at.config.StrategyConfig == nil || ctx == nil || len(ctx.Positions) == 0 {
		return decisions
	}
	takeProfit := at.config.StrategyConfig.RiskControl.HardTakeProfitMarginPct
	stopLoss := at.config.StrategyConfig.RiskControl.HardStopLossMarginPct
	if takeProfit <= 0 && stopLoss <= 0 {
		return decisions
	}

	forced := make([]kernel.Decision, 0, 2)
	closing := make(map[string]bool)
	for _, pos := range ctx.Positions {
		if owned, ok := at.overflowOwnedSide(pos.Symbol); ok && strings.EqualFold(owned, pos.Side) {
			continue
		}
		var reason string
		switch {
		case takeProfit > 0 && pos.UnrealizedPnLPct >= takeProfit:
			reason = fmt.Sprintf(
				"code-enforced take-profit: unrealized %.1f%% >= %.1f%%, close to lock profit and free a slot",
				pos.UnrealizedPnLPct, takeProfit,
			)
		case stopLoss > 0 && pos.UnrealizedPnLPct <= -stopLoss:
			reason = fmt.Sprintf(
				"code-enforced stop-loss: unrealized %.1f%% <= -%.1f%%, close to cap the loss and free a slot",
				pos.UnrealizedPnLPct, stopLoss,
			)
		default:
			continue
		}

		action := "close_long"
		if strings.EqualFold(pos.Side, "short") {
			action = "close_short"
		}
		closing[hardExitBase(pos.Symbol)] = true
		forced = append(forced, kernel.Decision{
			Symbol:     pos.Symbol,
			Action:     action,
			Confidence: 100,
			Reasoning:  reason,
		})
	}
	if len(forced) == 0 {
		return decisions
	}

	kept := make([]kernel.Decision, 0, len(decisions))
	for _, d := range decisions {
		if closing[hardExitBase(d.Symbol)] {
			continue
		}
		kept = append(kept, d)
	}
	return sortDecisionsByPriority(append(forced, kept...))
}

func hardExitBase(symbol string) string {
	s := strings.ToUpper(strings.TrimSpace(symbol))
	s = strings.TrimPrefix(s, "XYZ:")
	s = strings.TrimSuffix(s, "USDT")
	s = strings.TrimSuffix(s, "USDC")
	return strings.Trim(s, "-_")
}
