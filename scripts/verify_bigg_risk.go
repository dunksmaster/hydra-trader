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
	t, _ := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "v@local")
	req, _ := http.NewRequest("GET", "https://nofx-production-fcd1.up.railway.app/api/strategies/b723efa8-729d-47cd-a71e-99429c639b6a", nil)
	req.Header.Set("Authorization", "Bearer "+t)
	resp, _ := http.DefaultClient.Do(req)
	b, _ := io.ReadAll(resp.Body)
	var s map[string]any
	json.Unmarshal(b, &s)
	cfg, _ := s["config"].(map[string]any)
	ai, _ := cfg["ai_config"].(map[string]any)
	risk, _ := ai["risk_control"].(map[string]any)
	rb, _ := json.MarshalIndent(risk, "", "  ")
	fmt.Println(string(rb))
	req2, _ := http.NewRequest("GET", "https://nofx-production-fcd1.up.railway.app/api/traders/8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332/config", nil)
	req2.Header.Set("Authorization", "Bearer "+t)
	resp2, _ := http.DefaultClient.Do(req2)
	b2, _ := io.ReadAll(resp2.Body)
	fmt.Printf("trader config: %s\n", string(b2))
}
