// Deliberately has release identity but no recovery capability. Test-only binary.
package main

import (
	"github.com/Techshrr/GoJet/internal/releaseinfo"
	"os"
)

func main() {
	if releaseinfo.Print(os.Args[1:], "operationsmonitor", os.Stdout) {
		return
	}
	os.Exit(1)
}
