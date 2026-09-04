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

func main() {
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	token, _ := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "x@local")
	req, _ := http.NewRequest("GET", "https://nofx-production-fcd1.up.railway.app/api/traders/8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_openai_1787396193/config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := http.DefaultClient.Do(req)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Printf("status=%d\n", resp.StatusCode)
	var cfg map[string]any
	json.Unmarshal(body, &cfg)
	fmt.Printf("exchange_id=%v\n", cfg["exchange_id"])
}
