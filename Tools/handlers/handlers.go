package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
)

type Handlers struct {
	Templates *template.Template
}

type PageData struct {
	Title string
}

type surveyBotPageData struct {
	Title            string
	TurnstileSiteKey string
}

func (h *Handlers) render(w http.ResponseWriter, tmpl string, title string) {
	if err := h.Templates.ExecuteTemplate(w, tmpl, PageData{Title: title}); err != nil {
		log.Printf("template %q error: %v", tmpl, err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (h *Handlers) IndexHandler(w http.ResponseWriter, r *http.Request) {
	h.render(w, "index.html", "Tools")
}

func (h *Handlers) EchoHandler(w http.ResponseWriter, r *http.Request) {
	h.render(w, "echo.html", "Echo")
}

func (h *Handlers) SurveyBotHandler(w http.ResponseWriter, r *http.Request) {
	data := surveyBotPageData{
		Title:            "Survey Bot",
		TurnstileSiteKey: os.Getenv("TurnstileSiteKey"),
	}
	if err := h.Templates.ExecuteTemplate(w, "survey-bot.html", data); err != nil {
		log.Printf("template %q error: %v", "survey-bot.html", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func verifyTurnstile(token string) error {
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

func (h *Handlers) SurveyBotAPIHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request", http.StatusBadRequest)
		return
	}

	var req struct {
		TurnstileToken string `json:"turnstileToken"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := verifyTurnstile(req.TurnstileToken); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	backend := os.Getenv("SURVEY_BOT_BACKEND")
	if backend == "" {
		backend = "http://localhost:3000"
	}
	fmt.Println("backend: ", backend)
	resp, err := http.Post(backend+"/api/survey-bot", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("survey-bot backend error: %v", err)
		http.Error(w, "backend unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
