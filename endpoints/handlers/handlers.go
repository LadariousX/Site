package handlers

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
)

// Handlers serves the small JSON endpoints mounted under /e/ and the
// /endpoints page that lists them. Each endpoint lives in its own file in this
// package.
type Handlers struct {
	Templates *template.Template
}

func (h *Handlers) EndpointsPageHandler(w http.ResponseWriter, r *http.Request) {
	if err := h.Templates.ExecuteTemplate(w, "endpoints.html", nil); err != nil {
		log.Printf("template %q error: %v", "endpoints.html", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("endpoint json encode error: %v", err)
	}
}
