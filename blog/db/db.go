package db

import (
	"fmt"
	"log"
	"os"
	"time"

	"site/blog/models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Open connects to Postgres, retrying for a while since this stack has no
// compose healthcheck/depends_on readiness gate — the db service may still
// be initializing when this process starts. It then auto-migrates the schema.
func Open() (*gorm.DB, error) {
	host := getEnv("POSTGRES_HOST", "postgres")
	port := getEnv("POSTGRES_PORT", "5432")
	user := getEnv("POSTGRES_USER", "site")
	password := os.Getenv("POSTGRES_PASSWORD")
	dbname := "blog"

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname,
	)

	const maxAttempts = 10
	const retryDelay = 2 * time.Second

	var db *gorm.DB
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
		if err == nil {
			break
		}
		log.Printf("blog db: connect attempt %d/%d failed: %v", attempt, maxAttempts, err)
		time.Sleep(retryDelay)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to open database after %d attempts: %w", maxAttempts, err)
	}

	if err := db.AutoMigrate(&models.Post{}); err != nil {
		return nil, fmt.Errorf("failed to migrate schema: %w", err)
	}

	return db, nil
}
