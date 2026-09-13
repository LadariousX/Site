package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"time"

	"site/auth"
	"site/auth/models"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	sessionCookieName = "session"
	sessionDuration   = 30 * 24 * time.Hour
	otpExpiration     = 15 * time.Minute
	otpResendCooldown = 60 * time.Second
	maxOTPAttempts    = 5
)

type loginPageData struct {
	TurnstileSiteKey string
}

func (h *Handlers) LoginPageHandler(w http.ResponseWriter, r *http.Request) {
	if err := h.Templates.ExecuteTemplate(w, "login.html", loginPageData{
		TurnstileSiteKey: os.Getenv("TurnstileSiteKey"),
	}); err != nil {
		log.Printf("template %q error: %v", "login.html", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func isValidEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}

func (h *Handlers) LoginRequestCodeHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email          string `json:"email"`
		TurnstileToken string `json:"turnstileToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := auth.VerifyTurnstile(req.TurnstileToken); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !isValidEmail(email) {
		http.Error(w, "please enter a valid email address", http.StatusBadRequest)
		return
	}

	// Cheap cooldown so a "resend" button can't be hammered to burn through
	// the Resend account's email quota.
	var recent models.LoginCode
	if err := h.DB.Where("email = ?", email).Order("created_at DESC").First(&recent).Error; err == nil {
		if wait := otpResendCooldown - time.Since(recent.CreatedAt); wait > 0 {
			http.Error(w, "please wait a moment before requesting another code", http.StatusTooManyRequests)
			return
		}
	}

	code, err := GenerateOTP()
	if err != nil {
		log.Printf("login: failed to generate code: %v", err)
		http.Error(w, "failed to generate code", http.StatusInternalServerError)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("login: failed to hash code: %v", err)
		http.Error(w, "failed to generate code", http.StatusInternalServerError)
		return
	}

	loginCode := models.LoginCode{
		Email:     email,
		CodeHash:  string(hash),
		ExpiresAt: time.Now().Add(otpExpiration),
	}
	if err := h.DB.Create(&loginCode).Error; err != nil {
		log.Printf("login: failed to store code: %v", err)
		http.Error(w, "failed to generate code", http.StatusInternalServerError)
		return
	}

	if err := SendLoginCode(email, code); err != nil {
		log.Printf("login: failed to send email: %v", err)
		http.Error(w, "failed to send verification email", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (h *Handlers) LoginVerifyCodeHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	code := strings.TrimSpace(req.Code)

	var loginCode models.LoginCode
	err := h.DB.Where("email = ? AND consumed = false AND expires_at > ?", email, time.Now()).
		Order("created_at DESC").First(&loginCode).Error
	if err != nil {
		http.Error(w, "code expired or not found; please request a new one", http.StatusBadRequest)
		return
	}

	if loginCode.Attempts >= maxOTPAttempts {
		http.Error(w, "too many incorrect attempts; please request a new code", http.StatusTooManyRequests)
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(loginCode.CodeHash), []byte(code)) != nil {
		h.DB.Model(&loginCode).Update("attempts", loginCode.Attempts+1)
		http.Error(w, "incorrect code", http.StatusUnauthorized)
		return
	}

	h.DB.Model(&loginCode).Update("consumed", true)

	var user models.User
	err = h.DB.Where("email = ?", email).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		user = models.User{Email: email}
		if err := h.DB.Create(&user).Error; err != nil {
			log.Printf("login: failed to create user: %v", err)
			http.Error(w, "failed to create account", http.StatusInternalServerError)
			return
		}
	} else if err != nil {
		log.Printf("login: failed to look up user: %v", err)
		http.Error(w, "failed to sign in", http.StatusInternalServerError)
		return
	}

	if err := h.createSession(w, user.ID); err != nil {
		log.Printf("login: failed to create session: %v", err)
		http.Error(w, "failed to sign in", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (h *Handlers) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		h.DB.Where("token_hash = ?", hashSessionToken(cookie.Value)).Delete(&models.Session{})
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func generateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hashSessionToken is stored in the DB in place of the raw token, so a
// database read alone (e.g. a backup or a bug) can't be used to forge a
// session — only the cookie in the browser holds the raw value.
func hashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (h *Handlers) createSession(w http.ResponseWriter, userID uint) error {
	token, err := generateSessionToken()
	if err != nil {
		return err
	}

	session := models.Session{
		TokenHash: hashSessionToken(token),
		UserID:    userID,
		ExpiresAt: time.Now().Add(sessionDuration),
	}
	if err := h.DB.Create(&session).Error; err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// CurrentUser resolves the signed-in user for a request, if any. Returns
// nil (no error) when there's simply no valid session — callers treat that
// as "signed out" rather than a failure. Exported as a package-level
// function (not a Handlers method) so link-manager's handlers, which have
// their own Handlers struct, can resolve the signed-in user too.
func CurrentUser(db *gorm.DB, r *http.Request) *models.User {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil
	}

	var session models.Session
	if err := db.Where("token_hash = ? AND expires_at > ?", hashSessionToken(cookie.Value), time.Now()).First(&session).Error; err != nil {
		return nil
	}

	var user models.User
	if err := db.First(&user, session.UserID).Error; err != nil {
		return nil
	}
	return &user
}

// MeHandler backs the shared header's client-side auth check: it returns
// {"email": "..."} when signed in, or an empty {} (still 200) when not —
// callers just check for a truthy email rather than branching on status.
func (h *Handlers) MeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if user := CurrentUser(h.DB, r); user != nil {
		json.NewEncoder(w).Encode(map[string]string{"email": user.Email})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{})
}
