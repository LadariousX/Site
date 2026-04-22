package db

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"Blog/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// Open opens the SQLite database at the given path with WAL mode and foreign keys enabled
func Open(dbPath string) (*gorm.DB, error) {
	// Ensure parent directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Enable foreign keys
	if _, err := sqlDB.Exec("PRAGMA foreign_keys=ON;"); err != nil {
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Auto-migrate the schema
	if err := db.AutoMigrate(&models.Post{}); err != nil {
		return nil, fmt.Errorf("failed to migrate schema: %w", err)
	}

	//log.Printf("Database opened successfully at %s", dbPath)
	return db, nil
}

// Backup creates a hot backup of the database using VACUUM INTO
func Backup(db *gorm.DB, dbPath, backupDir string) error {
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	timestamp := time.Now().Format("2006-01-02_15-04-05")
	backupPath := filepath.Join(backupDir, fmt.Sprintf("blog_%s.db", timestamp))

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	if _, err := sqlDB.Exec(fmt.Sprintf("VACUUM INTO '%s'", backupPath)); err != nil {
		return fmt.Errorf("failed to backup database: %w", err)
	}

	log.Printf("Database backed up to %s", backupPath)
	return nil
}

// ScheduleBackups runs automatic backups at the specified interval
func ScheduleBackups(db *gorm.DB, dbPath, backupDir string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := Backup(db, dbPath, backupDir); err != nil {
				log.Printf("Scheduled backup failed: %v", err)
			}
		}
	}()
	log.Printf("Scheduled backups every %v", interval)
}
