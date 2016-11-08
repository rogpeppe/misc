package auth_test

import (
	"io/ioutil"
	"net/http"
	"testing"
)

func BenchmarkAuthorizationWithAuthenticationMacaroon(b *testing.B) {
	h := testHandler{}
	s := newTestServers(h, ACLMap{
		"path-/bob": {
			"PUT": {"bob"},
		},
	})
	defer s.Close()
	// Acquire the authentication macaroon first.
	client := s.idmSrv.Client("bob")
	req, err := http.NewRequest("PUT", s.svc.URL+"/bob", nil)
	if err != nil {
		b.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		b.Fatal(err)
	}
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		b.Fatal(err)
	}
	resp.Body.Close()
	if got, want := string(data), successBody("PUT", "/bob"); got != want {
		b.Fatalf("unexpected body; got %q want %q", got, want)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, err := http.NewRequest("PUT", s.svc.URL+"/bob", nil)
		if err != nil {
			b.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		resp.Body.Close()
	}
}
