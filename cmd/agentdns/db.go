package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/agentdns/agent-dns/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func cmdDBReset() {
	// Check --confirm flag
	confirmed := false
	configPath := ""
	for i := 2; i < len(os.Args); i++ {
		if os.Args[i] == "--confirm" {
			confirmed = true
		}
		if os.Args[i] == "--config" && i+1 < len(os.Args) {
			configPath = os.Args[i+1]
			i++
		}
	}

	if !confirmed {
		fmt.Println("This will DELETE ALL DATA from the database.")
		fmt.Println("Run with --confirm to proceed:")
		fmt.Println("  agentdns db reset --confirm")
		os.Exit(1)
	}

	// Load config
	if configPath == "" {
		home, _ := os.UserHomeDir()
		configPath = filepath.Join(home, ".zynd", "data", "config.toml")
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("failed to load config from %s: %v", configPath, err)
	}

	if cfg.Registry.PostgresURL == "" {
		log.Fatalf("postgres_url is required in [registry] config section")
	}

	// Connect directly (bypass store migration)
	pool, err := pgxpool.New(context.Background(), cfg.Registry.PostgresURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()

	// Tables in dependency order (children first)
	tables := []string{
		"zns_versions",
		"zns_names",
		"gossip_zns_names",
		"peer_attestations",
		"registry_identity_proofs",
		"attestations",
		"tombstones",
		"gossip_entities",
		"gossip_developers",
		"entities",
		"developers",
		"node_meta",
	}

	fmt.Println("Clearing all tables...")
	for _, table := range tables {
		tag, err := pool.Exec(context.Background(), fmt.Sprintf("DELETE FROM %s", table))
		if err != nil {
			fmt.Printf("  %-30s ERROR: %v\n", table, err)
		} else {
			fmt.Printf("  %-30s %d rows deleted\n", table, tag.RowsAffected())
		}
	}

	fmt.Println("\nDatabase reset complete.")
}
