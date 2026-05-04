package main

import (
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"Blog/db"
	"Blog/handlers"
	"Blog/parser"
)

func main() {
	// Environment variables with defaults
	dbPath := getEnv("DB_PATH", "data/Blog.db")
	backupDir := getEnv("BACKUP_DIR", "data/backups")
	postsDir := getEnv("POSTS_DIR", "posts")
	addr := getEnv("ADDR", "0.0.0.0:8080")

	// Open database
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	// Initial sync of posts
	//log.Println("Syncing posts...")
	if err := parser.SyncDir(database, postsDir); err != nil {
		log.Printf("Warning: failed to sync posts: %v", err)
	}

	// Schedule automatic backups every 6 hours
	db.ScheduleBackups(database, dbPath, backupDir, 6*time.Hour)

	// Load templates - base template first!
	funcMap := template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"hasSuffix": func(s, suffix string) bool {
			return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
		},
		"basename": func(path string) string {
			return filepath.Base(path)
		},
	}

	// Load common-assets/templates/base.html first
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseGlob("../common-assets/templates/*.html"))
	// Then load partials (like carousel)
	tmpl = template.Must(tmpl.ParseGlob("templates/partials/*.html"))
	// Finally load page templates (index, post, gallery)
	tmpl = template.Must(tmpl.ParseGlob("templates/*.html"))

	// Initialize handlers
	h := &handlers.Handlers{
		DB:        database,
		Templates: tmpl,
		PostsDir:  postsDir,
		DBPath:    dbPath,
		BackupDir: backupDir,
	}

	// Routes
	mux := http.NewServeMux()

	// Static file servers (must be defined before dynamic routes to avoid conflicts)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.Handle("GET /post-images/", http.StripPrefix("/post-images/", http.FileServer(http.Dir(postsDir))))
	mux.Handle("GET /post-files/", http.StripPrefix("/post-files/", http.FileServer(http.Dir(postsDir))))
	mux.Handle("GET /common-assets/", http.StripPrefix("/common-assets/", http.FileServer(http.Dir("../common-assets"))))
	mux.Handle("GET /Blog/static/", http.StripPrefix("/Blog/static/", http.FileServer(http.Dir("static"))))

	// Page routes
	mux.HandleFunc("GET /{$}", h.IndexHandler)
	mux.HandleFunc("GET /about", h.AboutHandler)
	mux.HandleFunc("GET /posts/{slug}", h.PostHandler)
	mux.HandleFunc("GET /posts/{slug}/gallery", h.GalleryHandler)

	// Start server
	log.Printf("Server listening on %s", addr)
	//log.Printf("Posts directory: %s", postsDir)
	//log.Printf("Database: %s", dbPath)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
