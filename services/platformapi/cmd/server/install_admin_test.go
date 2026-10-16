package main

import (
	"strings"
	"testing"
)

func TestInitialAdminRequestBounds(t *testing.T) {
	valid := `{"email":"admin@example.test","display_name":"Initial admin","password":"test-password-long","correlation_id":"install-test"}`
	if _, err := readInitialAdmin(strings.NewReader(valid)); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{
		strings.Repeat("x", 4097), valid + `{}`, `{"display_name":""}`,
		`{"display_name":"root","permissions":["anything"]}`, `{broken`,
	} {
		if _, err := readInitialAdmin(strings.NewReader(input)); err == nil {
			t.Fatal("unsafe initial admin request accepted")
		}
	}
}
