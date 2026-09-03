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
	t, _ := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "probe@local")
	base := "https://nofx-production-fcd1.up.railway.app"
	bigg := "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"

	get := func(path string) {
		req, _ := http.NewRequest("GET", base+path, nil)
		req.Header.Set("Authorization", "Bearer "+t)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		fmt.Printf("\n=== GET %s (status=%d) ===\n", path, resp.StatusCode)
		var pretty any
		if json.Unmarshal(b, &pretty) == nil {
			out, _ := json.MarshalIndent(pretty, "", "  ")
			if len(out) > 4000 {
				fmt.Println(string(out[:4000]) + "\n...truncated...")
			} else {
				fmt.Println(string(out))
			}
		} else {
			fmt.Println(string(b))
		}
	}

	get("/api/account?trader_id=" + bigg)
	get("/api/positions?trader_id=" + bigg)
	get("/api/positions/history?trader_id=" + bigg + "&limit=5")
	get("/api/statistics/full?trader_id=" + bigg)
	get("/api/strategies/e6e58a0f-5b1a-4a28-a472-9c6743311db4")
}
