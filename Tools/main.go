package main

import (
	"html/template"
	"log"
	"net/http"

	"github.com/joho/godotenv"
	"Tools/handlers"
)

func main() {
	godotenv.Load()
	tmpl := template.Must(template.New("").ParseGlob("templates/partials/*.html"))
	tmpl = template.Must(tmpl.ParseGlob("templates/*.html"))

	h := &handlers.Handlers{Templates: tmpl}

	mux := http.NewServeMux()

	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.Handle("GET /common-assets/", http.StripPrefix("/common-assets/", http.FileServer(http.Dir("../common-assets"))))

	mux.HandleFunc("GET /{$}", h.IndexHandler)
	mux.HandleFunc("GET /echo", h.EchoHandler)
	mux.HandleFunc("GET /survey-bot", h.SurveyBotHandler)
	mux.HandleFunc("POST /api/survey-bot", h.SurveyBotAPIHandler)

	addr := "0.0.0.0:5002"
	log.Printf("Tools server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}
