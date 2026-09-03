//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"nofx/auth"
)

func main() {
	baseURL := os.Getenv("NOFX_BASE_URL")
	if baseURL == "" {
		baseURL = "https://nofx-production-fcd1.up.railway.app"
	}
	auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
	token, _ := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "restart-all-copy@local")
	client := &http.Client{Timeout: 90 * time.Second}

	getRaw := func(path string) []byte {
		req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return body
	}

	post := func(path string) {
		req, _ := http.NewRequest(http.MethodPost, baseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			panic(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("%s status=%d body=%s\n", path, resp.StatusCode, string(body))
	}

	var trList []map[string]any
	raw := getRaw("/api/my-traders")
	var arr []map[string]any
	if json.Unmarshal(raw, &arr) != nil {
		var wrap map[string]any
		_ = json.Unmarshal(raw, &wrap)
		if items, ok := wrap["traders"].([]any); ok {
			for _, item := range items {
				if m, ok := item.(map[string]any); ok {
					trList = append(trList, m)
				}
			}
		}
	} else {
		trList = arr
	}

	var ids []string
	for _, tr := range trList {
		name := strings.ToLower(fmt.Sprint(tr["name"], tr["trader_name"]))
		if strings.Contains(name, "copy") {
			id := fmt.Sprint(tr["trader_id"])
			if id == "" {
				id = fmt.Sprint(tr["id"])
			}
			ids = append(ids, id)
			fmt.Printf("found copy bot: %s (%s)\n", tr["trader_name"], id)
		}
	}

	for _, id := range ids {
		post("/api/traders/" + id + "/stop")
		time.Sleep(1 * time.Second)
	}
	time.Sleep(3 * time.Second)
	for _, id := range ids {
		post("/api/traders/" + id + "/start")
		time.Sleep(2 * time.Second)
	}
}
