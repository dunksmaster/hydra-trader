//go:build ignore

package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"nofx/auth"
)

const copyTraderID = "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_openai_1787127468"

func main() {
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	token, _ := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "restart-copy@local")
	client := &http.Client{Timeout: 90 * time.Second}

	for _, step := range []struct{ method, path string }{
		{http.MethodPost, "/api/traders/" + copyTraderID + "/stop"},
		{http.MethodPost, "/api/traders/" + copyTraderID + "/start"},
	} {
		req, _ := http.NewRequest(step.method, baseURL+step.path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("%s status=%d body=%s\n", step.path, resp.StatusCode, string(b))
		time.Sleep(2 * time.Second)
	}
}
