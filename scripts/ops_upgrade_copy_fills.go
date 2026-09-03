//go:build ignore

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
	strategyID     = "00e95f8a-baf4-4d80-85fb-9ce5060e7fbb"
	leaderAddress  = "0x66f889094739dbb7d20aa60f645acd88feba75a9"
	ownerUserID    = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
)

func main() {
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	dryRun := os.Getenv("COPY_DRY_RUN") != "false"

	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	token, err := auth.GenerateJWT(ownerUserID, "upgrade-copy-fills@local")
	if err != nil {
		panic(err)
	}

	payload := map[string]any{
		"config": map[string]any{
			"strategy_type": "copy_trading",
			"language":      "en",
			"copy_config": map[string]any{
				"leader_address":         leaderAddress,
				"copy_mode":              "fills",
				"size_mode":              "fixed_notional",
				"notional_usd":           25,
				"min_notional_usd":       12,
				"max_notional_pct":       45,
				"max_leverage":           10,
				"max_positions":          3,
				"exit_mode":              "leader_plus_stop",
				"safety_stop_pct":        15,
				"symbol_blocklist":       []string{},
				"reconcile_interval_sec": 60,
				"copy_on_start":          true,
				"min_leader_fill_usd":    10,
				"dry_run":                dryRun,
				"inverse":                false,
			},
		},
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPut, baseURL+"/api/strategies/"+strategyID, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	out, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Printf("upgrade copy fills status=%d dry_run=%v body=%s\n", resp.StatusCode, dryRun, string(out))
	if resp.StatusCode >= 300 {
		os.Exit(1)
	}
	fmt.Println("Restart Autopilot Copy trader to pick up fills mode. Set COPY_DRY_RUN=false for live mirroring.")
}
