package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"

	"site/auth"
)

type Handlers struct {
	Templates *template.Template
}

type surveyBotPageData struct {
	TurnstileSiteKey string
}

func (h *Handlers) SurveyBotHandler(w http.ResponseWriter, r *http.Request) {
	data := surveyBotPageData{
		TurnstileSiteKey: os.Getenv("TurnstileSiteKey"),
	}
	if err := h.Templates.ExecuteTemplate(w, "survey-bot.html", data); err != nil {
		log.Printf("template %q error: %v", "survey-bot.html", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
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

	if err := auth.VerifyTurnstile(req.TurnstileToken); err != nil {
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
