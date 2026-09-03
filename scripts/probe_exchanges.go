//go:build ignore
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"nofx/auth"
)

func main() {
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	token, _ := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "x@local")
	base := "https://nofx-production-fcd1.up.railway.app"
	get := func(path string) {
		req, _ := http.NewRequest("GET", base+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, _ := http.DefaultClient.Do(req)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("\n=== %s status=%d ===\n%s\n", path, resp.StatusCode, string(body))
	}
	get("/api/exchanges")
}
