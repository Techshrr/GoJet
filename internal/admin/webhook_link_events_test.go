package admin

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestLinkWebhookPayloadContainsOnlyResourceVersion(t *testing.T) {
	body, err := linkWebhookPayload(42, 3)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]uint64
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 2 || payload["link_id"] != 42 || payload["version"] != 3 {
		t.Fatal("webhook payload exceeded the resource/version contract")
	}
	for _, pair := range [][2]uint64{{0, 1}, {1, 0}} {
		if _, err := linkWebhookPayload(pair[0], pair[1]); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid product event accepted")
		}
	}
}
