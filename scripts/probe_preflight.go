//go:build ignore

package main

import ("encoding/json"; "fmt"; "io"; "net/http"; "os"; "nofx/auth")
func main() {
  userID := "08ab3fcb-8486-45cf-bd27-0ad35443ff61"
  traderID := "8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_claw402_1786649332"
  auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
  token, _ := auth.GenerateJWT(userID, "preflight@local")
  req, _ := http.NewRequest("GET", "https://nofx-production-fcd1.up.railway.app/api/traders/"+traderID+"/preflight", nil)
  req.Header.Set("Authorization", "Bearer "+token)
  resp, _ := http.DefaultClient.Do(req)
  defer resp.Body.Close()
  body, _ := io.ReadAll(resp.Body)
  var out map[string]any
  json.Unmarshal(body, &out)
  checks, _ := out["checks"].([]any)
  for _, c := range checks {
    m, _ := c.(map[string]any)
    if m["id"] == "exchange_account" || m["id"] == "exchange_funds" {
      fmt.Printf("%s status=%s code=%v msg=%v\n", m["id"], m["status"], m["code"], m["message"])
    }
  }
}
