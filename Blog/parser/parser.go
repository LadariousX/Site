package parser

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"site/blog/models"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

// Frontmatter represents the YAML frontmatter in a post
type Frontmatter struct {
	Title   string    `yaml:"title"`
	Slug    string    `yaml:"slug"`
	Date    time.Time `yaml:"date"`
	Status  string    `yaml:"status"`
	Excerpt string    `yaml:"excerpt"`
	Cover   string    `yaml:"cover"` // filename from images/ directory
	Repo    string    `yaml:"repo"`  // GitHub repo URL
	Pinned  bool      `yaml:"pinned"`
	Hidden  bool      `yaml:"hidden"`
}

// ParsePost reads a post directory and returns a Post model
func ParsePost(postDir string) (*models.Post, error) {
	// Find the single .md file in the directory
	entries, err := os.ReadDir(postDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read post directory %s: %w", postDir, err)
	}

	var mdFile string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			if mdFile != "" {
				return nil, fmt.Errorf("multiple .md files found in %s", postDir)
			}
			mdFile = entry.Name()
		}
	}

	if mdFile == "" {
		return nil, fmt.Errorf("no .md file found in %s", postDir)
	}

	mdPath := filepath.Join(postDir, mdFile)
	content, err := os.ReadFile(mdPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", mdPath, err)
	}

	// Split frontmatter and body
	parts := bytes.SplitN(content, []byte("---"), 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid frontmatter in %s: expected --- delimiters", mdPath)
	}

	var fm Frontmatter
	if err := yaml.Unmarshal(parts[1], &fm); err != nil {
		return nil, fmt.Errorf("failed to parse frontmatter in %s: %w", mdPath, err)
	}

	// Derive slug from directory name if not specified
	if fm.Slug == "" {
		fm.Slug = filepath.Base(postDir)
	}

	// Scan images directory
	imagesDir := filepath.Join(postDir, "images")
	var imagePaths []string
	if _, err := os.Stat(imagesDir); err == nil {
		imageEntries, err := os.ReadDir(imagesDir)
		if err != nil {
			return nil, fmt.Errorf("failed to read images directory %s: %w", imagesDir, err)
		}

		for _, entry := range imageEntries {
			if !entry.IsDir() {
				ext := strings.ToLower(filepath.Ext(entry.Name()))
				// Support images and videos
				if ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".gif" || ext == ".webp" ||
					ext == ".mov" || ext == ".mp4" || ext == ".webm" {
					// Store relative path from posts/ root
					relPath := filepath.Join(fm.Slug, "images", entry.Name())
					imagePaths = append(imagePaths, relPath)
				}
			}
		}
	}

	// Sort images alphabetically for consistent ordering
	sort.Strings(imagePaths)

	// Resolve cover image by matching basename (without extension) in images dir
	var coverImage string
	if fm.Cover != "" {
		if _, err := os.Stat(imagesDir); err == nil {
			imageEntries, _ := os.ReadDir(imagesDir)
			for _, entry := range imageEntries {
				if !entry.IsDir() {
					name := entry.Name()
					nameNoExt := strings.TrimSuffix(name, filepath.Ext(name))
					if nameNoExt == fm.Cover {
						coverImage = filepath.Join(fm.Slug, "images", name)
						break
					}
				}
			}
		}
		if coverImage == "" {
			coverImage = filepath.Join(fm.Slug, "images", fm.Cover)
		}
	} else if len(imagePaths) > 0 {
		coverImage = imagePaths[0]
	}

	// Scan files directory
	filesDir := filepath.Join(postDir, "files")
	var filePaths []string
	if _, err := os.Stat(filesDir); err == nil {
		fileEntries, err := os.ReadDir(filesDir)
		if err != nil {
			return nil, fmt.Errorf("failed to read files directory %s: %w", filesDir, err)
		}

		for _, entry := range fileEntries {
			if !entry.IsDir() {
				// Store relative path from posts/ root
				relPath := filepath.Join(fm.Slug, "files", entry.Name())
				filePaths = append(filePaths, relPath)
			}
		}
	}

	// Sort files alphabetically for consistent ordering
	sort.Strings(filePaths)

	// Render markdown to HTML
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			extension.Typographer,
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(), // Allow raw HTML (needed for <div class="md-inline-grid"> etc)
		),
	)

	var htmlBuf bytes.Buffer
	if err := md.Convert(parts[2], &htmlBuf); err != nil {
		return nil, fmt.Errorf("failed to render markdown in %s: %w", mdPath, err)
	}

	// Fix relative image paths in rendered HTML
	htmlStr := htmlBuf.String()
	htmlStr = strings.ReplaceAll(htmlStr, `src="images/`, `src="/post-images/`+fm.Slug+`/images/`)
	htmlStr = strings.ReplaceAll(htmlStr, `src="files/`, `src="/post-files/`+fm.Slug+`/files/`)

	// Encode images as JSON
	imagesJSON, err := json.Marshal(imagePaths)
	if err != nil {
		return nil, fmt.Errorf("failed to encode images as JSON: %w", err)
	}

	// Encode files as JSON
	filesJSON, err := json.Marshal(filePaths)
	if err != nil {
		return nil, fmt.Errorf("failed to encode files as JSON: %w", err)
	}

	// Set PublishedAt if status is published
	var publishedAt *time.Time
	if fm.Status == "published" {
		publishedAt = &fm.Date
	}

	post := &models.Post{
		Title:       fm.Title,
		Slug:        fm.Slug,
		Body:        string(parts[2]),
		BodyHTML:    htmlStr,
		Excerpt:     fm.Excerpt,
		Status:      fm.Status,
		PublishedAt: publishedAt,
		Images:      string(imagesJSON),
		CoverImage:  coverImage,
		Files:       string(filesJSON),
		RepoURL:     fm.Repo,
		Pinned:      fm.Pinned,
		Hidden:      fm.Hidden,
	}

	return post, nil
}

// ParseDir scans a directory for post subdirectories and returns all posts
func ParseDir(dir string) ([]*models.Post, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read posts directory %s: %w", dir, err)
	}

	var posts []*models.Post
	for _, entry := range entries {
		if entry.IsDir() {
			postDir := filepath.Join(dir, entry.Name())
			post, err := ParsePost(postDir)
			if err != nil {
				log.Printf("Warning: skipping %s: %v", postDir, err)
				continue
			}
			posts = append(posts, post)
		}
	}

	// Sort by date, newest first
	sort.Slice(posts, func(i, j int) bool {
		if posts[i].PublishedAt != nil && posts[j].PublishedAt != nil {
			return posts[i].PublishedAt.After(*posts[j].PublishedAt)
		}
		if posts[i].PublishedAt != nil {
			return true
		}
		if posts[j].PublishedAt != nil {
			return false
		}
		return posts[i].CreatedAt.After(posts[j].CreatedAt)
	})

	return posts, nil
}

// SyncDir parses all posts and upserts them into the database
func SyncDir(db *gorm.DB, dir string) error {
	posts, err := ParseDir(dir)
	if err != nil {
		return fmt.Errorf("failed to parse posts: %w", err)
	}

	added := 0
	updated := 0

	for _, post := range posts {
		var existing models.Post
		result := db.Where("slug = ?", post.Slug).First(&existing)

		if result.Error == gorm.ErrRecordNotFound {
			// Insert new post
			if err := db.Create(post).Error; err != nil {
				log.Printf("Failed to insert post %s: %v", post.Slug, err)
				continue
			}
			added++
		} else if result.Error == nil {
			// Update existing post
			post.ID = existing.ID
			post.CreatedAt = existing.CreatedAt
			if err := db.Save(post).Error; err != nil {
				log.Printf("Failed to update post %s: %v", post.Slug, err)
				continue
			}
			updated++
		} else {
			log.Printf("Database error for post %s: %v", post.Slug, result.Error)
		}
	}

	log.Printf("DB Sync complete: %d added, %d updated", added, updated)
	return nil
}
