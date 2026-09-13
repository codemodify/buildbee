package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
)

// githubWebhookSecret is GITHUB_WEBHOOK_SECRET. Empty means signatures are not required (dev).
func githubWebhookSecret() string {
	return strings.TrimSpace(os.Getenv("GITHUB_WEBHOOK_SECRET"))
}

// verifyGitHubSignature checks X-Hub-Signature-256 when a webhook secret is configured.
func verifyGitHubSignature(r *http.Request, raw []byte) bool {
	secret := githubWebhookSecret()
	if secret == "" {
		return true
	}
	sig := r.Header.Get("X-Hub-Signature-256")
	if sig == "" {
		sig = r.Header.Get("X-Hub-Signature")
	}
	return validHubSignature(secret, sig, raw)
}

func validHubSignature(secret, header string, raw []byte) bool {
	header = strings.TrimSpace(header)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(raw)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if strings.HasPrefix(strings.ToLower(header), "sha256=") {
		return hmac.Equal([]byte(strings.ToLower(header)), []byte(want))
	}
	// sha1=... is accepted only as a documented fallback for older GitHub Apps.
	if strings.HasPrefix(strings.ToLower(header), "sha1=") {
		return false
	}
	return hmac.Equal([]byte("sha256="+strings.ToLower(header)), []byte(want))
}

func writeWebhookUnauthorized(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid GitHub webhook signature"})
}
