package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	httpware "github.com/Bengo-Hub/httpware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
	_ "modernc.org/sqlite"

	"github.com/bengobox/hospital-service/internal/ent"
)

func newCRMLinksTestClient(t *testing.T) *ent.Client {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=foreign_keys(1)", t.Name())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// marketflow asks, before erasing a CRM contact, whether the hospital still links it. A patient
// carrying it answers yes whichever known tenant asks; no patient answers no. {tenant} may be
// the UUID or the slug, and the route needs the service key.
func TestCRMLinksAnswersPerTenant(t *testing.T) {
	client := newCRMLinksTestClient(t)
	ctx := context.Background()
	now := time.Now()
	tenantA, tenantB := uuid.New(), uuid.New()
	slugA := "clinic-" + tenantA.String()[:8]
	client.Tenant.Create().SetID(tenantA).SetName("Clinic").SetSlug(slugA).SetStatus("active").
		SetSyncStatus("synced").SetCreatedAt(now).SetUpdatedAt(now).ExecX(ctx)
	client.Tenant.Create().SetID(tenantB).SetName("Other").SetSlug("clinic-" + tenantB.String()[:8]).SetStatus("active").
		SetSyncStatus("synced").SetCreatedAt(now).SetUpdatedAt(now).ExecX(ctx)
	linked, unlinked := uuid.New(), uuid.New()
	client.Patient.Create().SetTenantID(tenantA).SetOutletID(uuid.New()).SetMrn("MRN-1").SetFullName("Patient One").
		SetCrmContactID(linked).ExecX(ctx)

	const key = "test-service-key"
	r := chi.NewRouter()
	r.With(httpware.RequireServiceKey(key)).
		Get("/api/v1/s2s/{tenant}/crm-links/{contactID}", NewCRMLinksHandler(client, zap.NewNop()).Referenced)

	ask := func(tenant, contact string, withKey bool) (int, map[string]any) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/s2s/"+tenant+"/crm-links/"+contact, nil)
		if withKey {
			req.Header.Set("X-API-Key", key)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body
	}

	if code, body := ask(tenantA.String(), linked.String(), true); code != http.StatusOK || body["referenced"] != true || len(body) != 1 {
		t.Fatalf("linked patient by tenant id: %d %v", code, body)
	}
	if code, body := ask(slugA, linked.String(), true); code != http.StatusOK || body["referenced"] != true {
		t.Fatalf("linked patient by slug: %d %v", code, body)
	}
	// Asked under another known tenant, the patient under tenant A still counts: contact ids are
	// unique across tenants, and a tenant mismatch must only ever keep the contact.
	if code, body := ask(tenantB.String(), linked.String(), true); code != http.StatusOK || body["referenced"] != true {
		t.Fatalf("a link under another tenant must count: %d %v", code, body)
	}
	if code, body := ask(tenantA.String(), unlinked.String(), true); code != http.StatusOK || body["referenced"] != false {
		t.Fatalf("unlinked contact: %d %v", code, body)
	}
	if code, _ := ask(tenantA.String(), "not-a-uuid", true); code != http.StatusBadRequest {
		t.Fatalf("bad id: %d", code)
	}
	if code, _ := ask("no-such-clinic", linked.String(), true); code != http.StatusNotFound {
		t.Fatalf("unknown slug: %d", code)
	}
	// A tenant UUID the hospital does not know (sync drift) cannot vouch for anything.
	if code, _ := ask(uuid.NewString(), linked.String(), true); code != http.StatusNotFound {
		t.Fatalf("unknown tenant id: %d", code)
	}
	if code, _ := ask(tenantA.String(), linked.String(), false); code != http.StatusUnauthorized {
		t.Fatalf("no key: %d", code)
	}
}
