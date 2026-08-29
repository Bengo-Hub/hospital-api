// Command seed loads idempotent global reference data for hospital-api — currently the starter
// ICD-11 diagnosis catalogue. Safe to run on every deploy/startup (also seeded inline by
// internal/app.New, so this binary is a belt-and-suspenders deploy-pipeline step, matching
// cmd/migrate's role for schema).
package main

import (
	"context"
	"database/sql"
	"log"
	"os"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/modules/refdata"
)

func main() {
	dsn := os.Getenv("POSTGRES_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/hospital?sslmode=disable"
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer sqlDB.Close()

	drv := entsql.OpenDB(dialect.Postgres, sqlDB)
	client := ent.NewClient(ent.Driver(drv))
	defer client.Close()

	logger, _ := zap.NewProduction()
	defer logger.Sync()

	ctx := context.Background()
	if err := refdata.SeedGlobalDiagnosisCatalog(ctx, client, logger); err != nil {
		log.Fatalf("seed global diagnosis catalog: %v", err)
	}
	log.Println("hospital-api seed: complete")
}
