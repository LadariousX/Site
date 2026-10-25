package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"os"
	"strings"

	"site/auth"

	"github.com/resend/resend-go/v4"
)

const (
	maxContactName    = 100
	maxContactEmail   = 254
	maxContactMessage = 5000
)

// ContactHandler handles the "Reach Out" form on the about page. It verifies
// Turnstile, then forwards the message via Resend to ContactEmailTo with the
// visitor's address set as Reply-To so replies go straight back to them.
func (h *Handlers) ContactHandler(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)

	var req struct {
		Name           string `json:"name"`
		Email          string `json:"email"`
		Message        string `json:"message"`
		TurnstileToken string `json:"turnstileToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Collapse newlines in single-line fields so they can't spill into headers
	name := strings.Join(strings.Fields(req.Name), " ")
	email := strings.TrimSpace(req.Email)
	message := strings.TrimSpace(req.Message)

	if name == "" || email == "" || message == "" {
		http.Error(w, "please fill in every field", http.StatusBadRequest)
		return
	}
	if len(name) > maxContactName || len(email) > maxContactEmail || len(message) > maxContactMessage {
		http.Error(w, "one of the fields is too long", http.StatusBadRequest)
		return
	}
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email {
		http.Error(w, "please enter a valid email address", http.StatusBadRequest)
		return
	}

	if err := auth.VerifyTurnstile(req.TurnstileToken); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	if err := sendContactEmail(name, email, message); err != nil {
		log.Printf("Error sending contact email: %v", err)
		http.Error(w, "failed to send message", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Thanks! Your message has been sent."})
}

// sendContactEmail delivers a contact-form submission via Resend. The
// recipient comes from ContactEmailTo; the sender reuses LoginEmailFrom
// (defaulting to Resend's sandbox address, which only delivers to the
// Resend account's own email until a domain is verified).
func sendContactEmail(name, email, message string) error {
	apiKey := os.Getenv("resendEmailKey")
	if apiKey == "" {
		return fmt.Errorf("resendEmailKey is not set")
	}
	to := os.Getenv("ContactEmailTo")
	if to == "" {
		return fmt.Errorf("ContactEmailTo is not set")
	}
	from := os.Getenv("LoginEmailFrom")
	if from == "" {
		from = "onboarding@resend.dev"
	}

	client := resend.NewClient(apiKey)
	params := &resend.SendEmailRequest{
		From:    from,
		To:      []string{to},
		ReplyTo: email,
		Subject: fmt.Sprintf("Site contact: %s", name),
		Text:    fmt.Sprintf("From: %s <%s>\n\n%s", name, email, message),
	}

	_, err := client.Emails.Send(params)
	return err
}
