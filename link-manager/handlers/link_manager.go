package handlers

import (
	"archive/tar"
	"archive/zip"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"site/auth"
	authhandlers "site/auth/handlers"
	authmodels "site/auth/models"
	"site/link-manager/linkmanager"
	"site/link-manager/models"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// draftExpiration is the fixed expiration a file-mode draft gets the moment
// its first file is dropped, before the user has chosen real settings.
const draftExpiration = 7 * 24 * time.Hour

// Every signed-in user's file links share one account-wide storage
// allowance, checked atomically after each individual file upload
// completes. The site owner gets a much larger allowance than everyone
// else.
const (
	defaultUserStorageBytes = 1 * 1024 * 1024 * 1024   // 1 GiB
	ownerStorageBytes       = 100 * 1024 * 1024 * 1024 // 100 GiB
	ownerEmail              = "laydenlblackwell@gmail.com"
)

// storageLimitBytes returns the total file-link storage a user is allowed.
func storageLimitBytes(user authmodels.User) int64 {
	if user.Email == ownerEmail {
		return ownerStorageBytes
	}
	return defaultUserStorageBytes
}

func formatGB(bytes int64) string {
	return fmt.Sprintf("%d GB", bytes/(1024*1024*1024))
}

type linkManagerPageData struct {
	TurnstileSiteKey string
	EditMode         bool
	SignedIn         bool
	LinkID           uint
	Alias            string
	Type             string
	Destination      string
	ExpiresAtUnix    int64
	HasPassword      bool
	FilesJSON        template.JS
	LinkHostDisplay  string
}

type linkLockedPageData struct {
	Alias string
	Error string
}

type linkFilesPageData struct {
	Alias           string
	Files           []models.LinkFile
	HasPassword     bool
	ShortLinkHost   string
	LinkHostDisplay string
}

type linkFileJSON struct {
	FileName  string `json:"fileName"`
	SizeBytes int64  `json:"sizeBytes"`
}

func filesToJSON(files []models.LinkFile) template.JS {
	items := make([]linkFileJSON, len(files))
	for i, f := range files {
		items[i] = linkFileJSON{FileName: f.FileName, SizeBytes: f.SizeBytes}
	}
	b, err := json.Marshal(items)
	if err != nil {
		return template.JS("[]")
	}
	return template.JS(b)
}

func (h *Handlers) renderLinkManager(w http.ResponseWriter, data linkManagerPageData) {
	data.LinkHostDisplay = linkmanager.ShortLinkHostDisplay()
	if err := h.Templates.ExecuteTemplate(w, "manager.html", data); err != nil {
		log.Printf("template %q error: %v", "manager.html", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (h *Handlers) LinkManagerHandler(w http.ResponseWriter, r *http.Request) {
	h.renderLinkManager(w, linkManagerPageData{
		TurnstileSiteKey: os.Getenv("TurnstileSiteKey"),
		SignedIn:         authhandlers.CurrentUser(h.DB, r) != nil,
		FilesJSON:        template.JS("[]"),
	})
}

type linkSummaryPageData struct {
	Alias    string
	ShortURL string
}

// LinkManagerSummaryHandler shows the "your link is ready" page after a
// successful create or update. It doesn't hit the DB — it just formats
// whatever alias it's given, so an old/bookmarked summary link degrades to
// showing a (possibly stale) short link rather than erroring.
func (h *Handlers) LinkManagerSummaryHandler(w http.ResponseWriter, r *http.Request) {
	alias := strings.TrimSpace(r.URL.Query().Get("alias"))
	if alias == "" || !linkmanager.ValidAlias(alias) {
		http.Redirect(w, r, "/link-manager", http.StatusFound)
		return
	}

	if err := h.Templates.ExecuteTemplate(w, "summary.html", linkSummaryPageData{
		Alias:    alias,
		ShortURL: linkmanager.ShortLinkHost + alias,
	}); err != nil {
		log.Printf("template %q error: %v", "summary.html", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (h *Handlers) LinkManagerCheckAliasHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Alias     string `json:"alias"`
		ExcludeID uint   `json:"excludeId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	resp := map[string]any{"available": false}
	if linkmanager.ValidAlias(req.Alias) {
		query := h.DB.Where("alias = ?", req.Alias)
		if req.ExcludeID != 0 {
			query = query.Where("id != ?", req.ExcludeID)
		}
		var existing models.Link
		err := query.First(&existing).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			resp["available"] = true
		case err == nil:
			// alias taken; nothing else to report now that editing is
			// account-ownership-based rather than edit-password-based
		default:
			log.Printf("link-manager: check-alias query failed: %v", err)
			http.Error(w, "failed to check alias", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func validAccessPassword(pw string) bool {
	return len(pw) >= 6
}

// normalizeDestination prepends "https://" when the user's URL has no
// scheme. Without this, a redirect Location header like "example.com" is
// treated by browsers as relative to the current path (/l/) and resolves to
// /l/example.com, which then 404s as an unknown alias.
func normalizeDestination(dest string) string {
	trimmed := strings.TrimSpace(dest)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return trimmed
	}
	return "https://" + trimmed
}

func expirationDuration(value int, unit string) (time.Duration, error) {
	if value <= 0 || value > 90 {
		return 0, fmt.Errorf("expiration value must be between 1 and 90")
	}
	switch unit {
	case "min":
		return time.Duration(value) * time.Minute, nil
	case "hr":
		return time.Duration(value) * time.Hour, nil
	case "days":
		return time.Duration(value) * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown expiration unit %q", unit)
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (h *Handlers) respondAliasTaken(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	json.NewEncoder(w).Encode(map[string]any{"error": "alias_taken"})
}

// LinkManagerDraftHandler creates a bare, alias-less file-mode link so the
// client can start uploading files immediately, before the user has chosen
// an alias, expiration, or password. Finalized later via
// LinkManagerCreateHandler with the returned id.
//
// File-mode links require a signed-in account (so they show up on the
// account page and can later be edited/deleted from there) — url-type
// links don't go through this draft step at all, so this is the only place
// that needs the check.
func (h *Handlers) LinkManagerDraftHandler(w http.ResponseWriter, r *http.Request) {
	user := authhandlers.CurrentUser(h.DB, r)
	if user == nil {
		http.Error(w, "please sign in to create a file link", http.StatusUnauthorized)
		return
	}

	var req struct {
		Type           string `json:"type"`
		TurnstileToken string `json:"turnstileToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := auth.VerifyTurnstile(req.TurnstileToken); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	if req.Type != models.LinkTypeFile {
		http.Error(w, `type must be "file"`, http.StatusBadRequest)
		return
	}

	link := models.Link{
		Type:      models.LinkTypeFile,
		UserID:    &user.ID,
		ExpiresAt: time.Now().Add(draftExpiration),
	}
	if err := h.DB.Create(&link).Error; err != nil {
		log.Printf("link-manager: failed to create draft: %v", err)
		http.Error(w, "failed to create draft", http.StatusInternalServerError)
		return
	}

	if err := linkmanager.CreateLinkDir(link.ID); err != nil {
		log.Printf("link-manager: failed to create upload folder: %v", err)
		http.Error(w, "failed to create upload folder", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]uint{"id": link.ID})
}

type createLinkRequest struct {
	ID              uint   `json:"id"` // present when finalizing a draft
	Alias           string `json:"alias"`
	Type            string `json:"type"`
	Destination     string `json:"destination"`
	ExpirationValue int    `json:"expirationValue"`
	ExpirationUnit  string `json:"expirationUnit"`
	Password        string `json:"password"`
	TurnstileToken  string `json:"turnstileToken"`
}

type createLinkResponse struct {
	ID        uint      `json:"id"`
	Alias     string    `json:"alias"`
	ShortURL  string    `json:"shortUrl"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (h *Handlers) LinkManagerCreateHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request", http.StatusBadRequest)
		return
	}
	var req createLinkRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := auth.VerifyTurnstile(req.TurnstileToken); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	duration, err := expirationDuration(req.ExpirationValue, req.ExpirationUnit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	expiresAt := time.Now().Add(duration)

	if req.Password != "" && !validAccessPassword(req.Password) {
		http.Error(w, "access password must be at least 6 characters", http.StatusBadRequest)
		return
	}
	var passwordHash string
	if req.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("link-manager: failed to hash password: %v", err)
			http.Error(w, "failed to hash password", http.StatusInternalServerError)
			return
		}
		passwordHash = string(hash)
	}

	user := authhandlers.CurrentUser(h.DB, r)

	alias := strings.TrimSpace(req.Alias)
	userSuppliedAlias := alias != ""
	if userSuppliedAlias && !linkmanager.ValidAlias(alias) {
		http.Error(w, "alias must be 3-64 characters: letters, numbers, hyphens, and underscores only", http.StatusBadRequest)
		return
	}

	var link models.Link
	isFinalize := req.ID != 0

	if isFinalize {
		if err := h.DB.First(&link, req.ID).Error; err != nil {
			http.Error(w, "draft not found", http.StatusNotFound)
			return
		}
		if link.Type != models.LinkTypeFile || link.HasAlias() {
			http.Error(w, "link is not a pending draft", http.StatusBadRequest)
			return
		}
		// Drafts require auth to create (see LinkManagerDraftHandler); only
		// the same signed-in user may finalize it — draft IDs are small,
		// sequential, and otherwise guessable.
		if user == nil || link.UserID == nil || *link.UserID != user.ID {
			http.Error(w, "you don't have permission to finalize this draft", http.StatusForbidden)
			return
		}
	} else {
		if req.Type != models.LinkTypeURL && req.Type != models.LinkTypeFile {
			http.Error(w, `type must be "url" or "file"`, http.StatusBadRequest)
			return
		}
		if req.Type == models.LinkTypeURL && req.Destination == "" {
			http.Error(w, "destination is required for url links", http.StatusBadRequest)
			return
		}
		link = models.Link{Type: req.Type}
		if user != nil {
			link.UserID = &user.ID
		}
	}

	if req.Destination != "" {
		link.Destination = normalizeDestination(req.Destination)
	}
	link.PasswordHash = passwordHash
	link.ExpiresAt = expiresAt

	const maxGenerateAttempts = 5
	for attempt := 0; ; attempt++ {
		if alias == "" {
			generated, err := linkmanager.GenerateAlias()
			if err != nil {
				log.Printf("link-manager: failed to generate alias: %v", err)
				http.Error(w, "failed to generate alias", http.StatusInternalServerError)
				return
			}
			alias = generated
		}
		aliasCopy := alias
		link.Alias = &aliasCopy

		var saveErr error
		if isFinalize {
			saveErr = h.DB.Save(&link).Error
		} else {
			saveErr = h.DB.Create(&link).Error
		}
		if saveErr == nil {
			break
		}

		if isUniqueViolation(saveErr) {
			if userSuppliedAlias {
				h.respondAliasTaken(w)
				return
			}
			if attempt+1 >= maxGenerateAttempts {
				http.Error(w, "failed to generate a unique alias, please try again", http.StatusInternalServerError)
				return
			}
			alias = ""
			continue
		}

		log.Printf("link-manager: failed to create link: %v", saveErr)
		http.Error(w, "failed to create link", http.StatusInternalServerError)
		return
	}

	if !isFinalize && link.Type == models.LinkTypeFile {
		if err := linkmanager.CreateLinkDir(link.ID); err != nil {
			log.Printf("link-manager: failed to create upload folder: %v", err)
			http.Error(w, "failed to create upload folder", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(createLinkResponse{
		ID:        link.ID,
		Alias:     alias,
		ShortURL:  linkmanager.ShortLinkHost + alias,
		ExpiresAt: expiresAt,
	})
}

func (h *Handlers) LinkManagerUploadHandler(w http.ResponseWriter, r *http.Request) {
	id, err := linkmanager.ParseLinkID(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid link id", http.StatusBadRequest)
		return
	}

	var link models.Link
	if err := h.DB.First(&link, id).Error; err != nil {
		http.Error(w, "link not found", http.StatusNotFound)
		return
	}
	if link.Type != models.LinkTypeFile {
		http.Error(w, "link is not a file link", http.StatusBadRequest)
		return
	}
	if time.Now().After(link.ExpiresAt) {
		http.Error(w, "this link has expired", http.StatusGone)
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "failed to parse upload", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	fileName, size, err := linkmanager.SaveUploadedFile(link.ID, header, file)
	if err != nil {
		log.Printf("link-manager: upload failed: %v", err)
		http.Error(w, "failed to save file", http.StatusInternalServerError)
		return
	}

	// The total-size check and the row insert must be atomic: drag-and-drop
	// uploads several files in parallel, so a plain "read total, then
	// insert" would let two concurrent uploads each see a total that's
	// still under the cap and both get accepted, together pushing the real
	// total over it. When the link has an owner, locking that user's row
	// for the duration of the transaction serializes concurrent uploads
	// across ALL of their file links (the limit is account-wide, not
	// per-link) — uploads by other users are unaffected. An ownerless link
	// (shouldn't normally happen; file links require sign-in to draft)
	// falls back to a per-link total under the default allowance.
	var overLimit bool
	var limitBytes int64
	txErr := h.DB.Transaction(func(tx *gorm.DB) error {
		var currentTotal int64

		if link.UserID != nil {
			var owner authmodels.User
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&owner, *link.UserID).Error; err != nil {
				return err
			}
			limitBytes = storageLimitBytes(owner)

			if err := tx.Model(&models.LinkFile{}).
				Joins("JOIN links ON links.id = link_files.link_id").
				Where("links.user_id = ?", *link.UserID).
				Select("COALESCE(SUM(link_files.size_bytes), 0)").
				Scan(&currentTotal).Error; err != nil {
				return err
			}
		} else {
			var locked models.Link
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, link.ID).Error; err != nil {
				return err
			}
			limitBytes = defaultUserStorageBytes

			if err := tx.Model(&models.LinkFile{}).
				Where("link_id = ?", link.ID).
				Select("COALESCE(SUM(size_bytes), 0)").
				Scan(&currentTotal).Error; err != nil {
				return err
			}
		}

		if currentTotal+size > limitBytes {
			overLimit = true
			return nil
		}

		return tx.Create(&models.LinkFile{LinkID: link.ID, FileName: fileName, SizeBytes: size}).Error
	})

	if txErr != nil {
		log.Printf("link-manager: failed to record uploaded file: %v", txErr)
		if err := linkmanager.DeleteFile(link.ID, fileName); err != nil {
			log.Printf("link-manager: failed to clean up file after error: %v", err)
		}
		http.Error(w, "failed to record file", http.StatusInternalServerError)
		return
	}

	if overLimit {
		if err := linkmanager.DeleteFile(link.ID, fileName); err != nil {
			log.Printf("link-manager: failed to remove oversized upload: %v", err)
		}
		http.Error(w, fmt.Sprintf("this exceeds your account's %s total storage limit", formatGB(limitBytes)), http.StatusRequestEntityTooLarge)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"fileName": fileName, "sizeBytes": size})
}

func (h *Handlers) LinkManagerDeleteFileHandler(w http.ResponseWriter, r *http.Request) {
	id, err := linkmanager.ParseLinkID(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid link id", http.StatusBadRequest)
		return
	}
	filename := r.PathValue("filename")

	var link models.Link
	if err := h.DB.First(&link, id).Error; err != nil {
		http.Error(w, "link not found", http.StatusNotFound)
		return
	}
	if link.Type != models.LinkTypeFile {
		http.Error(w, "link is not a file link", http.StatusBadRequest)
		return
	}

	if err := linkmanager.DeleteFile(link.ID, filename); err != nil && !os.IsNotExist(err) {
		log.Printf("link-manager: failed to delete file: %v", err)
		http.Error(w, "failed to delete file", http.StatusInternalServerError)
		return
	}

	if err := h.DB.Where("link_id = ? AND file_name = ?", link.ID, filename).Delete(&models.LinkFile{}).Error; err != nil {
		log.Printf("link-manager: failed to delete file record: %v", err)
		http.Error(w, "failed to delete file record", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func serveLinkFile(w http.ResponseWriter, r *http.Request, id uint, filename string) {
	f, err := linkmanager.OpenLinkFile(id, filename)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	modTime := time.Now()
	if info, err := f.Stat(); err == nil {
		modTime = info.ModTime()
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(filename)))
	http.ServeContent(w, r, filename, modTime, f)
}

func unlockCookieName(alias string) string {
	return "link_unlock_" + alias
}

func computeUnlockToken(alias, passwordHash string) string {
	secret := os.Getenv("LinkCookieSecret")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(alias + ":" + passwordHash))
	return hex.EncodeToString(mac.Sum(nil))
}

// hasValidUnlock reports whether the request has already unlocked a
// password-protected link, via either the browser's cookie (set by
// LinkUnlockHandler after the lock-page form) or HTTP Basic Auth (for
// curl/wget, which have no way to POST the lock-page form and hold a
// cookie jar across a single invocation). The Basic Auth username is
// ignored — only the password field is checked.
func hasValidUnlock(r *http.Request, alias, passwordHash string) bool {
	if cookie, err := r.Cookie(unlockCookieName(alias)); err == nil {
		if hmac.Equal([]byte(cookie.Value), []byte(computeUnlockToken(alias, passwordHash))) {
			return true
		}
	}
	if _, pass, ok := r.BasicAuth(); ok {
		if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(pass)) == nil {
			return true
		}
	}
	return false
}

func (h *Handlers) renderLinkLocked(w http.ResponseWriter, alias, errMsg string) {
	data := linkLockedPageData{Alias: alias, Error: errMsg}
	if err := h.Templates.ExecuteTemplate(w, "locked.html", data); err != nil {
		log.Printf("template %q error: %v", "locked.html", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func isCLIUserAgent(ua string) bool {
	ua = strings.ToLower(ua)
	return strings.HasPrefix(ua, "curl/") || strings.HasPrefix(ua, "wget/")
}

// respondCLIAuthRequired tells curl/wget how to authenticate with HTTP
// Basic Auth instead of the browser's cookie-based lock page.
func respondCLIAuthRequired(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="link"`)
	http.Error(w, "this link is password protected; use -u :<password> (curl) or --http-password=<password> (wget)", http.StatusUnauthorized)
}

func (h *Handlers) LinkAccessHandler(w http.ResponseWriter, r *http.Request) {
	alias := r.PathValue("alias")

	var link models.Link
	if err := h.DB.Preload("Files").Where("alias = ?", alias).First(&link).Error; err != nil {
		http.Redirect(w, r, "/link-manager?error=not_found", http.StatusFound)
		return
	}
	if time.Now().After(link.ExpiresAt) {
		http.Error(w, "this link has expired", http.StatusGone)
		return
	}

	cli := isCLIUserAgent(r.Header.Get("User-Agent"))

	if link.PasswordHash != "" && !hasValidUnlock(r, alias, link.PasswordHash) {
		if cli {
			respondCLIAuthRequired(w)
			return
		}
		h.renderLinkLocked(w, alias, "")
		return
	}

	if link.Type == models.LinkTypeURL {
		http.Redirect(w, r, link.Destination, http.StatusFound)
		return
	}

	if cli {
		h.serveFileLinkForCLI(w, r, link)
		return
	}

	data := linkFilesPageData{
		Alias:           alias,
		Files:           link.Files,
		HasPassword:     link.PasswordHash != "",
		ShortLinkHost:   linkmanager.ShortLinkHost,
		LinkHostDisplay: linkmanager.ShortLinkHostDisplay(),
	}
	if err := h.Templates.ExecuteTemplate(w, "files.html", data); err != nil {
		log.Printf("template %q error: %v", "files.html", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

// serveFileLinkForCLI always streams a tar archive, even for a
// single-file link, so the file page can document exactly one CLI command
// (`wget -qO- … | tar -xf -`) regardless of how many files the link holds.
func (h *Handlers) serveFileLinkForCLI(w http.ResponseWriter, r *http.Request, link models.Link) {
	if len(link.Files) == 0 {
		http.Error(w, "no files", http.StatusNotFound)
		return
	}
	serveFilesAsTar(w, link)
}

// serveFilesAsTar streams a link's files as an uncompressed tar archive.
// tar (not zip) so the CLI instructions on the file page can pipe the
// response straight into `tar -xf -` — a zip's central directory sits at
// the end of the stream, so it can't be extracted from a pipe.
func serveFilesAsTar(w http.ResponseWriter, link models.Link) {
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, link.AliasString()+".tar"))

	tw := tar.NewWriter(w)
	defer tw.Close()
	for _, lf := range link.Files {
		f, err := linkmanager.OpenLinkFile(link.ID, lf.FileName)
		if err != nil {
			log.Printf("link-manager: tar: skipping missing file %q for link %d: %v", lf.FileName, link.ID, err)
			continue
		}
		info, err := f.Stat()
		if err != nil {
			log.Printf("link-manager: tar: skipping unstattable file %q for link %d: %v", lf.FileName, link.ID, err)
			f.Close()
			continue
		}
		hdr := &tar.Header{
			Name:    lf.FileName,
			Mode:    0644,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			log.Printf("link-manager: tar: write header failed for link %d: %v", link.ID, err)
			f.Close()
			return
		}
		if _, err := io.Copy(tw, f); err != nil {
			log.Printf("link-manager: tar: copy failed for %q link %d: %v", lf.FileName, link.ID, err)
			f.Close()
			return
		}
		f.Close()
	}
}

// serveFilesAsZip streams a link's files as a zip archive — the format the
// browser's "Download All" button uses, since (unlike the CLI's tar/wget
// path) a browser download isn't piped and can hold the whole archive, and
// zip is what most users can open without extra tools.
func serveFilesAsZip(w http.ResponseWriter, link models.Link) {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, link.AliasString()+".zip"))

	zw := zip.NewWriter(w)
	defer zw.Close()
	for _, lf := range link.Files {
		f, err := linkmanager.OpenLinkFile(link.ID, lf.FileName)
		if err != nil {
			log.Printf("link-manager: zip: skipping missing file %q for link %d: %v", lf.FileName, link.ID, err)
			continue
		}
		zf, err := zw.Create(lf.FileName)
		if err != nil {
			log.Printf("link-manager: zip: failed to add %q for link %d: %v", lf.FileName, link.ID, err)
			f.Close()
			return
		}
		if _, err := io.Copy(zf, f); err != nil {
			log.Printf("link-manager: zip: copy failed for %q link %d: %v", lf.FileName, link.ID, err)
			f.Close()
			return
		}
		f.Close()
	}
}

// LinkDownloadZipHandler backs the "Download All" button on the browser
// file-list page, enforcing the same expiration/password gate as viewing
// the page itself.
func (h *Handlers) LinkDownloadZipHandler(w http.ResponseWriter, r *http.Request) {
	alias := r.PathValue("alias")

	var link models.Link
	if err := h.DB.Preload("Files").Where("alias = ?", alias).First(&link).Error; err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if link.Type != models.LinkTypeFile {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if time.Now().After(link.ExpiresAt) {
		http.Error(w, "this link has expired", http.StatusGone)
		return
	}
	if link.PasswordHash != "" && !hasValidUnlock(r, alias, link.PasswordHash) {
		h.renderLinkLocked(w, alias, "")
		return
	}
	if len(link.Files) == 0 {
		http.Error(w, "no files", http.StatusNotFound)
		return
	}

	serveFilesAsZip(w, link)
}

func (h *Handlers) LinkUnlockHandler(w http.ResponseWriter, r *http.Request) {
	alias := r.PathValue("alias")

	var link models.Link
	if err := h.DB.Where("alias = ?", alias).First(&link).Error; err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if time.Now().After(link.ExpiresAt) {
		http.Error(w, "this link has expired", http.StatusGone)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	password := r.FormValue("password")
	if link.PasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(link.PasswordHash), []byte(password)) != nil {
		h.renderLinkLocked(w, alias, "Incorrect password")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     unlockCookieName(alias),
		Value:    computeUnlockToken(alias, link.PasswordHash),
		Path:     "/l/" + alias,
		Expires:  link.ExpiresAt,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/l/"+alias, http.StatusSeeOther)
}

func (h *Handlers) LinkFileDownloadHandler(w http.ResponseWriter, r *http.Request) {
	alias := r.PathValue("alias")
	filename := r.PathValue("filename")

	var link models.Link
	if err := h.DB.Where("alias = ?", alias).First(&link).Error; err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if link.Type != models.LinkTypeFile {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if time.Now().After(link.ExpiresAt) {
		http.Error(w, "this link has expired", http.StatusGone)
		return
	}
	if link.PasswordHash != "" && !hasValidUnlock(r, alias, link.PasswordHash) {
		if isCLIUserAgent(r.Header.Get("User-Agent")) {
			respondCLIAuthRequired(w)
			return
		}
		h.renderLinkLocked(w, alias, "")
		return
	}

	serveLinkFile(w, r, link.ID, filename)
}

// LinkEditPageHandler serves GET /l/{alias}/edit. Edit access is purely
// account-ownership-based now: you must be signed in, and the link must
// belong to you. An anonymous (unowned) link can never be edited by anyone.
func (h *Handlers) LinkEditPageHandler(w http.ResponseWriter, r *http.Request) {
	alias := r.PathValue("alias")

	var link models.Link
	if err := h.DB.Preload("Files").Where("alias = ?", alias).First(&link).Error; err != nil {
		http.Redirect(w, r, "/link-manager?error=not_found", http.StatusFound)
		return
	}

	// An anonymous (ownerless) link can never be edited by anyone — don't
	// even bother prompting for sign-in, just send them back to create one.
	if link.UserID == nil {
		http.Redirect(w, r, "/link-manager", http.StatusFound)
		return
	}

	user := authhandlers.CurrentUser(h.DB, r)
	if user == nil {
		http.Redirect(w, r, "/login?redirect=/account", http.StatusFound)
		return
	}

	if *link.UserID != user.ID {
		http.Redirect(w, r, "/account", http.StatusFound)
		return
	}

	h.renderLinkManager(w, linkManagerPageData{
		TurnstileSiteKey: os.Getenv("TurnstileSiteKey"),
		EditMode:         true,
		SignedIn:         true,
		LinkID:           link.ID,
		Alias:            alias,
		Type:             link.Type,
		Destination:      link.Destination,
		ExpiresAtUnix:    link.ExpiresAt.Unix(),
		HasPassword:      link.PasswordHash != "",
		FilesJSON:        filesToJSON(link.Files),
	})
}

type updateLinkRequest struct {
	Alias           string `json:"alias"`
	Destination     string `json:"destination"`
	ResetExpiration bool   `json:"resetExpiration"`
	ExpirationValue int    `json:"expirationValue"`
	ExpirationUnit  string `json:"expirationUnit"`
	Password        string `json:"password"`
	RemovePassword  bool   `json:"removePassword"`
}

func (h *Handlers) LinkManagerUpdateHandler(w http.ResponseWriter, r *http.Request) {
	id, err := linkmanager.ParseLinkID(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid link id", http.StatusBadRequest)
		return
	}

	var link models.Link
	if err := h.DB.First(&link, id).Error; err != nil {
		http.Error(w, "link not found", http.StatusNotFound)
		return
	}

	user := authhandlers.CurrentUser(h.DB, r)
	if user == nil || link.UserID == nil || *link.UserID != user.ID {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "edit_auth_required"})
		return
	}

	var req updateLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	newAlias := strings.TrimSpace(req.Alias)
	if newAlias == "" {
		http.Error(w, "alias is required", http.StatusBadRequest)
		return
	}
	if !linkmanager.ValidAlias(newAlias) {
		http.Error(w, "alias must be 3-64 characters: letters, numbers, hyphens, and underscores only", http.StatusBadRequest)
		return
	}

	if link.Type == models.LinkTypeURL {
		if req.Destination == "" {
			http.Error(w, "destination is required for url links", http.StatusBadRequest)
			return
		}
		link.Destination = normalizeDestination(req.Destination)
	} else if req.Destination != "" {
		http.Error(w, "destination cannot be set on a file link", http.StatusBadRequest)
		return
	}

	if req.ResetExpiration {
		duration, err := expirationDuration(req.ExpirationValue, req.ExpirationUnit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		link.ExpiresAt = time.Now().Add(duration)
	}

	if req.RemovePassword {
		link.PasswordHash = ""
	} else if req.Password != "" {
		if !validAccessPassword(req.Password) {
			http.Error(w, "access password must be at least 6 characters", http.StatusBadRequest)
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("link-manager: failed to hash password: %v", err)
			http.Error(w, "failed to hash password", http.StatusInternalServerError)
			return
		}
		link.PasswordHash = string(hash)
	}

	aliasCopy := newAlias
	link.Alias = &aliasCopy
	if err := h.DB.Save(&link).Error; err != nil {
		if isUniqueViolation(err) {
			h.respondAliasTaken(w)
			return
		}
		log.Printf("link-manager: failed to update link: %v", err)
		http.Error(w, "failed to update link", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(createLinkResponse{
		ID:        link.ID,
		Alias:     newAlias,
		ShortURL:  linkmanager.ShortLinkHost + newAlias,
		ExpiresAt: link.ExpiresAt,
	})
}
