// Command migrate applies versioned Atlas/ent migrations for hospital-api.
package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/migrate"
)

func main() {
	dbURL := os.Getenv("POSTGRES_MIGRATE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("POSTGRES_URL")
	}
	if dbURL == "" {
		log.Fatal("POSTGRES_URL or POSTGRES_MIGRATE_URL must be set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	// Every replica runs this binary on startup — without coordination, N pods launching at once
	// each run their own Schema.Create concurrently against the same tables. A single physical
	// connection (MaxOpenConns=1) + a session-level advisory lock ensures only ONE pod across the
	// whole cluster ever executes the migration at a time; the rest block here until it finishes,
	// then find nothing pending and return immediately. Same fleet-standard pattern as
	// pos-api/inventory-api/treasury-api (lock keys 727271001-3); hospital-api uses 727271004.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)

	const migrationLockKey = 727271004
	if _, err := db.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationLockKey); err != nil {
		log.Fatalf("acquire migration lock: %v", err)
	}
	defer func() {
		if _, err := db.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", migrationLockKey); err != nil {
			log.Printf("release migration lock: %v", err)
		}
	}()

	drv := entsql.OpenDB(dialect.Postgres, db)
	client := ent.NewClient(ent.Driver(drv))
	defer client.Close()

	// WithDropIndex/WithDropColumn must be explicit — without them, ent's live diff (schema.WithDir
	// compares the CURRENT ent/schema/*.go structs against the live DB; there is no schema-revisions
	// table) silently skips column/index/constraint removals a struct change implies.
	if err := client.Schema.Create(ctx,
		schema.WithDir(migrate.Dir),
		schema.WithDropColumn(true),
		schema.WithDropIndex(true),
	); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Println("migrations completed")
}
