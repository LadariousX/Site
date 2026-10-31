package handlers

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"site/auth"

	"github.com/gorilla/websocket"
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

// surveyBotMessage mirrors the survey-bot backend's messages. Type is status, or a final expired / error / done
type surveyBotMessage struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// default CheckOrigin only accepts same-host origins, which is what we want for the page's own socket
var upgrader = websocket.Upgrader{}

const (
	surveyBotMaxDuration = 6 * time.Minute  // queue wait + the slowest solver
	surveyBotPingEvery   = 25 * time.Second // keeps nginx / cloudflare from closing an idle socket
)

// SurveyBotSocketHandler takes {url, email, turnstileToken} from the browser, checks turnstile, then relays the
// survey-bot backend's live status messages back until the survey finishes
func (h *Handlers) SurveyBotSocketHandler(w http.ResponseWriter, r *http.Request) {
	client, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("survey-bot websocket upgrade failed: %v", err)
		return
	}
	defer client.Close()

	var writeMu sync.Mutex
	write := func(messageType int, data []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = client.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return client.WriteMessage(messageType, data)
	}
	sendJSON := func(msg surveyBotMessage) {
		data, _ := json.Marshal(msg)
		_ = write(websocket.TextMessage, data)
	}
	defer func() {
		_ = write(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	}()

	var req struct {
		URL            string `json:"url"`
		Email          string `json:"email"`
		TurnstileToken string `json:"turnstileToken"`
	}
	client.SetReadLimit(16 << 10)
	_ = client.SetReadDeadline(time.Now().Add(30 * time.Second))
	if err := client.ReadJSON(&req); err != nil {
		sendJSON(surveyBotMessage{Type: "error", Message: "invalid request"})
		return
	}
	if err := auth.VerifyTurnstile(req.TurnstileToken); err != nil {
		sendJSON(surveyBotMessage{Type: "error", Message: err.Error()})
		return
	}

	backend, err := dialSurveyBot()
	if err != nil {
		log.Printf("survey-bot backend error: %v", err)
		sendJSON(surveyBotMessage{Type: "error", Message: "survey bot is unreachable, try again later"})
		return
	}
	defer backend.Close()
	if err := backend.WriteJSON(map[string]string{"url": req.URL, "email": req.Email}); err != nil {
		sendJSON(surveyBotMessage{Type: "error", Message: "survey bot is unreachable, try again later"})
		return
	}
	sendJSON(surveyBotMessage{Type: "status", Message: "Connected to survey bot..."})

	// browser side: keep reading so pongs and closes are handled, drop the backend if the browser leaves
	_ = client.SetReadDeadline(time.Time{})
	clientGone := make(chan struct{})
	go func() {
		defer close(clientGone)
		for {
			if _, _, err := client.ReadMessage(); err != nil {
				backend.Close()
				return
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(surveyBotPingEvery)
		defer ticker.Stop()
		for {
			select {
			case <-clientGone:
				return
			case <-ticker.C:
				if write(websocket.PingMessage, nil) != nil {
					return
				}
			}
		}
	}()

	// relay backend -> browser until a final message arrives
	_ = backend.SetReadDeadline(time.Now().Add(surveyBotMaxDuration))
	for {
		var msg surveyBotMessage
		if err := backend.ReadJSON(&msg); err != nil {
			select {
			case <-clientGone:
			default:
				log.Printf("survey-bot backend closed early: %v", err)
				sendJSON(surveyBotMessage{Type: "error", Message: "lost connection to survey bot before it finished"})
			}
			return
		}
		sendJSON(msg)
		if msg.Type != "status" {
			return
		}
	}
}

// dialSurveyBot opens a websocket to the survey-bot backend (SURVEY_BOT_BACKEND, http:// or ws:// both work)
func dialSurveyBot() (*websocket.Conn, error) {
	backend := os.Getenv("SURVEY_BOT_BACKEND")
	if backend == "" {
		backend = "http://localhost:3000"
	}
	backend = strings.TrimSuffix(backend, "/")
	backend = strings.Replace(backend, "https://", "wss://", 1)
	backend = strings.Replace(backend, "http://", "ws://", 1)

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.Dial(backend+"/ws/survey-bot", nil)
	return conn, err
}
