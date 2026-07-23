// Package database opens the Postgres connection and applies the schema.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	sqlddl "github.com/mohan/linkedin-apply-backend/sql"

	_ "github.com/lib/pq"
)

// Open connects to Postgres and verifies the connection.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetConnMaxLifetime(time.Hour)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return db, nil
}

// Migrate applies the embedded schema (idempotent via IF NOT EXISTS).
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, sqlddl.Schema); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}
