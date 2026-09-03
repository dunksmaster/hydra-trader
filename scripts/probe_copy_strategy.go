//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"nofx/auth"
	"nofx/store"
)

func main() {
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	token, _ := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "probe@local")

	req, _ := http.NewRequest(http.MethodGet, baseURL+"/api/strategies/00e95f8a-baf4-4d80-85fb-9ce5060e7fbb", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := http.DefaultClient.Do(req)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Printf("strategy status=%d\n%s\n\n", resp.StatusCode, string(body))

	var parsed store.StrategyConfig
	_ = json.Unmarshal(body, &parsed)
	// body is wrapped - try config field
	var wrap struct {
		Config json.RawMessage `json:"config"`
	}
	_ = json.Unmarshal(body, &wrap)
	if len(wrap.Config) > 0 {
		var cfg store.StrategyConfig
		_ = json.Unmarshal(wrap.Config, &cfg)
		fmt.Printf("parsed type=%q copy=%v leader=%q\n", cfg.StrategyType, cfg.CopyConfig != nil, func() string {
			if cfg.CopyConfig == nil {
				return ""
			}
			return cfg.CopyConfig.LeaderAddress
		}())
	}
}
