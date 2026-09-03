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
	req, _ := http.NewRequest("GET", "https://nofx-production-fcd1.up.railway.app/api/my-traders", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Printf("status=%d\n", resp.StatusCode)
	var rows []map[string]any
	json.Unmarshal(body, &rows)
	if len(rows) > 0 {
		fmt.Printf("first trader keys: exchange=%v exchange_id=%v name=%v\n",
			rows[0]["exchange"], rows[0]["exchange_id"], rows[0]["trader_name"])
	}
}
