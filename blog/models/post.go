package models

import (
	"time"

	"gorm.io/gorm"
)

type Post struct {
	ID        uint `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"` // soft-delete

	Title       string `gorm:"not null"`
	Slug        string `gorm:"uniqueIndex;not null"`
	Body        string `gorm:"type:text"` // raw markdown
	BodyHTML    string `gorm:"type:text"` // rendered HTML (cached)
	Excerpt     string
	Status      string `gorm:"default:'draft'"`
	PublishedAt *time.Time
	Images      string `gorm:"type:text"` // JSON array of image paths
	CoverImage  string // first image, denormalized
	Files       string `gorm:"type:text"` // JSON array of file paths
	RepoURL     string // GitHub repo URL from frontmatter
	Pinned      bool   `gorm:"not null;default:false"` // shown first on the index, ahead of publish date
	Hidden      bool   `gorm:"not null;default:false"` // excluded from the index entirely
}
