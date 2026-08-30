package identity

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/modules/rbac"
)

// TestVerifyExtraRolesExpiryAndStatusLive is a throwaway, manual verification of Phase 3
// (additive multi-role, expires_at enforcement, user deactivation) against a real local
// Postgres sandbox. See rbac.TestVerifyCustomizeRoleLive for why this isn't a permanent
// sqlite-backed unit test (no such harness exists for this service).
func TestVerifyExtraRolesExpiryAndStatusLive(t *testing.T) {
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
	logger := zap.NewNop()
	repo := rbac.NewEntRepository(client)
	rbacSvc := rbac.NewService(repo, logger)
	if err := rbacSvc.SeedRoles(ctx); err != nil {
		t.Fatalf("seed roles: %v", err)
	}
	idSvc := NewService(client, nil)
	rbacSvc.SetUserResolver(idSvc)

	tenantID := uuid.New()
	authUserID := uuid.New()
	actorID := uuid.New()

	if _, err := client.Tenant.Create().
		SetID(tenantID).SetName("Verify Tenant").SetSlug("verify-tenant-" + tenantID.String()[:8]).
		Save(ctx); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	user, err := client.HospitalUser.Create().
		SetTenantID(tenantID).SetAuthServiceUserID(authUserID).SetEmail("verify@example.com").
		Save(ctx)
	if err != nil {
		t.Fatalf("create hospital user: %v", err)
	}

	// Primary role + one extra role.
	if err := rbacSvc.SetUserRole(ctx, tenantID, user.ID, actorID, rbac.RoleNurse); err != nil {
		t.Fatalf("SetUserRole: %v", err)
	}
	if err := rbacSvc.AssignExtraRole(ctx, tenantID, user.ID, actorID, rbac.RolePharmacist); err != nil {
		t.Fatalf("AssignExtraRole: %v", err)
	}
	// Idempotent re-grant of the same extra role must not error.
	if err := rbacSvc.AssignExtraRole(ctx, tenantID, user.ID, actorID, rbac.RolePharmacist); err != nil {
		t.Fatalf("AssignExtraRole (idempotent re-grant): %v", err)
	}
	roles, err := rbacSvc.GetUserRoles(ctx, tenantID, user.ID)
	if err != nil {
		t.Fatalf("GetUserRoles: %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("GetUserRoles after primary+extra = %d roles, want 2 (%+v)", len(roles), roles)
	}

	// Revoking the extra role leaves the primary one intact.
	if err := rbacSvc.RevokeExtraRole(ctx, tenantID, user.ID, actorID, rbac.RolePharmacist); err != nil {
		t.Fatalf("RevokeExtraRole: %v", err)
	}
	roles, err = rbacSvc.GetUserRoles(ctx, tenantID, user.ID)
	if err != nil {
		t.Fatalf("GetUserRoles after revoke: %v", err)
	}
	if len(roles) != 1 || roles[0].RoleCode != rbac.RoleNurse {
		t.Fatalf("GetUserRoles after revoking extra = %+v, want exactly [nurse]", roles)
	}

	// expires_at enforcement: grant an ALREADY-EXPIRED extra role directly (bypassing the
	// service, which doesn't take an expiry param yet) and confirm GetUserRoles excludes it.
	doctorRole, err := repo.GetRoleByCode(ctx, tenantID, rbac.RoleDoctor)
	if err != nil {
		t.Fatalf("get doctor role: %v", err)
	}
	past := time.Now().Add(-1 * time.Hour)
	if _, err := client.UserRoleAssignment.Create().
		SetTenantID(tenantID).SetUserID(user.ID).SetRoleID(doctorRole.ID).SetAssignedBy(actorID).
		SetExpiresAt(past).Save(ctx); err != nil {
		t.Fatalf("create expired assignment: %v", err)
	}
	roles, err = rbacSvc.GetUserRoles(ctx, tenantID, user.ID)
	if err != nil {
		t.Fatalf("GetUserRoles with an expired extra assignment present: %v", err)
	}
	if len(roles) != 1 || roles[0].RoleCode != rbac.RoleNurse {
		t.Fatalf("expired assignment leaked into GetUserRoles: %+v, want exactly [nurse]", roles)
	}

	// HasAnyPermissionForAuthUser resolves via the auth ID while the user is active.
	has, err := rbacSvc.HasAnyPermissionForAuthUser(ctx, tenantID, authUserID, rbac.PermTriageView)
	if err != nil {
		t.Fatalf("HasAnyPermissionForAuthUser (active): %v", err)
	}
	if !has {
		t.Fatalf("HasAnyPermissionForAuthUser (active, nurse role) = false, want true")
	}

	// Deactivation enforcement: SetUserStatus("inactive") must make every *ForAuthUser check
	// fail closed, without touching the underlying UserRoleAssignment rows.
	if _, err := idSvc.SetUserStatus(ctx, tenantID, actorID, user.ID, "inactive"); err != nil {
		t.Fatalf("SetUserStatus: %v", err)
	}
	has, err = rbacSvc.HasAnyPermissionForAuthUser(ctx, tenantID, authUserID, rbac.PermTriageView)
	if err != nil {
		t.Fatalf("HasAnyPermissionForAuthUser (inactive) returned an error, want (false, nil): %v", err)
	}
	if has {
		t.Fatalf("HasAnyPermissionForAuthUser (inactive user) = true, want false")
	}
	// The role assignment itself is untouched — reactivation must not require re-assigning roles.
	if _, err := idSvc.SetUserStatus(ctx, tenantID, actorID, user.ID, "active"); err != nil {
		t.Fatalf("SetUserStatus (reactivate): %v", err)
	}
	has, err = rbacSvc.HasAnyPermissionForAuthUser(ctx, tenantID, authUserID, rbac.PermTriageView)
	if err != nil || !has {
		t.Fatalf("HasAnyPermissionForAuthUser after reactivation = (%v, %v), want (true, nil)", has, err)
	}

	// Cleanup.
	_, _ = db.ExecContext(ctx, `DELETE FROM user_role_assignments WHERE tenant_id = $1`, tenantID)
	_, _ = db.ExecContext(ctx, `DELETE FROM hospital_users WHERE tenant_id = $1`, tenantID)
	_, _ = db.ExecContext(ctx, `DELETE FROM tenants WHERE id = $1`, tenantID)
	t.Log("verification passed and cleaned up")
}
