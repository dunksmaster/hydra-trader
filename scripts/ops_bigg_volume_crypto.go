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
	userID         = "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	biggStrategyID = "e6e58a0f-5b1a-4a28-a472-9c6743311db4"
)

func main() {
	mode := "apply"
	if len(os.Args) > 1 {
		mode = os.Args[1]
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
	token, err := auth.GenerateJWT(userID, "ops-bigg-volume@local")
	if err != nil {
		fatal("jwt: %v", err)
	}

	c := &http.Client{}
	get := func(path string) map[string]any {
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

	strategy := get("/api/strategies/" + biggStrategyID)
	config, _ := strategy["config"].(map[string]any)
	ai, _ := config["ai_config"].(map[string]any)
	cs, _ := ai["coin_source"].(map[string]any)

	fmt.Printf("before: source_type=%v category=%v direction=%v limit=%v\n",
		cs["source_type"], cs["hyper_rank_category"], cs["hyper_rank_direction"], cs["hyper_rank_limit"])

	if mode == "snapshot" || mode == "get" {
		return
	}

	patch := map[string]any{
		"config": map[string]any{
			"ai_config": map[string]any{
				"coin_source": map[string]any{
					"source_type":            "hyper_rank",
					"hyper_rank_category":    "crypto",
					"hyper_rank_direction":   "volume",
					"hyper_rank_limit":       5,
					"vergex_market_type":     "",
					"vergex_chain":           "",
					"vergex_limit":           0,
					"vergex_liq_band":        "",
				},
				"indicators": map[string]any{
					"enable_quant_data": false,
					"enable_quant_oi":   false,
				},
			},
		},
	}

	data, _ := json.Marshal(patch)
	req, _ := http.NewRequest(http.MethodPut, base+"/api/strategies/"+biggStrategyID, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		fatal("PUT: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("patch status=%d body=%s\n", resp.StatusCode, string(body))

	strategy = get("/api/strategies/" + biggStrategyID)
	config, _ = strategy["config"].(map[string]any)
	ai, _ = config["ai_config"].(map[string]any)
	cs, _ = ai["coin_source"].(map[string]any)
	ind, _ := ai["indicators"].(map[string]any)
	fmt.Printf("after: source_type=%v category=%v direction=%v limit=%v quant_data=%v\n",
		cs["source_type"], cs["hyper_rank_category"], cs["hyper_rank_direction"], cs["hyper_rank_limit"], ind["enable_quant_data"])
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
