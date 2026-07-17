package database

import (
	"log"

	"gorm.io/gorm"
)

type Seeder struct {
	db *gorm.DB
}

func NewSeeder(db *gorm.DB) *Seeder {
	return &Seeder{db: db}
}

// SeedAll is a no-op for the trimmed gateway.
// Virtual keys are created via the admin API using the master key.
func (s *Seeder) SeedAll() error {
	log.Println("Skipping database seed — create virtual keys via /api/admin/keys")
	return nil
}
