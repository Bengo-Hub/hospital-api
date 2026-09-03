// Command seed-tenant seeds representative demo/config reference data for ONE already-provisioned
// tenant — starter billable-item catalog rows (including any added after the tenant's original
// provisioning, e.g. ADMISSION_DEPOSIT) and a small set of demo wards/beds so a sandbox/demo tenant
// has something to click through instead of empty states. Deliberately reuses the SAME idempotent
// logic the rest of the app already relies on (refdata.SeedFacilityBillableItems/
// EnsureBillableItem, the same field-setting CreateWard/CreateBed already perform) rather than raw
// SQL, so every row this creates is exactly what the real app would have created via its own
// service layer. Every step checks for an existing row first — safe to re-run, never overwrites a
// tenant's own customization.
//
// Usage:
//
//	POSTGRES_URL=... go run ./cmd/seed-tenant -tenant-slug=demo-afya-clinic
package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"os"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/outlet"
	"github.com/bengobox/hospital-service/internal/ent/tenant"
	"github.com/bengobox/hospital-service/internal/ent/ward"
	"github.com/bengobox/hospital-service/internal/modules/refdata"
)

// backfillCodes are starter catalog codes worth ensuring on an already-provisioned tenant even
// when SeedFacilityBillableItems' own "any rows already exist" guard no-ops — i.e. codes that may
// have been added to the defaults table after this tenant was first provisioned.
var backfillCodes = []string{"ADMISSION_DEPOSIT", "WARD_DAY_RATE"}

type seedWard struct {
	name     string
	wardType string
	capacity int
}

// demoWards intentionally leaves billable_item_code unset on every row — Ward.billable_item_code
// nil already falls back to the tenant's WARD_DAY_RATE code at discharge-time billing (see
// inpatient.Service's ward-charge computation), so there is nothing to backfill per-ward here.
var demoWards = []seedWard{
	{"General Ward", "general", 20},
	{"Maternity Ward", "general", 10},
	{"ICU", "icu", 4},
}

func main() {
	slug := flag.String("tenant-slug", "", "tenant slug to seed demo data for (required)")
	flag.Parse()
	if *slug == "" {
		log.Fatal("seed-tenant: -tenant-slug is required")
	}

	dsn := os.Getenv("POSTGRES_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/hospital?sslmode=disable"
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("seed-tenant: open db: %v", err)
	}
	defer sqlDB.Close()

	drv := entsql.OpenDB(dialect.Postgres, sqlDB)
	client := ent.NewClient(ent.Driver(drv))
	defer client.Close()

	logger, _ := zap.NewProduction()
	defer logger.Sync()

	ctx := context.Background()

	t, err := client.Tenant.Query().Where(tenant.Slug(*slug)).Only(ctx)
	if err != nil {
		log.Fatalf("seed-tenant: tenant %q not found: %v", *slug, err)
	}
	log.Printf("seed-tenant: tenant %q resolved to id=%s", *slug, t.ID)

	facilityType, _ := t.Metadata["facility_type"].(string)
	if facilityType == "" {
		facilityType = "facility"
		log.Printf("seed-tenant: no facility_type in tenant metadata, defaulting to %q", facilityType)
	}

	// Fresh-tenant path (no-ops if the tenant already has any catalog rows at all).
	if err := refdata.SeedFacilityBillableItems(ctx, client, t.ID, facilityType, logger); err != nil {
		log.Fatalf("seed-tenant: seed billable items: %v", err)
	}
	// Backfill path — catches codes added to the defaults table after this tenant was first
	// provisioned, which the guard above would otherwise silently skip.
	for _, code := range backfillCodes {
		item, err := refdata.EnsureBillableItem(ctx, client, t.ID, facilityType, code)
		if err != nil {
			log.Fatalf("seed-tenant: ensure billable item %s: %v", code, err)
		}
		log.Printf("seed-tenant: billable item %s OK (id=%s)", item.Code, item.ID)
	}

	out, err := client.Outlet.Query().Where(outlet.TenantID(t.ID)).First(ctx)
	if err != nil {
		log.Fatalf("seed-tenant: no outlet found for tenant %q: %v", *slug, err)
	}
	log.Printf("seed-tenant: outlet resolved to id=%s (%s)", out.ID, out.Name)

	for _, sw := range demoWards {
		existing, err := client.Ward.Query().
			Where(ward.TenantID(t.ID), ward.OutletID(out.ID), ward.Name(sw.name)).
			Only(ctx)
		if err == nil {
			log.Printf("seed-tenant: ward %q already exists (id=%s), skipping", sw.name, existing.ID)
			continue
		}
		if !ent.IsNotFound(err) {
			log.Fatalf("seed-tenant: check ward %q: %v", sw.name, err)
		}
		w, err := client.Ward.Create().
			SetTenantID(t.ID).
			SetOutletID(out.ID).
			SetName(sw.name).
			SetWardType(ward.WardType(sw.wardType)).
			SetCapacity(sw.capacity).
			Save(ctx)
		if err != nil {
			log.Fatalf("seed-tenant: create ward %q: %v", sw.name, err)
		}
		log.Printf("seed-tenant: created ward %q (id=%s)", sw.name, w.ID)

		for i := 1; i <= sw.capacity && i <= 6; i++ { // demo data only needs a handful of beds per ward, not the full capacity
			bedNumber := sw.name[:1] + "-" + itoa(i)
			if _, err := client.Bed.Create().
				SetTenantID(t.ID).
				SetWardID(w.ID).
				SetBedNumber(bedNumber).
				Save(ctx); err != nil {
				log.Fatalf("seed-tenant: create bed %q in ward %q: %v", bedNumber, sw.name, err)
			}
		}
		log.Printf("seed-tenant: created %d demo beds in ward %q", min(sw.capacity, 6), sw.name)
	}

	log.Println("seed-tenant: complete")
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
