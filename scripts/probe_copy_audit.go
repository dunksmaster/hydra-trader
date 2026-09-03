//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"nofx/auth"
)

const (
	userID     = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	copyTrader = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_openai_1787127468"
	strategyID = "00e95f8a-baf4-4d80-85fb-9ce5060e7fbb"
)

func main() {
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET missing")
		os.Exit(1)
	}
	auth.SetJWTSecret(secret)
	token, _ := auth.GenerateJWT(userID, "copy-audit@local")
	client := &http.Client{}

	get := func(path string) (map[string]any, int) {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var out map[string]any
		_ = json.Unmarshal(body, &out)
		return out, resp.StatusCode
	}

	tg, tgStatus := get("/api/telegram")
	fmt.Printf("=== Telegram (HTTP %d) ===\n", tgStatus)
	fmt.Printf("configured=%v is_bound=%v username=%v model_id=%v\n",
		tg["configured"], tg["is_bound"], tg["username"], tg["model_id"])

	st, stStatus := get("/api/strategies/" + strategyID)
	fmt.Printf("\n=== Copy strategy (HTTP %d) ===\n", stStatus)
	cfg, _ := st["config"].(map[string]any)
	copyCfg, _ := cfg["copy_config"].(map[string]any)
	fmt.Printf("strategy_type=%v\n", cfg["strategy_type"])
	fmt.Printf("leader_address=%v\n", copyCfg["leader_address"])
	fmt.Printf("copy_mode=%v dry_run=%v notional_usd=%v max_positions=%v\n",
		copyCfg["copy_mode"], copyCfg["dry_run"], copyCfg["notional_usd"], copyCfg["max_positions"])
	fmt.Printf("symbol_blocklist=%v reconcile_interval_sec=%v\n",
		copyCfg["symbol_blocklist"], copyCfg["reconcile_interval_sec"])

	status, _ := get("/api/status?trader_id=" + copyTrader)
	fmt.Printf("\n=== Autopilot Copy status ===\n")
	fmt.Printf("running check via config endpoint\n")
	fmt.Printf("equity=%v available=%v position_count=%v strategy_type=%v\n",
		status["total_equity"], status["available_balance"], status["position_count"], status["strategy_type"])
	if pos, ok := status["positions"].([]any); ok {
		fmt.Printf("open_legs=%d\n", len(pos))
		for _, raw := range pos {
			p, _ := raw.(map[string]any)
			if p == nil {
				continue
			}
			fmt.Printf("  %v %v qty=%v entry=%v\n", p["symbol"], p["side"], p["quantity"], p["entry_price"])
		}
	}

	cfgTrader, _ := get("/api/traders/" + copyTrader + "/config")
	fmt.Printf("\n=== Trader config ===\n")
	fmt.Printf("is_running=%v name=%v exchange=%v\n",
		cfgTrader["is_running"], cfgTrader["name"], cfgTrader["exchange_id"])
}
