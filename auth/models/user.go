package models

import "time"

// User is created the moment someone first completes email verification —
// there's no separate signup step, and no password. Every login proves
// ownership of the email again via a fresh one-time code.
type User struct {
	ID        uint `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time

	Email string `gorm:"uniqueIndex;not null"`
}

// LoginCode is a one-time 6-digit code sent to an email address to prove
// ownership of it, for both sign-in and sign-up. Keyed by email rather than
// a user ID since the user row may not exist yet when the code is issued.
type LoginCode struct {
	ID        uint `gorm:"primarykey"`
	CreatedAt time.Time

	Email     string    `gorm:"index;not null"`
	CodeHash  string    `gorm:"not null"` // bcrypt hash — never store the plaintext code
	ExpiresAt time.Time `gorm:"not null"`
	Consumed  bool      `gorm:"not null;default:false"`
	Attempts  int       `gorm:"not null;default:0"` // wrong guesses; capped to make brute-forcing a 6-digit code impractical
}

// Session backs the signed-in cookie. TokenHash (not the raw token) is
// stored so a DB read alone can't be used to impersonate a session — the
// raw token only ever lives in the browser's cookie.
type Session struct {
	TokenHash string `gorm:"primarykey;size:64"`
	UserID    uint   `gorm:"index;not null"`
	CreatedAt time.Time
	ExpiresAt time.Time `gorm:"index;not null"`
}
