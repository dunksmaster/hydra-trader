//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"nofx/auth"
)

func main() {
	userID := "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	secret := os.Getenv("JWT_SECRET")
	auth.SetJWTSecret(secret)
	token, _ := auth.GenerateJWT(userID, "probe@local")
	base := "https://nofx-production-fcd1.up.railway.app"
	h := http.DefaultClient

	get := func(path string) any {
		req, _ := http.NewRequest("GET", base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := h.Do(req)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		var out any
		json.Unmarshal(b, &out)
		return out
	}

	fmt.Println("=== models ===")
	models, _ := get("/api/models").([]any)
	for _, raw := range models {
		m, _ := raw.(map[string]any)
		fmt.Printf("id=%v provider=%v enabled=%v model=%v\n", m["id"], m["provider"], m["enabled"], m["custom_model_name"])
	}

	fmt.Println("\n=== traders config ===")
	for _, id := range []string{
		"8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786632502",
		"8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332",
	} {
		req, _ := http.NewRequest("GET", base+"/api/traders/"+id+"/config", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, _ := h.Do(req)
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var cfg map[string]any
		json.Unmarshal(b, &cfg)
		fmt.Printf("%s ai_model=%v strategy=%v running=%v\n", cfg["trader_name"], cfg["ai_model"], cfg["strategy_id"], cfg["is_running"])
	}

	fmt.Println("\n=== BigG strategy coin source ===")
	strategies, _ := get("/api/strategies").([]any)
	for _, raw := range strategies {
		s, _ := raw.(map[string]any)
		name, _ := s["name"].(string)
		if !strings.Contains(strings.ToLower(name), "claw402") {
			continue
		}
		id, _ := s["id"].(string)
		detail, _ := get("/api/strategies/"+id).(map[string]any)
		cfg, _ := detail["config"].(map[string]any)
		ai, _ := cfg["ai_config"].(map[string]any)
		cs, _ := ai["coin_source"].(map[string]any)
		fmt.Printf("strategy=%q source_type=%v category=%v limit=%v\n", name, cs["source_type"], cs["hyper_rank_category"], cs["hyper_rank_limit"])
	}
}
