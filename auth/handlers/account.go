package handlers

import (
	"log"
	"net/http"
	"time"

	authmodels "site/auth/models"
	"site/link-manager/linkmanager"
	"site/link-manager/models"
)

type accountLinkView struct {
	ID        uint
	Alias     string
	ShortURL  string
	CreatedAt time.Time
	ExpiresAt time.Time
	Expired   bool
}

type accountPageData struct {
	Email     string
	CreatedAt time.Time
	Links     []accountLinkView
	Folders   []accountLinkView
}

func (h *Handlers) AccountPageHandler(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(h.DB, r)
	if user == nil {
		http.Redirect(w, r, "/login?redirect=/account", http.StatusFound)
		return
	}

	var links []models.Link
	if err := h.DB.Where("user_id = ?", user.ID).Order("created_at DESC").Find(&links).Error; err != nil {
		log.Printf("account: failed to load links: %v", err)
		http.Error(w, "failed to load your links", http.StatusInternalServerError)
		return
	}

	data := accountPageData{
		Email:     user.Email,
		CreatedAt: user.CreatedAt,
	}
	now := time.Now()
	for _, link := range links {
		if !link.HasAlias() {
			continue // still-unfinalized draft; nothing worth showing yet
		}
		view := accountLinkView{
			ID:        link.ID,
			Alias:     link.AliasString(),
			ShortURL:  linkmanager.ShortLinkHost + link.AliasString(),
			CreatedAt: link.CreatedAt,
			ExpiresAt: link.ExpiresAt,
			Expired:   now.After(link.ExpiresAt),
		}
		if link.Type == models.LinkTypeURL {
			data.Links = append(data.Links, view)
		} else {
			data.Folders = append(data.Folders, view)
		}
	}

	if err := h.Templates.ExecuteTemplate(w, "account.html", data); err != nil {
		log.Printf("template %q error: %v", "account.html", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (h *Handlers) AccountDeleteLinkHandler(w http.ResponseWriter, r *http.Request) {
	id, err := linkmanager.ParseLinkID(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid link id", http.StatusBadRequest)
		return
	}

	user := CurrentUser(h.DB, r)
	if user == nil {
		http.Error(w, "please sign in", http.StatusUnauthorized)
		return
	}

	var link models.Link
	if err := h.DB.First(&link, id).Error; err != nil {
		http.Error(w, "link not found", http.StatusNotFound)
		return
	}
	if link.UserID == nil || *link.UserID != user.ID {
		http.Error(w, "you don't have permission to delete this link", http.StatusForbidden)
		return
	}

	if err := linkmanager.DeleteLink(h.DB, link); err != nil {
		log.Printf("account: failed to delete link %d: %v", id, err)
		http.Error(w, "failed to delete link", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// AccountDeleteHandler permanently deletes the signed-in user's account and
// everything tied to it: every link they own (and, for file links, the
// uploaded files on disk), all their sessions, and the user row itself.
func (h *Handlers) AccountDeleteHandler(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(h.DB, r)
	if user == nil {
		http.Error(w, "please sign in", http.StatusUnauthorized)
		return
	}

	var links []models.Link
	if err := h.DB.Where("user_id = ?", user.ID).Find(&links).Error; err != nil {
		log.Printf("account: failed to load links for deletion of user %d: %v", user.ID, err)
		http.Error(w, "failed to delete account", http.StatusInternalServerError)
		return
	}
	for _, link := range links {
		if err := linkmanager.DeleteLink(h.DB, link); err != nil {
			log.Printf("account: failed to delete link %d while deleting user %d: %v", link.ID, user.ID, err)
			http.Error(w, "failed to delete account", http.StatusInternalServerError)
			return
		}
	}

	if err := h.DB.Where("user_id = ?", user.ID).Delete(&authmodels.Session{}).Error; err != nil {
		log.Printf("account: failed to delete sessions for user %d: %v", user.ID, err)
		http.Error(w, "failed to delete account", http.StatusInternalServerError)
		return
	}

	if err := h.DB.Unscoped().Delete(&authmodels.User{}, user.ID).Error; err != nil {
		log.Printf("account: failed to delete user %d: %v", user.ID, err)
		http.Error(w, "failed to delete account", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	w.WriteHeader(http.StatusNoContent)
}
