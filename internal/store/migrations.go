package store

import (
	"context"
	"embed"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationFS embeds every .sql file under internal/store/migrations/ into
// the binary at compile time, so the agentdns executable ships its schema
// migrations as embedded resources. This removes the need to ship a
// separate migrations/ directory alongside the binary or to run psql by
// hand at deploy time.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// runMigrations applies any pending SQL migrations bundled with the binary,
// in lexicographic filename order, and records each successful application
// in the `schema_migrations` table so re-runs are idempotent.
//
// Each migration runs inside its own transaction — a partial failure rolls
// back that migration cleanly, and subsequent startups retry it.
//
// Migration files themselves must be written to be idempotent: they should
// use DO blocks with EXCEPTION WHEN undefined_column/undefined_table to
// no-op gracefully on fresh installs (where target tables don't exist yet)
// and on already-migrated DBs (where the old column names are gone). The
// schema_migrations table is the primary re-run guard; the DO-block safety
// is a second line of defense.
func runMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	// Ensure the tracking table exists before we read from it.
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations dir: %w", err)
	}

	var versions []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		versions = append(versions, e.Name())
	}
	sort.Strings(versions)

	if len(versions) == 0 {
		log.Printf("migrations: no embedded migration files found")
		return nil
	}

	for _, version := range versions {
		var applied bool
		if err := pool.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)",
			version,
		).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if applied {
			log.Printf("migrations: %s already applied, skipping", version)
			continue
		}

		content, err := migrationFS.ReadFile("migrations/" + version)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}

		log.Printf("migrations: applying %s", version)

		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx, string(content)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx,
			"INSERT INTO schema_migrations (version) VALUES ($1)",
			version,
		); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", version, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", version, err)
		}

		log.Printf("migrations: applied %s", version)
	}

	return nil
}
