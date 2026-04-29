// Package store wraps GORM and exposes typed access to the gap-tracker DB.
//
// Schema ownership: the raw DDL in schema.go is the source of truth, not
// GORM struct tags. See feedback_gorm_automigrate — AutoMigrate is not
// reliable against SQLite with composite primary keys + foreign keys.
// GORM is kept for the query builder and the trivial driver swap when we
// move to Postgres later.
package store

import (
	"fmt"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Store is the typed DB handle passed around to commands and services.
type Store struct {
	db *gorm.DB
}

// Open opens the SQLite DB at path and ensures the schema exists.
// Creates the file if it doesn't exist. Uses the pure-Go glebarez/sqlite
// driver so the binary stays cgo-free.
func Open(path string) (*Store, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	s := &Store{db: db}
	if err := s.EnsureSchema(); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("ensure schema: %w", err)
	}
	return s, nil
}

// DB exposes the underlying *gorm.DB for callers that need raw GORM.
// Prefer typed methods on Store where they exist.
func (s *Store) DB() *gorm.DB { return s.db }

// EnsureSchema runs the canonical DDL. Idempotent — every statement uses
// CREATE TABLE IF NOT EXISTS so it's safe to call on every Open().
// After DDL, rebuilds the FTS5 index to pick up any data written
// before the FTS table existed (or while it was out of sync).
func (s *Store) EnsureSchema() error {
	if err := s.db.Exec(schemaSQL).Error; err != nil {
		return err
	}
	return s.RebuildFTS()
}

// Close releases the underlying database connection.
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
