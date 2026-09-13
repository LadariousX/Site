// Package handlers implements the auth subproject: login (OTP over email),
// sessions, and the signed-in user's account page.
package handlers

import (
	"html/template"

	"gorm.io/gorm"
)

type Handlers struct {
	DB        *gorm.DB
	Templates *template.Template
}
