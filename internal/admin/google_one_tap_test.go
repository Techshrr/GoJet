package admin

import "testing"

func TestGoogleOneTapSettingValidation(t *testing.T) {
	for _, value := range []string{"true", "false"} {
		clean, err := normalizeSettingValue("google_one_tap", map[string]string{"enabled": value})
		if err != nil || clean["enabled"] != value {
			t.Fatal("valid switch rejected")
		}
	}
	for _, value := range []map[string]string{
		nil, {"enabled": "yes"}, {"enabled": "true", "client_secret": "forbidden"},
	} {
		if _, err := normalizeSettingValue("google_one_tap", value); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}
