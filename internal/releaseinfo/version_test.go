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
