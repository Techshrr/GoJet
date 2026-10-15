package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// T017 replays the real P20 login-bearing predecessor timeline before probing
// Text. Respect the configured P15 Redis auth-rate window instead of weakening,
// clearing, or bypassing the production limiter for the final real login.
func init() {
	raw := strings.TrimSpace(os.Getenv("GOJET_AUTH_RATE_WINDOW_SECONDS"))
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 || seconds > int((24*time.Hour)/time.Second) {
		return
	}
	wait := time.Duration(seconds+2) * time.Second
	fmt.Fprintf(os.Stderr, "T017 discovery: respecting P15 auth-rate window for %s before final real login\n", wait)
	time.Sleep(wait)
}
