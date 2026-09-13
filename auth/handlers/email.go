package handlers

import (
	"fmt"
	"os"

	"github.com/resend/resend-go/v4"
)

// SendLoginCode emails a one-time sign-in code via Resend. The API key
// comes from the root .env (resendEmailKey); the sender address is
// configurable via LoginEmailFrom, defaulting to Resend's sandbox address.
//
// Note: onboarding@resend.dev (the default) only delivers to the email
// address on the Resend account itself until a custom domain is verified —
// real signups from other addresses will need LoginEmailFrom set to a
// verified domain.
func SendLoginCode(email, code string) error {
	apiKey := os.Getenv("resendEmailKey")
	if apiKey == "" {
		return fmt.Errorf("resendEmailKey is not set")
	}

	from := os.Getenv("LoginEmailFrom")
	if from == "" {
		from = "onboarding@resend.dev"
	}

	client := resend.NewClient(apiKey)
	params := &resend.SendEmailRequest{
		From:    from,
		To:      []string{email},
		Subject: "Your sign-in code",
		Html:    fmt.Sprintf("<p>Your verification code is <strong>%s</strong>.</p><p>It expires in 15 minutes. If you didn't request this, you can ignore this email.</p>", code),
	}

	_, err := client.Emails.Send(params)
	return err
}
