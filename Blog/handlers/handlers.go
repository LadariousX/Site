package handlers

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"sort"
	"time"

	"site/blog/models"

	"gorm.io/gorm"
)

type Handlers struct {
	DB        *gorm.DB
	Templates *template.Template
	PostsDir  string
}

// IndexHandler serves the home page with all published posts
func (h *Handlers) IndexHandler(w http.ResponseWriter, r *http.Request) {
	var posts []models.Post
	result := h.DB.Where("status = ? AND hidden = ?", "published", false).
		Order("pinned DESC, published_at DESC").Find(&posts)
	if result.Error != nil {
		log.Printf("Error fetching posts: %v", result.Error)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Transform posts to include decoded images
	type PostView struct {
		models.Post
		Images []string
	}

	var postViews []PostView
	for _, post := range posts {
		var images []string
		if err := json.Unmarshal([]byte(post.Images), &images); err != nil {
			log.Printf("Error decoding images for post %s: %v", post.Slug, err)
		}
		postViews = append(postViews, PostView{
			Post:   post,
			Images: images,
		})
	}

	data := map[string]interface{}{
		"Posts": postViews,
	}

	if err := h.Templates.ExecuteTemplate(w, "index.html", data); err != nil {
		log.Printf("Error rendering index: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// DirHandler serves the directory page
func (h *Handlers) DirHandler(w http.ResponseWriter, r *http.Request) {
	if err := h.Templates.ExecuteTemplate(w, "directory.html", nil); err != nil {
		log.Printf("Error rendering directory: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// PostHandler serves a single post page
func (h *Handlers) PostHandler(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	var post models.Post
	result := h.DB.Where("slug = ?", slug).First(&post)
	if result.Error == gorm.ErrRecordNotFound {
		http.NotFound(w, r)
		return
	}
	if result.Error != nil {
		log.Printf("Error fetching post %s: %v", slug, result.Error)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Decode images
	var images []string
	if err := json.Unmarshal([]byte(post.Images), &images); err != nil {
		log.Printf("Error decoding images for post %s: %v", slug, err)
	}

	// Decode files
	var files []string
	if err := json.Unmarshal([]byte(post.Files), &files); err != nil {
		log.Printf("Error decoding files for post %s: %v", slug, err)
	}

	// Strip system files
	filtered := files[:0]
	for _, f := range files {
		if f != ".DS_Store" {
			filtered = append(filtered, f)
		}
	}
	files = filtered

	// Sort files: .zip files first, then others
	sort.Slice(files, func(i, j int) bool {
		iIsZip := len(files[i]) >= 4 && files[i][len(files[i])-4:] == ".zip"
		jIsZip := len(files[j]) >= 4 && files[j][len(files[j])-4:] == ".zip"
		if iIsZip && !jIsZip {
			return true
		}
		if !iIsZip && jIsZip {
			return false
		}
		return files[i] < files[j]
	})

	// Check if there are 3D models, downloadable files, and repo.zip
	has3DModel := false
	hasDownloadableFiles := false
	hasRepoZip := false
	for _, file := range files {
		if (len(file) >= 5 && file[len(file)-5:] == ".gltf") || (len(file) >= 4 && file[len(file)-4:] == ".glb") {
			has3DModel = true
		} else if len(file) >= 8 && file[len(file)-8:] == "repo.zip" {
			hasRepoZip = true
		} else {
			hasDownloadableFiles = true
		}
	}

	// Create a view struct with decoded data
	postView := struct {
		Title                string
		Slug                 string
		Excerpt              string
		PublishedAt          *time.Time
		BodyHTML             template.HTML
		Images               []string
		Files                []string
		Has3DModel           bool
		HasDownloadableFiles bool
		HasRepoZip           bool
		RepoURL              string
	}{
		Title:                post.Title,
		Slug:                 post.Slug,
		Excerpt:              post.Excerpt,
		PublishedAt:          post.PublishedAt,
		BodyHTML:             template.HTML(post.BodyHTML),
		Images:               images,
		Files:                files,
		Has3DModel:           has3DModel,
		HasDownloadableFiles: hasDownloadableFiles,
		HasRepoZip:           hasRepoZip,
		RepoURL:              post.RepoURL,
	}

	data := map[string]interface{}{
		"Post": postView,
	}

	if err := h.Templates.ExecuteTemplate(w, "post.html", data); err != nil {
		log.Printf("Error rendering post: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// AboutHandler serves the about page
func (h *Handlers) AboutHandler(w http.ResponseWriter, r *http.Request) {
	if err := h.Templates.ExecuteTemplate(w, "about.html", nil); err != nil {
		log.Printf("Error rendering about: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// GalleryHandler serves the gallery page for a post
func (h *Handlers) GalleryHandler(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	var post models.Post
	result := h.DB.Where("slug = ?", slug).First(&post)
	if result.Error == gorm.ErrRecordNotFound {
		http.NotFound(w, r)
		return
	}
	if result.Error != nil {
		log.Printf("Error fetching post %s: %v", slug, result.Error)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Decode images
	var images []string
	if err := json.Unmarshal([]byte(post.Images), &images); err != nil {
		log.Printf("Error decoding images for post %s: %v", slug, err)
	}

	// Create view struct with decoded data
	postView := struct {
		Title  string
		Slug   string
		Images []string
	}{
		Title:  post.Title,
		Slug:   post.Slug,
		Images: images,
	}

	data := map[string]interface{}{
		"Post": postView,
	}

	if err := h.Templates.ExecuteTemplate(w, "gallery.html", data); err != nil {
		log.Printf("Error rendering gallery: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

//.
