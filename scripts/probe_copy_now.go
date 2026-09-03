//go:build ignore
package main
import ("fmt"; "io"; "net/http"; "os"; "nofx/auth")
func main() {
  auth.SetJWTSecret(os.Getenv("JWT_SECRET"))
  t,_:=auth.GenerateJWT("08ab3fcb-8486-45cf-bd27-0ad35443ff61","pos@local")
  id:="8cc80c16_08ab3fcb-8486-45cf-bd27-0ad35443ff61_openai_1787127468"
  base:="https://nofx-production-fcd1.up.railway.app"
  for _,p:=range []string{"/api/account?trader_id="+id,"/api/positions?trader_id="+id}{
    req,_:=http.NewRequest("GET",base+p,nil)
    req.Header.Set("Authorization","Bearer "+t)
    resp,_:=http.DefaultClient.Do(req)
    b,_:=io.ReadAll(resp.Body)
    resp.Body.Close()
    fmt.Printf("%s\n%s\n\n",p,string(b))
  }
}
