// Package handlers implements the link-manager subproject: short links and
// password-protected file shares.
package handlers

import (
	"html/template"

	"gorm.io/gorm"
)

type Handlers struct {
	DB        *gorm.DB
	Templates *template.Template
}
