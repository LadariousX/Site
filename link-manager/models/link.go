package models

import (
	"time"

	"gorm.io/gorm"
)

const (
	LinkTypeURL  = "url"
	LinkTypeFile = "file"
)

type Link struct {
	ID        uint `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`

	// Alias is nil until claimed. A draft file-mode link (one with files
	// uploading before the user finalizes it) starts with no alias.
	// Postgres unique indexes treat every NULL as distinct, so any number of
	// draft rows can coexist with Alias == nil without violating the unique
	// index — this must stay *string (NULL), never "" (which collides).
	Alias *string `gorm:"uniqueIndex;size:64"`

	Type         string `gorm:"not null"` // "url" | "file"
	Destination  string // target URL; empty for file mode
	PasswordHash string // bcrypt; empty = no access password

	// UserID is set only when the creator was signed in at creation time.
	// Edit access is entirely ownership-based now (no edit password): only
	// the owning user, signed in, can reach this link's edit page. An
	// anonymous (nil) link can never be edited by anyone.
	UserID *uint `gorm:"index"`

	ExpiresAt time.Time `gorm:"index;not null"`

	Files []LinkFile `gorm:"constraint:OnDelete:CASCADE"`
}

// AliasString returns "" for a not-yet-finalized draft link.
func (l Link) AliasString() string {
	if l.Alias == nil {
		return ""
	}
	return *l.Alias
}

// HasAlias reports whether the link has claimed a public alias.
func (l Link) HasAlias() bool { return l.Alias != nil }

type LinkFile struct {
	ID        uint `gorm:"primarykey"`
	CreatedAt time.Time
	LinkID    uint   `gorm:"index;not null"`
	FileName  string `gorm:"not null"`
	SizeBytes int64  `gorm:"not null"`
}
