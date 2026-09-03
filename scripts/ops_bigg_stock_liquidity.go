//go:build ignore

// Retune Crypto BigG: stock universe + liquidity-aware leverage caps on Bitget.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"nofx/auth"
)

const (
	userID         = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	biggTraderID   = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"
	biggStrategyID = "b723efa8-729d-47cd-a71e-99429c639b6a"
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
	token, err := auth.GenerateJWT(userID, "bigg-stock@local")
	if err != nil {
		panic(err)
	}
	client := &http.Client{}

	get := func(path string) map[string]any {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 300 {
			panic(fmt.Sprintf("GET %s status=%d body=%s", path, resp.StatusCode, string(body)))
		}
		var out map[string]any
		_ = json.Unmarshal(body, &out)
		return out
	}

	// Verify strategy is bound to BigG.
	trCfg := get("/api/traders/" + biggTraderID + "/config")
	if fmt.Sprint(trCfg["strategy_id"]) != biggStrategyID {
		fmt.Printf("warning: BigG strategy_id=%v expected %s\n", trCfg["strategy_id"], biggStrategyID)
	}

	strategy := get("/api/strategies/" + biggStrategyID)
	config, _ := strategy["config"].(map[string]any)
	ai, _ := config["ai_config"].(map[string]any)
	cs, _ := ai["coin_source"].(map[string]any)
	risk, _ := ai["risk_control"].(map[string]any)
	fmt.Printf("before: category=%v direction=%v max_pos=%v leverage=%v/%v margin=%v\n",
		cs["hyper_rank_category"], cs["hyper_rank_direction"],
		risk["max_positions"], risk["btc_eth_max_leverage"], risk["altcoin_max_leverage"], risk["max_margin_usage"])

	patch := map[string]any{
		"config": map[string]any{
			"ai_config": map[string]any{
				"coin_source": map[string]any{
					"source_type":          "hyper_rank",
					"hyper_rank_category":  "stock",
					"hyper_rank_direction": "volume",
					"hyper_rank_limit":     10,
				},
				"risk_control": map[string]any{
					"max_positions":                    3,
					"btc_eth_max_leverage":             10,
					"altcoin_max_leverage":             10,
					"btc_eth_max_position_value_ratio": 1.5,
					"altcoin_max_position_value_ratio": 1.5,
					"max_margin_usage":                 0.6,
					"min_position_size":                12,
				},
			},
		},
	}
	body, _ := json.Marshal(patch)
	req, _ := http.NewRequest(http.MethodPut, baseURL+"/api/strategies/"+biggStrategyID, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	fmt.Printf("update status=%d body=%s\n", resp.StatusCode, string(out))
	if resp.StatusCode >= 300 {
		os.Exit(1)
	}

	strategy = get("/api/strategies/" + biggStrategyID)
	config, _ = strategy["config"].(map[string]any)
	ai, _ = config["ai_config"].(map[string]any)
	cs, _ = ai["coin_source"].(map[string]any)
	risk, _ = ai["risk_control"].(map[string]any)
	fmt.Printf("after: category=%v direction=%v max_pos=%v leverage=%v/%v margin=%v\n",
		cs["hyper_rank_category"], cs["hyper_rank_direction"],
		risk["max_positions"], risk["btc_eth_max_leverage"], risk["altcoin_max_leverage"], risk["max_margin_usage"])
}
