package releaseinfo

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestOfflineIdentity(t *testing.T) {
	var out bytes.Buffer
	if !Print([]string{"--version"}, "platformapi", &out) {
		t.Fatal("version not handled")
	}
	var row map[string]string
	if err := json.Unmarshal(out.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if len(row) != 3 || row["service"] != "platformapi" || row["version"] != Version || row["commit"] != Commit {
		t.Fatal("wrong identity")
	}
	for _, args := range [][]string{nil, {"--version", "extra"}, {"start"}} {
		out.Reset()
		if Print(args, "platformapi", &out) || out.Len() != 0 {
			t.Fatal("startup intercepted")
		}
	}
}

func TestOfflineUpgradeCapabilities(t *testing.T) {
	var out bytes.Buffer
	if !PrintUpgradeCapabilities([]string{"--upgrade-capabilities"}, "operationsmonitor", &out) {
		t.Fatal("capabilities not handled")
	}
	var row map[string]string
	if err := json.Unmarshal(out.Bytes(), &row); err != nil || len(row) != 4 ||
		row["service"] != "operationsmonitor" || row["version"] != Version || row["commit"] != Commit ||
		row["risk_projection_restore"] != "gojet.risk-projection-restore.v1" {
		t.Fatal("invalid offline recovery contract")
	}
	for _, service := range []string{"operationsmonitor", "platformapi"} {
		for _, args := range [][]string{nil, {"--version"}, {"--upgrade-capabilities", "extra"}, {"--restore-risk-projections"}} {
			out.Reset()
			if PrintUpgradeCapabilities(args, service, &out) || out.Len() != 0 {
				t.Fatal("startup intercepted")
			}
		}
	}
	out.Reset()
	if PrintUpgradeCapabilities([]string{"--upgrade-capabilities"}, "platformapi", &out) || out.Len() != 0 {
		t.Fatal("wrong service advertised recovery")
	}
}
