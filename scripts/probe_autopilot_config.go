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
	userID := "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
	autopilot := "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786632502"
	secret := os.Getenv("JWT_SECRET")
	auth.SetJWTSecret(secret)
	token, _ := auth.GenerateJWT(userID, "probe@local")
	base := "https://nofx-production-fcd1.up.railway.app"

	req, _ := http.NewRequest("GET", base+"/api/traders/"+autopilot+"/config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("status=%d\n", resp.StatusCode)
	var pretty json.RawMessage
	json.Unmarshal(body, &pretty)
	out, _ := json.MarshalIndent(pretty, "", "  ")
	fmt.Println(string(out))
}
