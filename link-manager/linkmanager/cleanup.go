package linkmanager

import (
	"log"
	"time"

	"site/link-manager/models"

	"gorm.io/gorm"
)

// DeleteLink removes a link's database row (cascading to its LinkFile
// children) and, for file-mode links, its on-disk upload folder. Shared by
// the expiry sweep below and the account page's explicit delete action.
func DeleteLink(db *gorm.DB, link models.Link) error {
	if link.Type == models.LinkTypeFile {
		if err := RemoveLinkDir(link.ID); err != nil {
			log.Printf("link-manager: failed to remove files for link %d (alias %q): %v", link.ID, link.AliasString(), err)
		}
	}
	return db.Unscoped().Delete(&models.Link{}, link.ID).Error
}

// CleanupExpired removes every expired link via DeleteLink.
//
// This is a housekeeping pass only, not the authoritative expiration gate —
// the access handler independently checks ExpiresAt on every request, since
// a link can be expired but not yet swept by this ticker.
func CleanupExpired(db *gorm.DB) error {
	var expired []models.Link
	if err := db.Where("expires_at < ?", time.Now()).Find(&expired).Error; err != nil {
		return err
	}

	for _, link := range expired {
		if err := DeleteLink(db, link); err != nil {
			log.Printf("link-manager cleanup: failed to delete link %d (alias %q): %v", link.ID, link.AliasString(), err)
		}
	}

	return nil
}

// ScheduleCleanup runs CleanupExpired on a repeating interval in the background.
func ScheduleCleanup(db *gorm.DB, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := CleanupExpired(db); err != nil {
				log.Printf("link-manager cleanup failed: %v", err)
			}
		}
	}()
	log.Printf("link-manager: scheduled expired-link cleanup every %v", interval)
}
