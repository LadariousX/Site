package linkmanager

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// UploadsDir returns the root directory under which every link's uploaded
// files are stored, one subfolder per link ID. Named distinctly from the
// "link-manager" templates/feature name to make clear this is just the raw
// file blobs, and nested under a "data" segment so it's covered by
// deploy/sync/rsync_ignore.txt's existing **/data/ rule without needing a
// new entry there (mirrors Blog's data/ bind-mount).
func UploadsDir() string {
	return getEnv("LINK_UPLOADS_DIR", "db/link-manager-filedata/data/uploads")
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// LinkDir returns the folder for a given link's uploaded files. Keyed by the
// link's numeric ID (not its alias), since a link's alias may not exist yet
// (a draft) or may change later (edit mode), while its ID never changes.
func LinkDir(id uint) string {
	return filepath.Join(UploadsDir(), strconv.FormatUint(uint64(id), 10))
}

// CreateLinkDir creates the folder for a file-mode link.
func CreateLinkDir(id uint) error {
	return os.MkdirAll(LinkDir(id), 0755)
}

// RemoveLinkDir deletes a link's entire upload folder, used by cleanup.
func RemoveLinkDir(id uint) error {
	return os.RemoveAll(LinkDir(id))
}

// SaveUploadedFile streams an uploaded multipart file into a link's folder,
// sanitizing the filename and de-duplicating collisions. Returns the final
// on-disk filename and size written.
func SaveUploadedFile(id uint, header *multipart.FileHeader, src multipart.File) (string, int64, error) {
	name := filepath.Base(header.Filename)
	if name == "" || name == "." || name == string(filepath.Separator) || strings.Contains(name, "..") {
		return "", 0, errors.New("invalid file name")
	}

	dir := LinkDir(id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", 0, fmt.Errorf("failed to create link directory: %w", err)
	}

	finalName := name
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, finalName)); os.IsNotExist(err) {
			break
		}
		finalName = fmt.Sprintf("%s-%d%s", base, i, ext)
	}

	dst, err := os.Create(filepath.Join(dir, finalName))
	if err != nil {
		return "", 0, fmt.Errorf("failed to create file: %w", err)
	}
	defer dst.Close()

	size, err := io.Copy(dst, src)
	if err != nil {
		return "", 0, fmt.Errorf("failed to write file: %w", err)
	}

	return finalName, size, nil
}

// DeleteFile removes a single previously uploaded file from a link's
// folder, rejecting any filename that doesn't resolve to a direct child of
// that folder.
func DeleteFile(id uint, filename string) error {
	safeName := filepath.Base(filename)
	if safeName != filename || safeName == "." {
		return fmt.Errorf("invalid file name")
	}
	return os.Remove(filepath.Join(LinkDir(id), safeName))
}

// OpenLinkFile safely opens a previously uploaded file for download,
// rejecting any filename that doesn't resolve to a direct child of the
// link's folder.
func OpenLinkFile(id uint, filename string) (*os.File, error) {
	safeName := filepath.Base(filename)
	if safeName != filename || safeName == "." {
		return nil, fmt.Errorf("invalid file name")
	}
	return os.Open(filepath.Join(LinkDir(id), safeName))
}
