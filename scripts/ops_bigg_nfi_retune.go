//go:build ignore

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"nofx/auth"
)

const (
	userID         = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	biggTraderID   = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"
	biggStrategyID = "e6e58a0f-5b1a-4a28-a472-9c6743311db4"
)

type client struct {
	base  string
	token string
	h     *http.Client
}

func main() {
	mode := "apply"
	if len(os.Args) > 1 {
		mode = strings.ToLower(strings.TrimSpace(os.Args[1]))
	}

	base := os.Getenv("NOFX_BASE_URL")
	if base == "" {
		base = "https://nofx-production-fcd1.up.railway.app"
	}
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		fatal("JWT_SECRET missing")
	}
	auth.SetJWTSecret(secret)
	token, err := auth.GenerateJWT(userID, "ops-nfi-retune@local")
	if err != nil {
		fatal("jwt: %v", err)
	}
	c := &client{base: base, token: token, h: http.DefaultClient}

	strategyID := findBigGStrategyID(c)
	if strategyID == "" {
		strategyID = biggStrategyID
	}

	switch mode {
	case "snapshot", "get":
		snapshot(c, strategyID)
	case "apply", "patch":
		snapshot(c, strategyID)
		applyPatch(c, strategyID)
		snapshot(c, strategyID)
	case "verify":
		snapshot(c, strategyID)
	default:
		fatal("unknown mode %q (use snapshot|apply|verify)", mode)
	}
}

func findBigGStrategyID(c *client) string {
	traders := c.getJSON("/api/my-traders")
	traderList, _ := traders.([]any)
	for _, raw := range traderList {
		t, _ := raw.(map[string]any)
		if t == nil {
			continue
		}
		if t["trader_id"] == biggTraderID {
			if sid, _ := t["strategy_id"].(string); sid != "" {
				return sid
			}
		}
	}
	return ""
}

func snapshot(c *client, strategyID string) {
	strategyRaw := c.getJSON("/api/strategies/" + strategyID)
	strategy, ok := strategyRaw.(map[string]any)
	if !ok {
		fatal("strategy response invalid")
	}
	config, ok := strategy["config"].(map[string]any)
	if !ok {
		fatal("strategy config missing")
	}
	aiConfig, ok := config["ai_config"].(map[string]any)
	if !ok {
		fatal("ai_config missing")
	}

	fmt.Printf("=== strategy %q id=%s ===\n", strategy["name"], strategyID)

	coinSource, _ := aiConfig["coin_source"].(map[string]any)
	printSection("coin_source", coinSource)

	indicators, _ := aiConfig["indicators"].(map[string]any)
	if klines, ok := indicators["klines"].(map[string]any); ok {
		printSection("indicators.klines", klines)
	}
	indicatorToggles := map[string]any{}
	for _, key := range []string{
		"enable_ema", "enable_rsi", "enable_macd", "enable_atr", "enable_volume",
		"ema_periods", "rsi_periods", "atr_periods",
	} {
		if v, ok := indicators[key]; ok {
			indicatorToggles[key] = v
		}
	}
	printSection("indicator_toggles", indicatorToggles)

	promptSections, _ := aiConfig["prompt_sections"].(map[string]any)
	if promptSections != nil {
		for _, key := range []string{"role_definition", "trading_frequency", "entry_standards", "decision_process"} {
			if v, ok := promptSections[key].(string); ok && v != "" {
				preview := v
				if len(preview) > 120 {
					preview = preview[:120] + "..."
				}
				fmt.Printf("prompt_sections.%s=%q\n", key, preview)
			}
		}
	}

	riskControl, _ := aiConfig["risk_control"].(map[string]any)
	printSection("risk_control", riskControl)
}

func printSection(name string, m map[string]any) {
	if m == nil {
		fmt.Printf("%s=<nil>\n", name)
		return
	}
	data, _ := json.Marshal(m)
	fmt.Printf("%s=%s\n", name, string(data))
}

func applyPatch(c *client, strategyID string) {
	patch := map[string]any{
		"config": map[string]any{
			"ai_config": map[string]any{
				"coin_source": map[string]any{
					"vergex_market_type": "",
					"vergex_chain":       "",
					"vergex_limit":       0,
					"vergex_liq_band":    "",
				},
				"indicators": map[string]any{
					"klines": map[string]any{
						"primary_timeframe":      "5m",
						"primary_count":          20,
						"selected_timeframes":    []string{"5m", "15m", "1h", "4h"},
						"enable_multi_timeframe": true,
					},
					"enable_ema":    true,
					"enable_rsi":    true,
					"enable_macd":   true,
					"enable_atr":    true,
					"enable_volume": true,
					"ema_periods":   []int{20, 50},
					"rsi_periods":   []int{7, 14},
					"atr_periods":   []int{14},
				},
				"prompt_sections": map[string]any{
					"role_definition":   roleDefinition,
					"trading_frequency": tradingFrequency,
					"entry_standards":   entryStandards,
					"decision_process":  decisionProcess,
				},
				"risk_control": map[string]any{
					"max_positions":                    2,
					"min_confidence":                   70,
					"btc_eth_max_leverage":             5,
					"altcoin_max_leverage":             5,
					"btc_eth_max_position_value_ratio": 1,
					"altcoin_max_position_value_ratio": 1,
					"max_margin_usage":                 1,
					"min_position_size":                12,
					"min_risk_reward_ratio":            2,
				},
			},
		},
	}

	status, body := c.put("/api/strategies/"+strategyID, patch)
	fmt.Printf("patch status=%d body=%s\n", status, string(body))
}

const roleDefinition = `# You are the Crypto BigG auto-trader on Bitget USDT perpetuals

Trade only the USDT-quoted perpetual contracts in this cycle's candidate list. The candidate pool is a crypto ranking board: it defines what you may trade, never why you should trade it. Never invent tickers or trade outside the provided universe.`

const tradingFrequency = `# Trading Frequency and Hold Discipline

Target book: 2 concurrent positions. If you already hold one and a second candidate qualifies, open the second this cycle. Do not sit on a single fill when another name has a valid setup.

These limits are enforced in code; decisions that violate them are rejected:
- At most 2 positions total, 2 new opens per cycle, 3 per hour.
- Hold new positions at least 90 minutes.
- Do not close inside the -2% to +3% price-move band before 3 hours.
- After closing a symbol, wait 4 hours before re-entering it.
- Only two things bypass those hold gates: price move at or below -3%, or at or above +8%.

A round trip costs roughly 0.1% of notional, so only take setups whose realistic target clears fees by a wide margin: stops around -3%, targets around +8% or beyond. Do not scalp for 0.2-0.3%. Wait only when every remaining candidate is conflicted.`

const entryStandards = `# Entry Standards

Never open on a single indicator. Require at least two independent signal families to agree on direction:
1. Trend - EMA structure and higher-timeframe direction
2. Momentum - RSI, MACD
3. Structure and participation - price levels, volume, ATR-relative range

Use the 5m series for entry timing only. The 15m and 1h series must not contradict the direction you take. The 4h series is context; a mild 4h disagreement is not enough to skip a second-slot fill.

Ranking position is not an entry reason. Prefer filling the second slot over waiting when two signal families agree on 15m/1h.`

const decisionProcess = `# Decision Process

1. Review existing positions first, respecting the hold and cooldown gates: take profit, stop out, or hold.
2. Count free slots. Target 2 open positions. If one slot is free, pick the best unused candidate and open it when two signal families agree.
3. Confirm 15m/1h do not contradict the entry. Place stops around -3% and targets around +8%.
4. Write concise reasoning, then output strict JSON. Include an open for the second slot when it qualifies — do not output only hold.`

func (c *client) getJSON(path string) any {
	req, _ := http.NewRequest(http.MethodGet, c.base+path, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.h.Do(req)
	if err != nil {
		fatal("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		fatal("GET %s status=%d body=%s", path, resp.StatusCode, string(body))
	}
	var out any
	if err := json.Unmarshal(body, &out); err != nil {
		fatal("GET %s json: %v", path, err)
	}
	return out
}

func (c *client) put(path string, payload any) (int, []byte) {
	data, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPut, c.base+path, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.h.Do(req)
	if err != nil {
		fatal("PUT %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
