package patients

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/Bengo-Hub/shared-service-client/crm"
	"github.com/google/uuid"
	"go.uber.org/zap"
	_ "modernc.org/sqlite"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/rbacauditlog"
	"github.com/bengobox/hospital-service/internal/modules/auditlog"
	"github.com/bengobox/hospital-service/internal/modules/billing"
	"github.com/bengobox/hospital-service/internal/modules/treasury"
)

// newCRMTestClient builds an in-memory, schema-migrated ent client, one database per test.
func newCRMTestClient(t *testing.T) *ent.Client {
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

type recordedRequest struct {
	Method string
	Path   string
	Body   map[string]any
}

// fakeCRM stands in for marketflow's internal contacts API and records every request body.
type fakeCRM struct {
	srv       *httptest.Server
	contactID uuid.UUID
	status    int // reply status for upserts, 200 when zero
	mu        sync.Mutex
	requests  []recordedRequest
}

func newFakeCRM(t *testing.T) *fakeCRM {
	t.Helper()
	f := &fakeCRM{contactID: uuid.New()}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.requests = append(f.requests, recordedRequest{Method: r.Method, Path: r.URL.Path, Body: body})
		status := f.status
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "phone is not valid", "code": "invalid_phone"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         f.contactID.String(),
			"first_name": body["first_name"],
			"last_name":  body["last_name"],
			"phone":      body["phone"],
		})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeCRM) recorded() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedRequest(nil), f.requests...)
}

func newCRMTestService(t *testing.T, client *ent.Client, billingSvc *billing.Service, f *fakeCRM) *Service {
	t.Helper()
	log := zap.NewNop()
	svc := NewService(client, billingSvc, log)
	svc.SetCRMClient(crm.New(f.srv.URL, "test-key", log))
	svc.SetAuditWriter(auditlog.NewWriter(client, log))
	return svc
}

func TestNoConsentNoCRMCall(t *testing.T) {
	ctx := context.Background()
	client := newCRMTestClient(t)
	f := newFakeCRM(t)
	svc := newCRMTestService(t, client, nil, f)
	tenantID := uuid.New()

	p, err := svc.RegisterPatient(ctx, tenantID, RegisterPatientRequest{
		FullName: "Jane Wanjiru", Phone: "0712345678", IDNumber: "12345678", OutletID: uuid.New(),
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	newName := "Jane W. Kamau"
	if _, err := svc.UpdatePatient(ctx, tenantID, p.ID, UpdatePatientRequest{FullName: &newName}); err != nil {
		t.Fatalf("update: %v", err)
	}

	if got := f.recorded(); len(got) != 0 {
		t.Fatalf("CRM was called %d times without consent, want 0", len(got))
	}
	stored, err := svc.GetPatient(ctx, tenantID, p.ID)
	if err != nil {
		t.Fatalf("get patient: %v", err)
	}
	if stored.CrmConsentAt != nil {
		t.Errorf("crm_consent_at = %v, want nil", stored.CrmConsentAt)
	}
	if stored.CrmContactID != nil {
		t.Errorf("crm_contact_id = %v, want nil", stored.CrmContactID)
	}
}

func TestConsentLinksNameAndPhoneOnly(t *testing.T) {
	ctx := context.Background()
	client := newCRMTestClient(t)
	f := newFakeCRM(t)
	svc := newCRMTestService(t, client, nil, f)
	tenantID := uuid.New()
	dob := time.Date(1990, 4, 12, 0, 0, 0, 0, time.UTC)

	p, link, err := svc.RegisterPatientWithCRMLink(ctx, tenantID, RegisterPatientRequest{
		FullName: "Jane Wanjiru Kamau", Phone: "0712345678", DOB: &dob, Sex: "F",
		IDNumber: "29876543", IdentificationType: "national_id", SHABeneficiaryNumber: "SHA-998877",
		Address: "Kilimani", NextOfKin: "John Kamau", Allergies: []string{"penicillin"},
		OutletID: uuid.New(), CRMConsent: true,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if !link.Linked || link.Notice != "" {
		t.Fatalf("link = %+v, want linked with no notice", link)
	}
	if p.CrmConsentAt == nil {
		t.Fatal("crm_consent_at not set after consent")
	}
	if p.CrmContactID == nil || *p.CrmContactID != f.contactID {
		t.Fatalf("crm_contact_id = %v, want %s", p.CrmContactID, f.contactID)
	}

	reqs := f.recorded()
	if len(reqs) != 1 {
		t.Fatalf("CRM calls = %d, want 1", len(reqs))
	}
	if reqs[0].Method != http.MethodPost || !strings.HasSuffix(reqs[0].Path, "/contacts/upsert") {
		t.Fatalf("CRM call = %s %s, want POST .../contacts/upsert", reqs[0].Method, reqs[0].Path)
	}
	allowed := map[string]bool{"tenant_id": true, "phone": true, "email": true, "first_name": true, "last_name": true}
	for key := range reqs[0].Body {
		if !allowed[key] {
			t.Errorf("CRM body carries unexpected key %q", key)
		}
	}
	body := reqs[0].Body
	if body["tenant_id"] != tenantID.String() {
		t.Errorf("tenant_id = %v, want %s", body["tenant_id"], tenantID)
	}
	if body["first_name"] != "Jane" || body["last_name"] != "Wanjiru Kamau" {
		t.Errorf("name = %v / %v, want Jane / Wanjiru Kamau", body["first_name"], body["last_name"])
	}
	if phone, _ := body["phone"].(string); !strings.HasSuffix(phone, "712345678") {
		t.Errorf("phone = %v, want the patient's phone", body["phone"])
	}
	if email, _ := body["email"].(string); email != "" {
		t.Errorf("email = %q, want empty", email)
	}
	raw, _ := json.Marshal(body)
	for _, secret := range []string{p.Mrn, "29876543", "1990-04-12", "SHA-998877", "Kilimani", "John Kamau", "penicillin", "national_id"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("CRM body leaks %q: %s", secret, raw)
		}
	}
}

func TestWithdrawConsentUnlinks(t *testing.T) {
	ctx := context.Background()
	client := newCRMTestClient(t)
	f := newFakeCRM(t)
	svc := newCRMTestService(t, client, nil, f)
	tenantID := uuid.New()
	actorID := uuid.New()

	p, err := svc.RegisterPatient(ctx, tenantID, RegisterPatientRequest{
		FullName: "Jane Wanjiru", Phone: "0712345678", OutletID: uuid.New(), CRMConsent: true,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if p.CrmContactID == nil {
		t.Fatal("patient not linked after consent")
	}

	no := false
	updated, err := svc.UpdatePatient(ctx, tenantID, p.ID, UpdatePatientRequest{CRMConsent: &no, ActorID: actorID})
	if err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if updated.CrmConsentAt != nil {
		t.Errorf("crm_consent_at = %v after withdrawal, want nil", updated.CrmConsentAt)
	}
	if updated.CrmContactID != nil {
		t.Errorf("crm_contact_id = %v after withdrawal, want nil", updated.CrmContactID)
	}

	// The CRM contact is left in place: only the one upsert from registration, no delete.
	reqs := f.recorded()
	if len(reqs) != 1 {
		t.Errorf("CRM calls = %d, want 1 (no call on withdrawal)", len(reqs))
	}
	for _, r := range reqs {
		if r.Method == http.MethodDelete {
			t.Errorf("withdrawal deleted the CRM contact: %s %s", r.Method, r.Path)
		}
	}

	rows, err := client.RbacAuditLog.Query().
		Where(rbacauditlog.TenantID(tenantID), rbacauditlog.Action(AuditActionCRMConsentWithdrawn)).
		All(ctx)
	if err != nil {
		t.Fatalf("query audit log: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("withdrawal audit rows = %d, want 1", len(rows))
	}
	if rows[0].TargetID != p.ID || rows[0].TargetType != "patient" || rows[0].ActorUserID != actorID {
		t.Errorf("audit row = %+v, want target patient %s by actor %s", rows[0], p.ID, actorID)
	}
}

func TestConsentRefusedStillSavesUnlinked(t *testing.T) {
	ctx := context.Background()
	client := newCRMTestClient(t)
	f := newFakeCRM(t)
	f.status = http.StatusUnprocessableEntity
	svc := newCRMTestService(t, client, nil, f)
	tenantID := uuid.New()

	p, link, err := svc.RegisterPatientWithCRMLink(ctx, tenantID, RegisterPatientRequest{
		FullName: "Jane Wanjiru", Phone: "0712345678", OutletID: uuid.New(), CRMConsent: true,
	})
	if err != nil {
		t.Fatalf("register must not fail on a CRM refusal: %v", err)
	}
	if link.Linked || !strings.Contains(link.Notice, "phone is not valid") {
		t.Errorf("link = %+v, want unlinked with the CRM's reason", link)
	}
	if p.CrmContactID != nil {
		t.Errorf("crm_contact_id = %v, want nil", p.CrmContactID)
	}
	if p.CrmConsentAt == nil {
		t.Error("crm_consent_at should still record the consent")
	}

	// Consent without a phone: saved, not linked, no call, no notice.
	q, link, err := svc.RegisterPatientWithCRMLink(ctx, tenantID, RegisterPatientRequest{
		FullName: "Baby Wanjiru", OutletID: uuid.New(), CRMConsent: true,
	})
	if err != nil {
		t.Fatalf("register without phone: %v", err)
	}
	if q.CrmContactID != nil || link.Notice != "" {
		t.Errorf("no-phone patient: contact %v, notice %q, want neither", q.CrmContactID, link.Notice)
	}
	if got := len(f.recorded()); got != 1 {
		t.Errorf("CRM calls = %d, want 1 (none for the patient without a phone)", got)
	}
}

// fakeTreasury records invoice bodies and answers the calls CollectCharge makes.
func fakeTreasury(t *testing.T, invoices *[]map[string]any, mu *sync.Mutex) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/invoices"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			*invoices = append(*invoices, body)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": uuid.New().String(), "invoice_no": "INV-TEST-0001", "status": "pending",
			})
		case strings.HasSuffix(r.URL.Path, "/payments/intents"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": uuid.New().String(), "status": "succeeded"})
		case strings.HasSuffix(r.URL.Path, "/etims/sign-pos-sale"):
			_ = json.NewEncoder(w).Encode(map[string]any{"signed": false})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestInvoiceCarriesCrmIDWhenLinked(t *testing.T) {
	ctx := context.Background()
	client := newCRMTestClient(t)
	f := newFakeCRM(t)
	log := zap.NewNop()

	var (
		mu       sync.Mutex
		invoices []map[string]any
	)
	treSrv := fakeTreasury(t, &invoices, &mu)
	billingSvc := billing.NewService(client, treasury.NewClient(treSrv.URL, "test-key", log), log)
	svc := newCRMTestService(t, client, billingSvc, f)
	tenantID := uuid.New()
	outletID := uuid.New()

	collect := func(p *ent.Patient) map[string]any {
		t.Helper()
		v, err := svc.CheckInVisit(ctx, tenantID, CheckInVisitRequest{PatientID: p.ID, OutletID: outletID})
		if err != nil {
			t.Fatalf("check in: %v", err)
		}
		tx, err := client.Tx(ctx)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		charge, err := billingSvc.PostCharge(ctx, tx, tenantID, billing.PostChargeRequest{
			PatientID: p.ID, VisitID: v.ID, SourceModule: "records", Description: "Registration", Amount: 150,
		})
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("post charge: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
		if _, err := billingSvc.CollectCharge(ctx, tenantID, charge.ID, billing.CollectChargeRequest{PaymentMethod: "cash"}); err != nil {
			t.Fatalf("collect charge: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		return invoices[len(invoices)-1]
	}

	linked, err := svc.RegisterPatient(ctx, tenantID, RegisterPatientRequest{
		FullName: "Jane Wanjiru", Phone: "0712345678", OutletID: outletID, CRMConsent: true,
	})
	if err != nil {
		t.Fatalf("register linked: %v", err)
	}
	inv := collect(linked)
	if inv["customer_id"] != f.contactID.String() {
		t.Errorf("customer_id = %v, want %s", inv["customer_id"], f.contactID)
	}
	if inv["customer_name"] != "Jane Wanjiru" {
		t.Errorf("customer_name = %v, want Jane Wanjiru", inv["customer_name"])
	}
	if inv["customer_phone"] != "0712345678" {
		t.Errorf("customer_phone = %v, want 0712345678", inv["customer_phone"])
	}

	unlinked, err := svc.RegisterPatient(ctx, tenantID, RegisterPatientRequest{
		FullName: "John Otieno", Phone: "0722000111", OutletID: outletID,
	})
	if err != nil {
		t.Fatalf("register unlinked: %v", err)
	}
	inv = collect(unlinked)
	for _, key := range []string{"customer_id", "customer_name", "customer_phone"} {
		if _, ok := inv[key]; ok {
			t.Errorf("unlinked patient's invoice carries %s = %v, want absent", key, inv[key])
		}
	}
}
