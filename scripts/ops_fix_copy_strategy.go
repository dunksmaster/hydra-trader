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

const strategyID = "00e95f8a-baf4-4d80-85fb-9ce5060e7fbb"

func main() {
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	token, _ := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "fix-copy@local")

	payload := map[string]any{
		"config": map[string]any{
			"strategy_type": "copy_trading",
			"language":      "en",
			"copy_config": map[string]any{
				"leader_address":    "0x6859da14835424957a1e6b397d8026b1d9ff7e1e",
				"size_mode":         "fixed_notional",
				"notional_usd":      15,
				"min_notional_usd":  12,
				"max_notional_pct":  45,
				"max_leverage":      10,
				"max_positions":     2,
				"exit_mode":         "leader_plus_stop",
				"safety_stop_pct":   15,
				"symbol_blocklist":  []string{"xyz:"},
				"dry_run":           false,
				"inverse":           false,
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
	fmt.Printf("update status=%d body=%s\n", resp.StatusCode, string(out))
	if resp.StatusCode >= 300 {
		os.Exit(1)
	}
}
