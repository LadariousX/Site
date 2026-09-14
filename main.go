package main

import (
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"site/database"

	authhandlers "site/auth/handlers"
	blogdb "site/blog/db"
	bloghandlers "site/blog/handlers"
	"site/blog/parser"
	linkmanagerhandlers "site/link-manager/handlers"
	"site/link-manager/linkmanager"
	surveybothandlers "site/survey-bot/handlers"

	"github.com/joho/godotenv"
)

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func main() {
	godotenv.Load(".env")

	postsDir := getEnv("POSTS_DIR", "blog/posts")
	addr := getEnv("ADDR", "0.0.0.0:8080")

	// --- Blog ---
	blogDatabase, err := blogdb.Open()
	if err != nil {
		log.Fatalf("Failed to open blog database: %v", err)
	}
	if err := parser.SyncDir(blogDatabase, postsDir); err != nil {
		log.Printf("Warning: failed to sync posts: %v", err)
	}

	blogFuncMap := template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"hasSuffix": func(s, suffix string) bool {
			return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
		},
		"basename": func(path string) string {
			return filepath.Base(path)
		},
		"posterPath": func(path string) string {
			dir, file := filepath.Split(path)
			stem := strings.TrimSuffix(file, filepath.Ext(file))
			return filepath.Join(dir, "thumbs", stem+".webp")
		},
	}
	blogTmpl := template.Must(template.New("").Funcs(blogFuncMap).ParseGlob("templates/partials/blog/*.html"))
	blogTmpl = template.Must(blogTmpl.ParseGlob("templates/blog/*.html"))

	blog := &bloghandlers.Handlers{
		DB:        blogDatabase,
		Templates: blogTmpl,
		PostsDir:  postsDir,
	}

	// --- Auth, link-manager, survey-bot share one Postgres DB and one
	// template tree (the "shared"/Tools-descended head, header, footer) ---
	appDatabase, err := database.Open()
	if err != nil {
		log.Fatalf("Failed to open shared database: %v", err)
	}
	linkmanager.ScheduleCleanup(appDatabase, 1*time.Minute)

	appTmpl := template.Must(template.New("").ParseGlob("templates/partials/shared/*.html"))
	appTmpl = template.Must(appTmpl.ParseGlob("templates/auth/*.html"))
	appTmpl = template.Must(appTmpl.ParseGlob("templates/link-manager/*.html"))
	appTmpl = template.Must(appTmpl.ParseGlob("templates/survey-bot/*.html"))

	auth := &authhandlers.Handlers{DB: appDatabase, Templates: appTmpl}
	linkManager := &linkmanagerhandlers.Handlers{DB: appDatabase, Templates: appTmpl}
	surveyBot := &surveybothandlers.Handlers{Templates: appTmpl}

	// --- Routes ---
	mux := http.NewServeMux()

	// Static/content file servers (before dynamic routes to avoid conflicts)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.Handle("GET /post-images/", http.StripPrefix("/post-images/", http.FileServer(http.Dir(postsDir))))
	mux.Handle("GET /post-files/", http.StripPrefix("/post-files/", http.FileServer(http.Dir(postsDir))))

	// Blog
	mux.HandleFunc("GET /{$}", blog.IndexHandler)
	mux.HandleFunc("GET /about", blog.AboutHandler)
	mux.HandleFunc("GET /dir", blog.DirHandler)
	mux.HandleFunc("GET /posts/{slug}", blog.PostHandler)
	mux.HandleFunc("GET /posts/{slug}/gallery", blog.GalleryHandler)

	// Auth
	mux.HandleFunc("GET /login", auth.LoginPageHandler)
	mux.HandleFunc("POST /api/login/request-code", auth.LoginRequestCodeHandler)
	mux.HandleFunc("POST /api/login/verify-code", auth.LoginVerifyCodeHandler)
	mux.HandleFunc("POST /logout", auth.LogoutHandler)
	mux.HandleFunc("GET /api/me", auth.MeHandler)
	mux.HandleFunc("GET /account", auth.AccountPageHandler)
	mux.HandleFunc("DELETE /api/account", auth.AccountDeleteHandler)
	mux.HandleFunc("DELETE /api/account/links/{id}", auth.AccountDeleteLinkHandler)

	// Link manager
	mux.HandleFunc("GET /link-manager", linkManager.LinkManagerHandler)
	mux.HandleFunc("GET /link-manager/summary", linkManager.LinkManagerSummaryHandler)
	mux.HandleFunc("POST /api/link-manager/check-alias", linkManager.LinkManagerCheckAliasHandler)
	mux.HandleFunc("POST /api/link-manager/links", linkManager.LinkManagerCreateHandler)
	mux.HandleFunc("POST /api/link-manager/links/draft", linkManager.LinkManagerDraftHandler)
	mux.HandleFunc("PATCH /api/link-manager/links/{id}", linkManager.LinkManagerUpdateHandler)
	// File uploads paused — see TODO.md ("due to network limitations").
	// mux.HandleFunc("POST /api/link-manager/links/{id}/files", linkManager.LinkManagerUploadHandler)
	mux.HandleFunc("DELETE /api/link-manager/links/{id}/files/{filename}", linkManager.LinkManagerDeleteFileHandler)
	mux.HandleFunc("GET /l/{alias}", linkManager.LinkAccessHandler)
	mux.HandleFunc("POST /l/{alias}", linkManager.LinkUnlockHandler)
	mux.HandleFunc("GET /l/{alias}/edit", linkManager.LinkEditPageHandler)
	mux.HandleFunc("GET /l/{alias}/download", linkManager.LinkDownloadZipHandler)
	mux.HandleFunc("GET /l/{alias}/{filename}", linkManager.LinkFileDownloadHandler)

	// Survey bot
	mux.HandleFunc("GET /survey-bot", surveyBot.SurveyBotHandler)
	mux.HandleFunc("POST /api/survey-bot", surveyBot.SurveyBotAPIHandler)

	log.Printf("Server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
