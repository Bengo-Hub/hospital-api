package identity

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/google/uuid"

	"github.com/bengobox/hospital-service/internal/ent"
)

// TestVerifyOutletAssignmentAndSettingsLive is a throwaway-permanent, opt-in (VERIFY_POSTGRES_URL)
// live verification of per-user outlet assignment CRUD and tenant settings — the 2026-08-30
// per-user outlet enforcement follow-up. See rbac.TestVerifyCustomizeRoleLive for why this isn't
// a sqlite-backed unit test.
func TestVerifyOutletAssignmentAndSettingsLive(t *testing.T) {
	dsn := os.Getenv("VERIFY_POSTGRES_URL")
	if dsn == "" {
		t.Skip("VERIFY_POSTGRES_URL not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	defer client.Close()

	ctx := context.Background()
	idSvc := NewService(client, nil)

	tenantID := uuid.New()
	actorID := uuid.New()
	userAuthID := uuid.New()
	outletA := uuid.New()
	outletB := uuid.New()

	if _, err := client.Tenant.Create().
		SetID(tenantID).SetName("Outlet Verify Tenant").SetSlug("outlet-verify-"+tenantID.String()[:8]).
		Save(ctx); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	user, err := client.HospitalUser.Create().
		SetTenantID(tenantID).SetAuthServiceUserID(userAuthID).SetEmail("outlet-verify@example.com").
		Save(ctx)
	if err != nil {
		t.Fatalf("create hospital user: %v", err)
	}
	for _, o := range []uuid.UUID{outletA, outletB} {
		if _, err := client.Outlet.Create().SetID(o).SetTenantID(tenantID).SetTenantSlug("outlet-verify").
			SetCode(o.String()[:8]).SetName("Outlet " + o.String()[:8]).SetStatus("active").
			Save(ctx); err != nil {
			t.Fatalf("create outlet: %v", err)
		}
	}

	// AssignUserOutlet rejects an outlet from a different tenant.
	otherTenant := uuid.New()
	if err := idSvc.AssignUserOutlet(ctx, otherTenant, actorID, user.ID, outletA, false); err == nil {
		t.Fatalf("AssignUserOutlet should reject an outlet belonging to a different tenant")
	}

	// Assign both outlets, A as home.
	if err := idSvc.AssignUserOutlet(ctx, tenantID, actorID, user.ID, outletA, true); err != nil {
		t.Fatalf("AssignUserOutlet(A): %v", err)
	}
	if err := idSvc.AssignUserOutlet(ctx, tenantID, actorID, user.ID, outletB, false); err != nil {
		t.Fatalf("AssignUserOutlet(B): %v", err)
	}
	// Idempotent re-assign.
	if err := idSvc.AssignUserOutlet(ctx, tenantID, actorID, user.ID, outletA, true); err != nil {
		t.Fatalf("AssignUserOutlet(A) idempotent re-assign: %v", err)
	}
	rows, err := idSvc.ListUserOutlets(ctx, tenantID, user.ID)
	if err != nil {
		t.Fatalf("ListUserOutlets: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("ListUserOutlets = %d rows, want 2", len(rows))
	}

	// Remove one; the other remains.
	if err := idSvc.RemoveUserOutlet(ctx, tenantID, actorID, user.ID, outletB); err != nil {
		t.Fatalf("RemoveUserOutlet: %v", err)
	}
	rows, err = idSvc.ListUserOutlets(ctx, tenantID, user.ID)
	if err != nil {
		t.Fatalf("ListUserOutlets after remove: %v", err)
	}
	if len(rows) != 1 || rows[0].OutletID != outletA {
		t.Fatalf("ListUserOutlets after remove = %+v, want exactly [outletA]", rows)
	}

	// UpdateTenantSettings: unknown key rejected, known keys merge without clobbering others.
	if _, err := idSvc.UpdateTenantSettings(ctx, tenantID, actorID, map[string]any{"not_a_real_setting": 1}); err == nil {
		t.Fatalf("UpdateTenantSettings should reject an unknown key")
	}
	updated, err := idSvc.UpdateTenantSettings(ctx, tenantID, actorID, map[string]any{"auto_logout_minutes": 15})
	if err != nil {
		t.Fatalf("UpdateTenantSettings(auto_logout_minutes): %v", err)
	}
	if v, _ := updated.Settings["auto_logout_minutes"].(float64); v != 15 {
		t.Fatalf("settings.auto_logout_minutes = %v, want 15", updated.Settings["auto_logout_minutes"])
	}
	updated, err = idSvc.UpdateTenantSettings(ctx, tenantID, actorID, map[string]any{"default_landing_view": "queue"})
	if err != nil {
		t.Fatalf("UpdateTenantSettings(default_landing_view): %v", err)
	}
	if updated.Settings["auto_logout_minutes"] == nil {
		t.Fatalf("second UpdateTenantSettings call clobbered the first key instead of merging: %+v", updated.Settings)
	}
	if v, _ := updated.Settings["default_landing_view"].(string); v != "queue" {
		t.Fatalf("settings.default_landing_view = %v, want %q", updated.Settings["default_landing_view"], "queue")
	}

	// Cleanup.
	_, _ = db.ExecContext(ctx, `DELETE FROM hospital_user_outlets WHERE tenant_id = $1`, tenantID)
	_, _ = db.ExecContext(ctx, `DELETE FROM hospital_users WHERE tenant_id = $1`, tenantID)
	_, _ = db.ExecContext(ctx, `DELETE FROM outlets WHERE tenant_id = $1`, tenantID)
	_, _ = db.ExecContext(ctx, `DELETE FROM tenants WHERE id = $1`, tenantID)
	t.Log("verification passed and cleaned up")
}
