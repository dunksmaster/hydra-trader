//go:build ignore

// Retunes both live books onto a fixed 2:1 code-enforced payoff.
//
// Why: closed-trade stats showed both books had average loss well above average
// win (BigG 3.88 vs 2.10, Autopilot 2.44 vs 1.42) because the engine had a
// code-enforced profit lock but no loss cut. Winners were capped and losers ran
// until the model asked to close, which the hold gates then blocked as noise.
// Setting hard_stop_loss_margin_pct at half the profit lock fixes the payoff;
// the confidence and scan changes cut the fee burn that ate Autopilot's edge.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"nofx/auth"
)

const (
	userID = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	base   = "https://nofx-production-fcd1.up.railway.app"

	biggTrader   = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"
	biggStrategy = "e6e58a0f-5b1a-4a28-a472-9c6743311db4"

	apTrader   = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786632502"
	apStrategy = "2e50a1e7-cb16-4d0f-8ed7-c8ea6cda3ad3"
)

func main() {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		fatal("JWT_SECRET missing")
	}
	auth.SetJWTSecret(secret)
	token, err := auth.GenerateJWT(userID, "ops-payoff@local")
	if err != nil {
		fatal("jwt: %v", err)
	}
	c := &client{token: token, h: &http.Client{Timeout: 30 * time.Second}}

	// The loss cut is a new field. A binary built before it would silently drop
	// it on the config round trip, so confirm persistence on a probe write
	// before touching the rest of the config.
	if !waitForHardStopSupport(c) {
		fatal("deployed binary still drops hard_stop_loss_margin_pct; retry after the Railway deploy finishes")
	}

	applyBigG(c)
	applyAutopilot(c)

	fmt.Println("\n===== verification =====")
	report(c, "Crypto BigG (Bitget)", biggStrategy, biggTrader)
	report(c, "NOFX Autopilot (Hyperliquid)", apStrategy, apTrader)
}

// waitForHardStopSupport writes the loss cut and reads it back until it sticks.
func waitForHardStopSupport(c *client) bool {
	probe := map[string]any{"config": map[string]any{"ai_config": map[string]any{
		"risk_control": map[string]any{"hard_stop_loss_margin_pct": 7.5},
	}}}
	for attempt := 1; attempt <= 40; attempt++ {
		st, body := c.put("/api/strategies/"+biggStrategy, probe)
		if st >= 300 {
			fmt.Printf("probe %d: put status=%d %s\n", attempt, st, string(body))
		} else if v := riskValue(c, biggStrategy, "hard_stop_loss_margin_pct"); v != nil {
			fmt.Printf("hard stop supported after %d attempt(s) (value=%v)\n", attempt, v)
			return true
		}
		fmt.Printf("probe %d: field not persisted yet, waiting 30s...\n", attempt)
		time.Sleep(30 * time.Second)
	}
	return false
}

func applyBigG(c *client) {
	fmt.Println("\n===== Crypto BigG (Bitget) =====")
	patch := map[string]any{"config": map[string]any{"ai_config": map[string]any{
		"risk_control": map[string]any{
			"max_positions":                    2,
			"btc_eth_max_leverage":             5,
			"altcoin_max_leverage":             5,
			"btc_eth_max_position_value_ratio": 1,
			"altcoin_max_position_value_ratio": 1,
			"max_margin_usage":                 1,
			"min_position_size":                12,
			"min_risk_reward_ratio":            2,
			// Worst win rate of the two books (46.7%), so demand more.
			"min_confidence": 75,
			// At 5x: +15% margin is +3.0% price, -7.5% margin is -1.5% price.
			"hard_take_profit_margin_pct": 15,
			"hard_stop_loss_margin_pct":   7.5,
		},
		// 5m entry timing on a 20-35 minute scan was a mismatch: the signal was
		// stale before the next cycle could act on it.
		"indicators": map[string]any{"klines": map[string]any{
			"primary_timeframe":      "15m",
			"primary_count":          20,
			"enable_multi_timeframe": true,
			"selected_timeframes":    []string{"15m", "1h", "4h"},
		}},
		"prompt_sections": map[string]any{
			"trading_frequency": `# Trading Frequency and Hold Discipline

Target book: 2 concurrent positions at 5x, each about 1x equity in notional.

Code-enforced exits run before you are asked, on every cycle:
- Force close at +15% margin PnL, about +3.0% price at 5x.
- Force close at -7.5% margin PnL, about -1.5% price at 5x.

That is a fixed 2:1 payoff, so you only need to be right about 40% of the time
to make money. Never widen a loss hoping it comes back; the code will cut it
and a smaller loss is the whole edge.

Other code limits reject violating decisions:
- At most 2 positions total, 2 new opens per cycle, 3 per hour.
- Hold new positions at least 90 minutes.
- Do not close inside the -2% to +3% price band before 3 hours.
- After closing a symbol, wait 4 hours before re-entering it.

Set the exchange stop_loss slightly wider than the code stop, about -2.0%
price, so it only acts as a failsafe when the bot is offline. Set take_profit
at about +3.5% price or better.

A round trip costs about 0.12% of notional. Only take setups where +3% price is
a realistic target within a few hours. Do not scalp for 0.2-0.3%. Waiting a
cycle costs nothing, so wait when every candidate is conflicted.`,
			"entry_standards": `# Entry Standards

Never open on a single indicator. Require at least two independent signal
families to agree on direction:
1. Trend - EMA structure and higher-timeframe direction
2. Momentum - RSI, MACD
3. Structure and participation - price levels, volume, ATR-relative range

Use the 15m series for entry timing. The 1h series must not contradict the
direction you take. The 4h series is context; a mild 4h disagreement is not
enough to skip a second-slot fill.

The stop is a fixed -1.5% price move, so entry location matters as much as
direction. Do not buy the top of a stretched 15m move or sell the bottom of
one. Wait for the pullback or the break-and-retest so -1.5% sits outside
normal noise instead of one candle away.

Prefer BTC, ETH and SOL. For thinner names such as ZEC, require all three
signal families to agree rather than two.

Ranking position is not an entry reason.`,
		},
	}}}
	st, body := c.put("/api/strategies/"+biggStrategy, patch)
	fmt.Printf("strategy patch status=%d %s\n", st, trim(body))
	if st >= 300 {
		os.Exit(1)
	}
	// Faster cycle so the code stop reacts closer to the threshold.
	setScanInterval(c, biggTrader, biggStrategy, 20)
}

func applyAutopilot(c *client) {
	fmt.Println("\n===== NOFX Autopilot (Hyperliquid) =====")
	patch := map[string]any{"config": map[string]any{"ai_config": map[string]any{
		"risk_control": map[string]any{
			"max_positions":        2,
			"btc_eth_max_leverage": 10,
			"altcoin_max_leverage": 10,
			// Ratio 4 (not 5) keeps a margin cushion with both slots filled.
			"btc_eth_max_position_value_ratio": 4,
			"altcoin_max_position_value_ratio": 4,
			"max_margin_usage":                 1,
			"min_position_size":                12,
			"min_risk_reward_ratio":            2,
			// Fees were 77% of this book's net loss, so raise the entry bar.
			"min_confidence": 72,
			// At 10x: +20% margin is +2.0% price, -10% margin is -1.0% price.
			"hard_take_profit_margin_pct": 20,
			"hard_stop_loss_margin_pct":   10,
		},
		"prompt_sections": map[string]any{
			"trading_frequency": `# Dollar book with a fixed 2:1 payoff (Hyperliquid Autopilot)

Use the full allowed notional; idle cash is a wasted slot. Notional per
position is set in code to equity x 4 at 10x.

Code-enforced exits run before you are asked, on every cycle:
- Force close at +20% margin PnL, about +2.0% price at 10x.
- Force close at -10% margin PnL, about -1.0% price at 10x.

That is a fixed 2:1 payoff, so you only need to be right about 35-40% of the
time. Never average down and never hold a loser hoping it returns.

Every round trip costs roughly $0.25 in fees, which is real money against a
$4 target. Trade count is not the goal: 4-6 good closes a day beat 12 marginal
ones. If no candidate has clean 15m and 1h alignment, wait.

Hold at least 20 minutes unless price is already at or beyond -1.2% or +1.2%.
After a close, the same symbol can re-enter after 30 minutes.

Set the exchange stop_loss slightly wider than the code stop, about -1.3%
price, as an offline failsafe. Set take_profit at about +2.5% price.`,
			"entry_standards": `# Entry Standards

Open only when the 15m and 1h series agree on direction. Prefer BTC, ETH, SOL
and HYPE from the volume board.

The stop is a fixed -1.0% price move, which is tight, so entry location decides
the outcome. Do not chase a 15m candle that has already run. Enter on the
pullback or the break-and-retest so -1.0% sits outside normal noise.

Size is set in code to equity x 4 notional. Do not output a tiny
position_size_usd.`,
		},
	}}}
	st, body := c.put("/api/strategies/"+apStrategy, patch)
	fmt.Printf("strategy patch status=%d %s\n", st, trim(body))
	if st >= 300 {
		os.Exit(1)
	}
	// 10m was burning AI calls and fees for no edge.
	setScanInterval(c, apTrader, apStrategy, 15)
}

func setScanInterval(c *client, traderID, strategyID string, minutes int) {
	cfg, _ := c.getJSON("/api/traders/" + traderID + "/config").(map[string]any)
	payload := map[string]any{
		"name":                  cfg["trader_name"],
		"ai_model_id":           cfg["ai_model"],
		"exchange_id":           cfg["exchange_id"],
		"strategy_id":           strategyID,
		"scan_interval_minutes": minutes,
	}
	if v, ok := cfg["is_cross_margin"].(bool); ok {
		payload["is_cross_margin"] = v
	}
	if v, ok := cfg["initial_balance"]; ok {
		payload["initial_balance"] = v
	}
	st, body := c.put("/api/traders/"+traderID, payload)
	fmt.Printf("scan interval -> %dm status=%d %s\n", minutes, st, trim(body))
}

func report(c *client, label, strategyID, traderID string) {
	strategy, _ := c.getJSON("/api/strategies/" + strategyID).(map[string]any)
	cfg, _ := strategy["config"].(map[string]any)
	ai, _ := cfg["ai_config"].(map[string]any)
	risk, _ := ai["risk_control"].(map[string]any)
	coin, _ := ai["coin_source"].(map[string]any)
	traderCfg, _ := c.getJSON("/api/traders/" + traderID + "/config").(map[string]any)

	fmt.Printf("\n%s\n  risk=%s\n", label, must(risk))
	fmt.Printf("  coin_source_type=%v direction=%v limit=%v\n",
		coin["source_type"], coin["hyper_rank_direction"], coin["hyper_rank_limit"])
	fmt.Printf("  scan_min=%v running=%v\n",
		traderCfg["scan_interval_minutes"], traderCfg["is_running"])

	tp, _ := risk["hard_take_profit_margin_pct"].(float64)
	sl, _ := risk["hard_stop_loss_margin_pct"].(float64)
	if sl > 0 {
		fmt.Printf("  payoff = %.2f:1 (breakeven win rate %.0f%%)\n", tp/sl, 100*sl/(tp+sl))
	} else {
		fmt.Printf("  WARNING: no loss cut set\n")
	}
}

func riskValue(c *client, strategyID, key string) any {
	strategy, _ := c.getJSON("/api/strategies/" + strategyID).(map[string]any)
	cfg, _ := strategy["config"].(map[string]any)
	ai, _ := cfg["ai_config"].(map[string]any)
	risk, _ := ai["risk_control"].(map[string]any)
	if risk == nil {
		return nil
	}
	return risk[key]
}

type client struct {
	token string
	h     *http.Client
}

func (c *client) getJSON(path string) any {
	req, _ := http.NewRequest(http.MethodGet, base+path, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.h.Do(req)
	if err != nil {
		fatal("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		fatal("GET %s status=%d body=%s", path, resp.StatusCode, string(b))
	}
	var out any
	json.Unmarshal(b, &out)
	return out
}

func (c *client) put(path string, payload any) (int, []byte) {
	data, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPut, base+path, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.h.Do(req)
	if err != nil {
		return 0, []byte(err.Error())
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func must(v any) string { b, _ := json.Marshal(v); return string(b) }

func trim(b []byte) string {
	if len(b) > 220 {
		return string(b[:220]) + "..."
	}
	return string(b)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
