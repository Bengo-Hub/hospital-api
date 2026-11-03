package identity

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	sharedevents "github.com/Bengo-Hub/shared-events"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/hospitaluser"
	enttenant "github.com/bengobox/hospital-service/internal/ent/tenant"
)

// auth-api's services list decides whether hospital keeps a member: a list without hospital
// removes the member's row in that tenant only; an event without the list removes nothing.
func TestServicesListRemovesHospitalUser(t *testing.T) {
	dsn := os.Getenv("POSTGRES_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/hospital?sslmode=disable"
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil || db.Ping() != nil {
		t.Skip("no Postgres available")
	}
	t.Cleanup(func() { _ = db.Close() })
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	ctx := context.Background()
	h := &AuthEventHandler{client: client, logger: zap.NewNop()}
	now := time.Now()
	tenants := []uuid.UUID{uuid.New(), uuid.New()}
	for _, id := range tenants {
		client.Tenant.Create().SetID(id).SetName("Clinic").SetSlug("clinic-" + id.String()[:8]).SetStatus("active").
			SetSyncStatus("synced").SetCreatedAt(now).SetUpdatedAt(now).ExecX(ctx)
	}
	authID := uuid.New()
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = client.HospitalUser.Delete().Where(hospitaluser.AuthServiceUserID(authID)).Exec(ctx)
		_, _ = client.Tenant.Delete().Where(enttenant.IDIn(tenants...)).Exec(ctx)
	})
	for _, tid := range tenants {
		client.HospitalUser.Create().SetTenantID(tid).SetAuthServiceUserID(authID).SetEmail("staff@clinic.test").
			SetStatus("active").SetSyncStatus("synced").SetCreatedAt(now).SetUpdatedAt(now).ExecX(ctx)
	}
	count := func(tid uuid.UUID) int {
		return client.HospitalUser.Query().Where(hospitaluser.AuthServiceUserID(authID), hospitaluser.TenantID(tid)).CountX(ctx)
	}
	event := func(tid uuid.UUID, payload map[string]any) *sharedevents.Event {
		payload["user_id"] = authID.String()
		return sharedevents.NewEvent("updated", "auth.user", authID, tid, payload)
	}

	if handled, err := h.revoked(ctx, event(tenants[0], map[string]any{}), authID); handled || err != nil {
		t.Fatalf("an event without services must not revoke: %v %v", handled, err)
	}
	if handled, err := h.revoked(ctx, event(tenants[0], map[string]any{"services": []any{"pos"}}), authID); !handled || err != nil {
		t.Fatalf("a list without hospital must revoke: %v %v", handled, err)
	}
	if count(tenants[0]) != 0 || count(tenants[1]) != 1 {
		t.Fatalf("revoke must remove only the event's tenant: left %d and %d", count(tenants[0]), count(tenants[1]))
	}
}
