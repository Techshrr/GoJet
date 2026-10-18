package links

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoopbackClientAddressBoundary(t *testing.T) {
	cases := []struct {
		name, peer string
		forwarded []string
		want string
		status int
	}{
		{"local-ipv4", "127.0.0.1:1234", []string{"198.51.100.4"}, "198.51.100.4:0", 204},
		{"local-ipv6", "[::1]:1234", []string{"2001:db8::4"}, "[2001:db8::4]:0", 204},
		{"mapped", "127.0.0.1:1234", []string{"::ffff:198.51.100.4"}, "198.51.100.4:0", 204},
		{"untrusted", "192.0.2.4:1234", []string{"198.51.100.4"}, "192.0.2.4:1234", 204},
		{"health", "127.0.0.1:1234", nil, "127.0.0.1:1234", 204},
		{"chain", "127.0.0.1:1234", []string{"198.51.100.4, 192.0.2.4"}, "", 400},
		{"duplicate", "127.0.0.1:1234", []string{"198.51.100.4", "192.0.2.4"}, "", 400},
		{"empty", "127.0.0.1:1234", []string{""}, "", 400},
		{"zone", "127.0.0.1:1234", []string{"fe80::1%eth0"}, "", 400},
		{"unspecified", "127.0.0.1:1234", []string{"0.0.0.0"}, "", 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/sample", nil)
			r.RemoteAddr = tc.peer
			for _, value := range tc.forwarded {
				r.Header.Add("X-Forwarded-For", value)
			}
			called := false
			h := WithLoopbackClientAddress(http.HandlerFunc(func(w http.ResponseWriter, got *http.Request) {
				called = true
				if got.RemoteAddr != tc.want {
					t.Fatalf("peer = %q, want %q", got.RemoteAddr, tc.want)
				}
				w.WriteHeader(204)
			}))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || called != (tc.status == 204) || r.RemoteAddr != tc.peer {
				t.Fatal("status, downstream invocation or original request mutated")
			}
		})
	}
}

func TestProxyIdentityReachesPasswordLimitAndABSeed(t *testing.T) {
	identities, seeds := map[string]string{}, map[string]string{}
	h := WithLoopbackClientAddress(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client := r.Header.Get("X-Forwarded-For")
		identities[client] = passwordAttemptKey(42, r.RemoteAddr)
		seeds[client] = (&RedirectHandler{}).resolveContext(r).ABSeed
	}))
	for _, client := range []string{"198.51.100.4", "198.51.100.5"} {
		r := httptest.NewRequest("GET", "/sample", nil)
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("X-Forwarded-For", client)
		h.ServeHTTP(httptest.NewRecorder(), r)
		if identities[client] != passwordAttemptKey(42, client+":9999") {
			t.Fatal("proxy identity differs from direct client password authority")
		}
	}
	if identities["198.51.100.4"] == identities["198.51.100.5"] || seeds["198.51.100.4"] == seeds["198.51.100.5"] {
		t.Fatal("distinct clients collapsed to ingress identity")
	}
}
