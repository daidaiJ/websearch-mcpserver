package redact

import (
	"strings"
	"testing"
)

func TestCredentialsRedactsKeyValueForms(t *testing.T) {
	secrets := []string{
		"as_sk_5d1f0c9a7b3e4d2f8a6c1b0e9d7f3a52",
		"temp_user_123",
		"K7mQ2xRtbV9d",
	}
	in := `api_key=as_sk_5d1f0c9a7b3e4d2f8a6c1b0e9d7f3a52 username=temp_user_123 password=K7mQ2xRtbV9d client_secret=cs_9f8e7d6c5b4a access_token=at_1234567890`
	got := Credentials(in)
	for _, secret := range append(secrets, "cs_9f8e7d6c5b4a", "at_1234567890") {
		if strings.Contains(got, secret) {
			t.Fatalf("Credentials() left %q in %q", secret, got)
		}
	}
}

func TestCredentialsRedactsBareTokens(t *testing.T) {
	for _, in := range []string{
		"as_sk_5d1f0c9a7b3e4d2f8a6c1b0e9d7f3a52",
		"sk-proj-abcdefgh12345678",
		"Authorization: Bearer as_sk_5d1f0c9a7b3e4d2f8a6c1b0e9d7f3a52",
		"api_key: \"as_sk_5d1f0c9a7b3e4d2f8a6c1b0e9d7f3a52\"",
	} {
		got := Credentials(in)
		if strings.Contains(got, "as_sk_") || strings.Contains(got, "sk-proj-") {
			t.Fatalf("Credentials(%q) = %q, still contains a token", in, got)
		}
	}
}

func TestCredentialsLeavesNormalText(t *testing.T) {
	in := "the token bucket is full and the key value pair is stable"
	if got := Credentials(in); got != in {
		t.Fatalf("Credentials() changed normal text: %q -> %q", in, got)
	}
}
