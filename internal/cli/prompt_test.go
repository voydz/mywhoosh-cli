package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthHiddenPromptAndUnavailableTerminal(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(map[bool]string{false:"hidden",true:"unavailable"}[unavailable], func(t *testing.T) {
			input, writer, err := os.Pipe()
			if err != nil { t.Fatal(err) }
			defer input.Close()
			_, _ = writer.Write([]byte("rider@example.test\n"))
			_ = writer.Close()
			called := false
			c := fixture(t,func(w http.ResponseWriter,r *http.Request) { _, _ = w.Write([]byte(`{"Success":true,"AccessToken":"token","WhooshId":"id"}`)) })
			var out, stderr bytes.Buffer
			a := app{in:input,out:&out,stderr:&stderr,client:c,getenv:func(string)string{return ""},isTerminal:func(int)bool{return true},readPassword:func(int)([]byte,error){
				called=true
				if unavailable { return nil,errors.New("private password reader error") }
				return []byte("hidden-secret"),nil
			}}
			code := a.run(context.Background(),[]string{"--config-dir",filepath.Join(t.TempDir(),"config"),"login"})
			want:=0
			if unavailable {want=3}
			if code!=want || !called || !strings.Contains(stderr.String(),"Password:") || strings.Contains(out.String()+stderr.String(),"hidden-secret") || strings.Contains(stderr.String(),"private password reader error") {t.Fatalf("prompt code=%d called=%v out=%q err=%q",code,called,out.String(),stderr.String())}
		})
	}
}

func TestRootConfigAndAuthGroupBoundary(t *testing.T) {
	private:=savedSession(t)
	insecure:=t.TempDir()
	if err:=os.Chmod(insecure,0755);err!=nil{t.Fatal(err)}
	c:=fixture(t,func(w http.ResponseWriter,r *http.Request){t.Error("unexpected request")})
	var out,stderr bytes.Buffer
	a:=app{in:strings.NewReader(""),out:&out,stderr:&stderr,client:c,getenv:func(key string)string{if key=="MYWHOOSH_CONFIG_DIR" {return insecure};return ""}}
	if code:=a.run(context.Background(),[]string{"status"});code!=2 {t.Fatalf("insecure env config exit=%d",code)}
	out.Reset();stderr.Reset()
	if code:=a.run(context.Background(),[]string{"--config-dir",private,"auth","status","--format","json"});code!=0 || !strings.Contains(out.String(),`"session_present":true`) {t.Fatalf("root override: %d %q %q",code,out.String(),stderr.String())}
	code,_,_:=invoke(t,c,private,"","auth","activities")
	if code!=2 {t.Fatalf("auth group accepted unrelated command: exit %d",code)}
}
