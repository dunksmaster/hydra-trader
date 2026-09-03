//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"

	"nofx/auth"
)

const (
	userID     = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	biggTrader = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"
	apTrader   = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786632502"
	biggStrat  = "e6e58a0f-5b1a-4a28-a472-9c6743311db4"
	apStrat    = "2e50a1e7-cb16-4d0f-8ed7-c8ea6cda3ad3"
	base       = "https://nofx-production-fcd1.up.railway.app"
)

func main() {
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	t, _ := auth.GenerateJWT(userID, "probe@local")
	c := &client{t}

	for _, id := range []string{biggTrader, apTrader} {
		fmt.Printf("\n======== TRADER %s ========\n", id[len(id)-10:])
		cfg, _ := c.getJSON("/api/traders/" + id + "/config").(map[string]any)
		fmt.Printf("name=%v running=%v scan=%v model=%v strategy=%v cross=%v\n",
			cfg["trader_name"], cfg["is_running"], cfg["scan_interval_minutes"], cfg["ai_model"], cfg["strategy_id"], cfg["is_cross_margin"])
		acct, _ := c.getJSON("/api/account?trader_id=" + id).(map[string]any)
		fmt.Printf("equity=%v avail=%v pos=%v margin_used_pct=%v\n",
			acct["total_equity"], acct["available_balance"], acct["position_count"], acct["margin_used_pct"])
		posRaw, _ := c.get("/api/positions?trader_id=" + id)
		fmt.Printf("positions=%s\n", trim(string(posRaw), 400))
	}

	biggAI := aiConfig(c, biggStrat)
	apAI := aiConfig(c, apStrat)
	fmt.Println("\n======== STRATEGY DIFF (BigG vs Autopilot) ========")
	compareRisk(biggAI, apAI)
	compareCoin(biggAI, apAI)
	compareIndicators(biggAI, apAI)

	// recent BigG cycle
	fmt.Println("\n======== RECENT BigG LOG CHECK ========")
	decRaw, _ := c.get("/api/decisions?trader_id=" + biggTrader + "&limit=3")
	fmt.Println(trim(string(decRaw), 1200))
}

type client struct{ token string }

func (c *client) get(path string) ([]byte, error) {
	req, _ := http.NewRequest("GET", base+path, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (c *client) getJSON(path string) any {
	b, err := c.get(path)
	if err != nil {
		panic(err)
	}
	var out any
	json.Unmarshal(b, &out)
	return out
}

func aiConfig(c *client, stratID string) map[string]any {
	s, _ := c.getJSON("/api/strategies/" + stratID).(map[string]any)
	cfg, _ := s["config"].(map[string]any)
	ai, _ := cfg["ai_config"].(map[string]any)
	return ai
}

func compareRisk(b, a map[string]any) {
	br, _ := b["risk_control"].(map[string]any)
	ar, _ := a["risk_control"].(map[string]any)
	keys := []string{
		"max_positions", "btc_eth_max_leverage", "altcoin_max_leverage",
		"btc_eth_max_position_value_ratio", "altcoin_max_position_value_ratio",
		"max_margin_usage", "min_confidence", "min_position_size",
		"hard_take_profit_margin_pct", "hard_stop_loss_margin_pct", "min_risk_reward_ratio",
	}
	for _, k := range keys {
		bv, av := br[k], ar[k]
		flag := " "
		if !reflect.DeepEqual(bv, av) {
			flag = "*"
		}
		fmt.Printf("%s %-32s BigG=%v  AP=%v\n", flag, k, bv, av)
	}
}

func compareCoin(b, a map[string]any) {
	bc, _ := b["coin_source"].(map[string]any)
	ac, _ := a["coin_source"].(map[string]any)
	keys := []string{"source_type", "hyper_rank_category", "hyper_rank_direction", "hyper_rank_limit", "use_oi_top"}
	for _, k := range keys {
		bv, av := bc[k], ac[k]
		flag := " "
		if fmt.Sprint(bv) != fmt.Sprint(av) {
			flag = "*"
		}
		fmt.Printf("%s coin.%s BigG=%v AP=%v\n", flag, k, bv, av)
	}
}

func compareIndicators(b, a map[string]any) {
	bi, _ := b["indicators"].(map[string]any)
	ai, _ := a["indicators"].(map[string]any)
	keys := []string{"enable_quant_data", "enable_oi", "enable_raw_klines", "enable_atr"}
	for _, k := range keys {
		bv, av := bi[k], ai[k]
		flag := " "
		if fmt.Sprint(bv) != fmt.Sprint(av) {
			flag = "*"
		}
		fmt.Printf("%s ind.%s BigG=%v AP=%v\n", flag, k, bv, av)
	}
}

func trim(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
