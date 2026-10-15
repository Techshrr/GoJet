package publicstyle

import (
	"bytes"
	"os"
	"testing"
)

func TestEmbeddedTokensMatchCanonicalDesignSystem(t *testing.T) {
	canonical, err := os.ReadFile("../../frontend/packages/tokens/generated/tokens.css")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, []byte(tokens)) {
		t.Fatal("embedded public tokens drifted; copy canonical generated tokens.css")
	}
}
