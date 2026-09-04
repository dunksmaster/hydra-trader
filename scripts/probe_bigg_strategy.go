//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"nofx/auth"
	"os"
)

func main() {
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	t, _ := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "x@local")
	req, _ := http.NewRequest("GET", "https://nofx-production-fcd1.up.railway.app/api/strategies/e6e58a0f-5b1a-4a28-a472-9c6743311db4", nil)
	req.Header.Set("Authorization", "Bearer "+t)
	resp, _ := http.DefaultClient.Do(req)
	b, _ := io.ReadAll(resp.Body)
	var s map[string]any
	json.Unmarshal(b, &s)
	cfg, _ := s["config"].(map[string]any)
	ai, _ := cfg["ai_config"].(map[string]any)
	cs, _ := ai["coin_source"].(map[string]any)
	fmt.Printf("name=%v source=%v cat=%v dir=%v limit=%v\n", s["name"], cs["source_type"], cs["hyper_rank_category"], cs["hyper_rank_direction"], cs["hyper_rank_limit"])
}
