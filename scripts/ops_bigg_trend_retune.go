//go:build ignore

// Retune Crypto BigG: volume-ranked liquid perps + EMA/RSI/MACD trend following, bidirectional long/short.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"nofx/auth"
)

const (
	userID       = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	biggTraderID = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"
	biggStrategy = "b723efa8-729d-47cd-a71e-99429c639b6a"
)

func main() {
	mode := "apply"
	if len(os.Args) > 1 {
		mode = strings.ToLower(strings.TrimSpace(os.Args[1]))
	}

	base := os.Getenv("NOFX_BASE_URL")
	if base == "" {
		base = "https://nofx-production-fcd1.up.railway.app"
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	if len(auth.JWTSecret) == 0 {
		fatal("JWT_SECRET missing")
	}
	token, err := auth.GenerateJWT(userID, "ops-bigg-trend@local")
	if err != nil {
		fatal("jwt: %v", err)
	}
	c := &http.Client{Timeout: 120 * time.Second}

	getMap := func(path string) map[string]any {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := c.Do(req)
		if err != nil {
			fatal("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 300 {
			fatal("GET %s status=%d body=%s", path, resp.StatusCode, string(body))
		}
		var out map[string]any
		json.Unmarshal(body, &out)
		return out
	}
	put := func(path string, payload any) (int, string) {
		raw, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPut, base+path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			fatal("PUT %s: %v", path, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, strings.TrimSpace(string(b))
	}
	post := func(path string) (int, string) {
		req, _ := http.NewRequest(http.MethodPost, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := c.Do(req)
		if err != nil {
			fatal("POST %s: %v", path, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, strings.TrimSpace(string(b))
	}

	st := getMap("/api/strategies/" + biggStrategy)
	config, _ := st["config"].(map[string]any)
	ai, _ := config["ai_config"].(map[string]any)
	cs, _ := ai["coin_source"].(map[string]any)
	rc, _ := ai["risk_control"].(map[string]any)
	fmt.Printf("before: board=%s/%s limit=%v min_conf=%v block_loser_close=%v\n",
		cs["hyper_rank_category"], cs["hyper_rank_direction"], cs["hyper_rank_limit"],
		rc["min_confidence"], rc["block_ai_close_on_loss"])

	if mode == "snapshot" {
		return
	}
	noRestart := mode == "apply-only" || mode == "config-only"

	payload := map[string]any{
		"name":        st["name"],
		"description": "Bitget BigG: volume-ranked liquid perps, EMA trend follow, long+short from structure (no gainers chase).",
		"config": map[string]any{
			"ai_config": map[string]any{
				"coin_source": map[string]any{
					"source_type":          "hyper_rank",
					"hyper_rank_category":  "crypto",
					"hyper_rank_direction": "volume",
					"hyper_rank_limit":     8,
					"use_ai500":            false,
					"use_oi_top":           false,
					"use_oi_low":           false,
					"use_hyper_all":        false,
					"use_hyper_main":       false,
					"vergex_limit":         0,
				},
				"indicators": map[string]any{
					"enable_raw_klines":      true,
					"enable_ema":             true,
					"enable_rsi":             true,
					"enable_macd":            true,
					"enable_atr":             true,
					"enable_volume":          true,
					"enable_quant_data":      false,
					"enable_quant_oi":        false,
					"enable_oi_ranking":      false,
					"enable_netflow_ranking": false,
					"enable_price_ranking":   true,
					"price_ranking_limit":    10,
					"price_ranking_duration": "1h,4h",
					"ema_periods":            []int{20, 50},
					"rsi_periods":            []int{7, 14},
					"atr_periods":            []int{14},
					"klines": map[string]any{
						"enable_multi_timeframe": true,
						"primary_timeframe":      "15m",
						"primary_count":          30,
						"selected_timeframes":    []string{"15m", "1h", "4h"},
					},
				},
				"prompt_sections": map[string]any{
					"role_definition": roleDefinition,
					"entry_standards": entryStandards,
					"trading_frequency": `# Trend follow — both directions

The candidate board is VOLUME-ranked liquid perps only. It does NOT mean "go long".
Direction must come from trend structure on 15m + 1h (+ 4h context):
- Uptrend (price above EMA20>EMA50, higher highs): prefer open_long on pullback.
- Downtrend (price below EMA20<EMA50, lower lows): prefer open_short on rally rejection.
- Range/chop with no alignment: wait — do not force a trade.

Use market price ranking (1h/4h gainers vs losers) as context, not as a blind entry signal.
Never chase a coin only because it is up 24h; never avoid shorts only because the board is "movers".

Losers (negative margin PnL): hold only — code SL/TP exits them. Winners may close per normal rules.`,
					"decision_process": `# Decision Process

1. Review open positions first (hold / close per rules; never close underwater losers).
2. For each candidate, classify trend on 15m and 1h. If they disagree, skip.
3. If downtrend confirmed → open_short. If uptrend confirmed → open_long. Equal weight to both.
4. Confirm ATR: stop must sit outside noise (if 1 ATR > ~2% of price, wait).
5. Size/leverage are set in code. Output strict JSON only.`,
				},
				"custom_prompt": "Bidirectional trend follower. Shorts are first-class — do not default to long-only.",
				"risk_control": map[string]any{
					"max_positions":                    1,
					"min_confidence":                   72,
					"btc_eth_max_leverage":             10,
					"altcoin_max_leverage":             10,
					"btc_eth_max_position_value_ratio": 1.5,
					"altcoin_max_position_value_ratio": 1.5,
					"max_margin_usage":                 0.3,
					"min_position_size":                12,
					"min_risk_reward_ratio":            2,
					"block_ai_close_on_loss":           true,
				},
			},
		},
	}

	code, body := put("/api/strategies/"+biggStrategy, payload)
	fmt.Printf("strategy patch status=%d %s\n", code, trunc(body, 160))
	if code >= 300 {
		fatal("strategy patch failed")
	}

	// Reload trader with slightly faster scan for trend responsiveness.
	tr := getMap("/api/traders/" + biggTraderID + "/config")
	trPayload := map[string]any{
		"name":                  tr["trader_name"],
		"ai_model_id":           tr["ai_model"],
		"exchange_id":           tr["exchange_id"],
		"strategy_id":           biggStrategy,
		"scan_interval_minutes": 20,
	}
	if v, ok := tr["is_cross_margin"].(bool); ok {
		trPayload["is_cross_margin"] = v
	}
	if v, ok := tr["initial_balance"]; ok {
		trPayload["initial_balance"] = v
	}
	code, body = put("/api/traders/"+biggTraderID, trPayload)
	fmt.Printf("trader scan=20m status=%d %s\n", code, trunc(body, 120))

	if !noRestart {
		if fmt.Sprint(tr["is_running"]) == "true" {
			post("/api/traders/" + biggTraderID + "/stop")
			time.Sleep(2 * time.Second)
		}
		code, body = post("/api/traders/" + biggTraderID + "/start")
		fmt.Printf("restart status=%d %s\n", code, trunc(body, 120))
	} else {
		fmt.Println("apply-only: strategy patched, trader left running (reload on next cycle)")
	}

	st = getMap("/api/strategies/" + biggStrategy)
	config, _ = st["config"].(map[string]any)
	ai, _ = config["ai_config"].(map[string]any)
	cs, _ = ai["coin_source"].(map[string]any)
	rc, _ = ai["risk_control"].(map[string]any)
	fmt.Printf("\n✅ after: board=%s/%s limit=%v min_conf=%v block_loser_close=%v\n",
		cs["hyper_rank_category"], cs["hyper_rank_direction"], cs["hyper_rank_limit"],
		rc["min_confidence"], rc["block_ai_close_on_loss"])
	tr = getMap("/api/traders/" + biggTraderID + "/config")
	fmt.Printf("running=%v scan=%v min\n", tr["is_running"], tr["scan_interval_minutes"])
}

const roleDefinition = `# You are the NOFX Bitget crypto auto-trader

Trade liquid USDT perps from the volume-ranked candidate board (free Hyperliquid + Bitget klines).
NVIDIA decides entries; direction is trend-driven — long OR short. No Claw402/Vergex paid boards.`

const entryStandards = `# Entry Standards — trend follow, both directions

Candidate board = liquid symbols only. Your job is to read trend, not to buy every green candle.

Long setup (open_long):
- 15m and 1h: price above EMA20, EMA20 above EMA50, RSI not extreme overbought (>75).
- Enter on pullback to EMA20 or break-and-retest, not after a vertical spike.

Short setup (open_short):
- 15m and 1h: price below EMA20, EMA20 below EMA50, RSI not extreme oversold (<25).
- Enter on rally into EMA20 resistance or failed breakout, not after a vertical dump.

If 15m says up and 1h says down (or vice versa), wait.
If 4h strongly opposes 15m/1h, wait unless the intraday setup is exceptional.

Use price ranking (1h/4h gainers/losers) as market context — shorts are valid when structure is bearish even if BTC is green.`

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
