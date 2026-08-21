package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
)

//go:embed migrations/*.up.sql
var migrationFiles embed.FS

func ApplyMigrations(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('agent-platform-engine-migrations', 0))`); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	names, err := fs.Glob(migrationFiles, "migrations/*.up.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(names)
	for _, name := range names {
		contents, readErr := migrationFiles.ReadFile(name)
		if readErr != nil {
			return fmt.Errorf("read migration %s: %w", name, readErr)
		}
		version := filepath.Base(name)
		checksum := fmt.Sprintf("sha256:%x", sha256.Sum256(contents))
		var appliedChecksum string
		queryErr := tx.QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE version = $1`, version).Scan(&appliedChecksum)
		if queryErr == nil {
			if appliedChecksum != checksum {
				return fmt.Errorf("migration %s checksum changed", name)
			}
			continue
		}
		if !errors.Is(queryErr, sql.ErrNoRows) {
			return fmt.Errorf("read migration ledger %s: %w", name, queryErr)
		}
		if _, execErr := tx.ExecContext(ctx, string(contents)); execErr != nil {
			return fmt.Errorf("apply migration %s: %w", name, execErr)
		}
		if _, execErr := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)`, version, checksum); execErr != nil {
			return fmt.Errorf("record migration %s: %w", name, execErr)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}
