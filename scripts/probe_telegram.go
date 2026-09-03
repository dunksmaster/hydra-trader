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
	token, _ := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "tg-probe@local")
	base := "https://nofx-production-fcd1.up.railway.app"

	req, _ := http.NewRequest("GET", base+"/api/telegram", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	fmt.Printf("telegram API status=%d\n%s\n", resp.StatusCode, string(b))

	var wrap map[string]any
	_ = json.Unmarshal(b, &wrap)
}
