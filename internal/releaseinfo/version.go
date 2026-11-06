// Package releaseinfo exposes offline release identity before service startup.
package releaseinfo

import (
	"encoding/json"
	"io"
)

var Version = "development"
var Commit = "unknown"

// Print handles only the standalone version flag. It does not inspect secrets,
// initialize clients or start workers. Normal service startup remains unchanged.
func Print(args []string, service string, out io.Writer) bool {
	if len(args) != 1 || args[0] != "--version" {
		return false
	}
	_ = json.NewEncoder(out).Encode(struct {
		Service string `json:"service"`
		Version string `json:"version"`
		Commit  string `json:"commit"`
	}{service, Version, Commit})
	return true
}

// PrintUpgradeCapabilities is an offline contract, not a database health check.
// Only operationsmonitor owns the dedicated destination-risk restore command.
func PrintUpgradeCapabilities(args []string, service string, out io.Writer) bool {
	if service != "operationsmonitor" || len(args) != 1 || args[0] != "--upgrade-capabilities" {
		return false
	}
	_ = json.NewEncoder(out).Encode(struct {
		Service               string `json:"service"`
		Version               string `json:"version"`
		Commit                string `json:"commit"`
		RiskProjectionRestore string `json:"risk_projection_restore"`
	}{service, Version, Commit, "gojet.risk-projection-restore.v1"})
	return true
}
