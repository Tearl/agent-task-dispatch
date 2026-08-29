package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	persistencepostgres "github.com/example/agent-platform/engine/internal/persistence/postgres"
	_ "github.com/lib/pq"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		fail("open PostgreSQL", err)
	}
	defer db.Close()

	if err = db.PingContext(ctx); err != nil {
		fail("connect to PostgreSQL", err)
	}
	if err = persistencepostgres.ApplyMigrations(ctx, db); err != nil {
		fail("apply PostgreSQL migrations", err)
	}

	var applied int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&applied); err != nil {
		fail("read PostgreSQL migration ledger", err)
	}
	fmt.Printf("PostgreSQL migrations are current (%d applied).\n", applied)
}

func fail(action string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", action, err)
	os.Exit(1)
}
