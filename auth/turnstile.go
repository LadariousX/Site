package auth

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
)

// VerifyTurnstile checks a Turnstile response token against Cloudflare's
// siteverify endpoint. Shared by every subproject that gates a form/API
// behind Turnstile: auth (login), link-manager (draft/create), and
// survey-bot (its API handler). Returns nil (no verification) when
// TurnstileSecret is unset, so local dev works without Cloudflare
// credentials.
func VerifyTurnstile(token string) error {
	secret := os.Getenv("TurnstileSecret")
	if secret == "" {
		return nil // dev mode: skip verification
	}
	resp, err := http.PostForm("https://challenges.cloudflare.com/turnstile/v0/siteverify",
		url.Values{"secret": {secret}, "response": {token}})
	if err != nil {
		return fmt.Errorf("turnstile request failed: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		Success    bool     `json:"success"`
		ErrorCodes []string `json:"error-codes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("turnstile response parse failed: %w", err)
	}
	if !result.Success {
		log.Printf("turnstile failed: %v", result.ErrorCodes)
		return fmt.Errorf("security check failed")
	}
	return nil
}
