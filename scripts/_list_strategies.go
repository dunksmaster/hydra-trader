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
  t, _ := auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61", "list@local")
  req, _ := http.NewRequest("GET", "https://nofx-production-fcd1.up.railway.app/api/strategies", nil)
  req.Header.Set("Authorization", "Bearer "+t)
  resp, _ := http.DefaultClient.Do(req)
  defer resp.Body.Close()
  b, _ := io.ReadAll(resp.Body)
  var out map[string]any
  json.Unmarshal(b, &out)
  strats, _ := out["strategies"].([]any)
  for _, item := range strats {
    s, _ := item.(map[string]any)
    fmt.Printf("%v | id=%v | active=%v | default=%v | public=%v\n", s["name"], s["id"], s["is_active"], s["is_default"], s["is_public"])
  }
}
