package handlers

import (
	"html/template"
	"log"
	"net/http"
)

// The PDF splitter runs entirely in the browser (pdf.js + pdf-lib), so the
// server only renders the page.
type Handlers struct {
	Templates *template.Template
}

func (h *Handlers) PDFSplitterHandler(w http.ResponseWriter, r *http.Request) {
	if err := h.Templates.ExecuteTemplate(w, "pdf-splitter.html", nil); err != nil {
		log.Printf("template %q error: %v", "pdf-splitter.html", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}
