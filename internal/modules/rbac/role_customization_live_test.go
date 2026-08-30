package rbac

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
)

// TestVerifyCustomizeRoleLive is a throwaway, manual verification of the Phase 2 copy-on-write
// role customization flow against a real local Postgres sandbox (set VERIFY_POSTGRES_URL to
// run; skipped otherwise so it never runs in CI). Not a permanent part of the suite — this
// service has no sqlite/CGO test harness (a documented platform-wide gap), so this is the only
// way to exercise the real Ent/Atlas-backed repository end-to-end before shipping.
func TestVerifyCustomizeRoleLive(t *testing.T) {
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
	repo := NewEntRepository(client)
	logger := zap.NewNop()
	svc := NewService(repo, logger)

	if err := svc.SeedRoles(ctx); err != nil {
		t.Fatalf("seed roles: %v", err)
	}

	tenantID := mustParseUUID(t, "33333333-3333-3333-3333-333333333333")
	actorID := mustParseUUID(t, "44444444-4444-4444-4444-444444444444")

	global, err := repo.GetGlobalRoleByCode(ctx, RoleDoctor)
	if err != nil {
		t.Fatalf("get global doctor role: %v", err)
	}
	globalPerms, err := repo.GetRolePermissions(ctx, global.ID)
	if err != nil {
		t.Fatalf("get global doctor permissions: %v", err)
	}
	t.Logf("global doctor role %s has %d permissions", global.ID, len(globalPerms))

	// First customize: should clone.
	clone1, err := svc.CustomizeRole(ctx, tenantID, actorID, RoleDoctor)
	if err != nil {
		t.Fatalf("CustomizeRole (1st): %v", err)
	}
	if clone1.TenantID == nil || *clone1.TenantID != tenantID {
		t.Fatalf("clone1 tenant_id = %v, want %s", clone1.TenantID, tenantID)
	}
	if clone1.ClonedFromRoleID == nil || *clone1.ClonedFromRoleID != global.ID {
		t.Fatalf("clone1 cloned_from_role_id = %v, want %s", clone1.ClonedFromRoleID, global.ID)
	}
	clonePerms, err := repo.GetRolePermissions(ctx, clone1.ID)
	if err != nil {
		t.Fatalf("get clone permissions: %v", err)
	}
	if len(clonePerms) != len(globalPerms) {
		t.Fatalf("clone has %d perms, want %d (copied from global)", len(clonePerms), len(globalPerms))
	}

	// Idempotent: second call returns the SAME clone, not a duplicate.
	clone2, err := svc.CustomizeRole(ctx, tenantID, actorID, RoleDoctor)
	if err != nil {
		t.Fatalf("CustomizeRole (2nd, idempotent): %v", err)
	}
	if clone2.ID != clone1.ID {
		t.Fatalf("2nd CustomizeRole created a NEW clone %s, want the same %s", clone2.ID, clone1.ID)
	}

	// GetRoleByCode for this tenant now prefers the clone over the global row.
	resolved, err := repo.GetRoleByCode(ctx, tenantID, RoleDoctor)
	if err != nil {
		t.Fatalf("GetRoleByCode after clone: %v", err)
	}
	if resolved.ID != clone1.ID {
		t.Fatalf("GetRoleByCode resolved %s, want the tenant clone %s (not the global row)", resolved.ID, clone1.ID)
	}

	// A DIFFERENT tenant still sees the global role, untouched.
	otherTenantID := mustParseUUID(t, "55555555-5555-5555-5555-555555555555")
	resolvedOther, err := repo.GetRoleByCode(ctx, otherTenantID, RoleDoctor)
	if err != nil {
		t.Fatalf("GetRoleByCode for other tenant: %v", err)
	}
	if resolvedOther.ID != global.ID {
		t.Fatalf("other tenant resolved %s, want the untouched global role %s", resolvedOther.ID, global.ID)
	}

	// UpdateRolePermissions on the clone works; rejecting the global row directly.
	if err := svc.UpdateRolePermissions(ctx, tenantID, actorID, clone1.ID, []string{PermLabView}); err != nil {
		t.Fatalf("UpdateRolePermissions on clone: %v", err)
	}
	updatedPerms, err := repo.GetRolePermissions(ctx, clone1.ID)
	if err != nil {
		t.Fatalf("get updated clone permissions: %v", err)
	}
	if len(updatedPerms) != 1 || updatedPerms[0].PermissionCode != PermLabView {
		t.Fatalf("clone permissions after update = %+v, want exactly [%s]", updatedPerms, PermLabView)
	}
	if err := svc.UpdateRolePermissions(ctx, tenantID, actorID, global.ID, []string{PermLabView}); err == nil {
		t.Fatalf("UpdateRolePermissions on the GLOBAL role should have been rejected, was not")
	}

	// CreateCustomRole rejects a code colliding with a global role.
	if _, err := svc.CreateCustomRole(ctx, tenantID, actorID, RoleDoctor, "Fake Doctor", "", nil); err == nil {
		t.Fatalf("CreateCustomRole with a global-colliding code should have been rejected")
	}
	custom, err := svc.CreateCustomRole(ctx, tenantID, actorID, "ward_clerk", "Ward Clerk", "custom", []string{PermRecordsView})
	if err != nil {
		t.Fatalf("CreateCustomRole: %v", err)
	}
	if custom.TenantID == nil || *custom.TenantID != tenantID || custom.ClonedFromRoleID != nil {
		t.Fatalf("custom role shape wrong: %+v", custom)
	}

	// Cleanup — leave the sandbox as it was found (roles/permissions are the only rows this
	// test creates; nothing else in the DB is touched).
	_, _ = db.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id IN ($1,$2)`, clone1.ID, custom.ID)
	_, _ = db.ExecContext(ctx, `DELETE FROM hospital_roles WHERE id IN ($1,$2)`, clone1.ID, custom.ID)
	t.Log("verification passed and cleaned up")
}

func mustParseUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	u, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u
}
