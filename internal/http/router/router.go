package router

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.uber.org/zap"

	httpware "github.com/Bengo-Hub/httpware"
	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/google/uuid"

	"github.com/bengobox/hospital-service/internal/ent"
	handlers "github.com/bengobox/hospital-service/internal/http/handlers"
	outletmw "github.com/bengobox/hospital-service/internal/http/middleware"
	"github.com/bengobox/hospital-service/internal/modules/identity"
	rbacmodule "github.com/bengobox/hospital-service/internal/modules/rbac"
	"github.com/bengobox/hospital-service/internal/platform/subscriptions"
)

// Deps bundles everything the router mounts. Sprint 4+ adds real domain
// handlers here (Patient, Visit, Triage, ...) alongside Health/Ping/AuthMe.
type Deps struct {
	Log            *zap.Logger
	Health         *handlers.HealthHandler
	AuthMiddleware *authclient.AuthMiddleware
	AllowedOrigins []string

	// Trinity Authorization plumbing (nil-safe: each stage no-ops if its dep is nil, so the
	// router still boots in a partially-wired test/dev environment).
	EntClient   *ent.Client
	IdentitySvc *identity.Service
	RBACSvc     *rbacmodule.Service
	AuthMe      *handlers.AuthMeHandler

	// Sprint 1: patient registration, OPD visit check-in/queue, triage.
	Patients *handlers.PatientsHandler
	// Sprint 2: consultation/examination, diagnosis catalog, referrals.
	Consultation *handlers.ConsultationHandler
	// Sprint 5 core: billing ledger (PatientAccount/BillableCharge collect/queue/settle).
	Billing *handlers.BillingHandler
	// Sprint 3: lab ordering, worklist, result capture, test catalog.
	Lab *handlers.LabHandler
	// Sprint 4: prescription lifecycle, dispensing, controlled-substance register.
	Pharmacy *handlers.PharmacyHandler
	// Tenant staff role-management and a read-only config view (2026-08-30).
	Users  *handlers.UsersHandler
	Config *handlers.ConfigHandler
}

// New builds the chi router with the standard platform middleware stack.
func New(d Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(httpware.RequestID)
	r.Use(httpware.Logging(d.Log))
	r.Use(httpware.Recover(d.Log))
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   d.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "Origin", "X-Request-ID", "X-Tenant-ID", "X-Tenant-Slug", "X-API-Key", "Idempotency-Key", "X-Outlet-ID"},
		ExposedHeaders:   []string{"Link", "Retry-After"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/healthz", d.Health.Liveness)
	r.Get("/readyz", d.Health.Readiness)
	r.Get("/metrics", d.Health.Metrics)

	r.Route("/api/v1/{tenant}/hospital", func(r chi.Router) {
		// ── Protected endpoints (auth required) ──────────────────────────────
		r.Group(func(prot chi.Router) {
			if d.AuthMiddleware != nil {
				prot.Use(d.AuthMiddleware.RequireAuth)
				// Mutations-only subscription gate (docs/integrations.md §4).
				prot.Use(subscriptions.SubscriptionGate())
			}

			// JIT provisioning: create/heal the local HospitalUser + role assignment on
			// every authenticated request, before tenant/outlet context is resolved.
			if d.IdentitySvc != nil {
				prot.Use(func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						claims, ok := authclient.ClaimsFromContext(r.Context())
						if ok && claims.Subject != "" {
							subject, _ := uuid.Parse(claims.Subject)
							slug := claims.GetTenantSlug()
							if slug != "" {
								_, err := d.IdentitySvc.EnsureUserFromToken(r.Context(), subject, slug, map[string]any{
									"email":             claims.Email,
									"full_name":         claims.Email,
									"roles":             claims.Roles,
									"permissions":       claims.Permissions,
									"is_platform_owner": claims.IsPlatformOwner,
									"outlet_id":         claims.GetOutletID(),
								})
								if err != nil {
									d.Log.Warn("jit provisioning failed", zap.Error(err))
								}
							}
						}
						next.ServeHTTP(w, r)
					})
				})
			}

			// Tenant param + claims-tenant match (existing scaffold already routes on
			// "{tenant}", not "{tenantID}" — keep the one URL segment, don't double it up).
			prot.Use(httpware.TenantV2(httpware.TenantConfig{
				ClaimsExtractor: func(ctx context.Context) (tenantID, tenantSlug string, isPlatformOwner bool, ok bool) {
					claims, found := authclient.ClaimsFromContext(ctx)
					if !found {
						return "", "", false, false
					}
					return claims.TenantID, claims.GetTenantSlug(), claims.IsPlatformOwner, true
				},
				URLParamFunc: chi.URLParam,
				URLParamName: "tenant",
				Required:     true,
			}))
			if d.EntClient != nil {
				prot.Use(outletmw.OutletContextMiddleware(d.EntClient, d.Log))
			}

			if d.AuthMe != nil {
				prot.Get("/auth/me", d.AuthMe.GetMe)
			}

			// Placeholder route proving JWKS auth + Trinity middleware chain works end to
			// end. Sprint 4+ mounts further domain routes (lab, pharmacy, billing, ...) here,
			// each gated with outletmw.RequireServicePermission(d.RBACSvc, ...).
			prot.Get("/ping", handlers.Ping)

			// Sprint 1 — Patients / OPD Reception / Triage. Each group carries BOTH the Layer 2
			// licensing gate (subscriptions.RequireFeature — enforces the standalone-chemist
			// module toggle at the ROUTE level, not just hidden in hospital-ui's sidebar) and the
			// existing Layer 3 RBAC gate. A chemist-tier tenant's JWT carries only
			// pharmacy_dispensing+billing (see subscriptions-api's hospChemistCore()), so these
			// routes 403 with feature_not_available for it even if someone calls the API directly
			// — closing the gap flagged in sprint-4-pharmacy-dispensing.md's Acceptance Gate.
			if d.Patients != nil {
				prot.With(subscriptions.RequireFeature(subscriptions.FeaturePatientRecords),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermRecordsAdd)).
					Post("/patients", d.Patients.RegisterPatient)
				prot.With(subscriptions.RequireFeature(subscriptions.FeaturePatientRecords),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermRecordsView)).
					Get("/patients", d.Patients.ListPatients)
				prot.With(subscriptions.RequireFeature(subscriptions.FeaturePatientRecords),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermRecordsView)).
					Get("/patients/{patientID}", d.Patients.GetPatient)
				prot.With(subscriptions.RequireFeature(subscriptions.FeaturePatientRecords),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermRecordsChange)).
					Put("/patients/{patientID}", d.Patients.UpdatePatient)

				prot.With(subscriptions.RequireFeature(subscriptions.FeatureReceptionQueuing),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermReceptionAdd)).
					Post("/visits", d.Patients.CheckInVisit)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureReceptionQueuing),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermReceptionView)).
					Get("/visits", d.Patients.ListVisits)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureReceptionQueuing),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermReceptionView)).
					Get("/visits/{visitID}", d.Patients.GetVisit)

				prot.With(subscriptions.RequireFeature(subscriptions.FeatureTriage),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermTriageAdd)).
					Post("/visits/{visitID}/triage", d.Patients.RecordTriage)
			}

			// Sprint 2 — Consultation / Examination / Diagnosis Catalog / Referrals.
			if d.Consultation != nil {
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureConsultation),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermConsultationAdd)).
					Post("/visits/{visitID}/examination", d.Consultation.RecordExamination)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureConsultation),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermConsultationAdd)).
					Post("/visits/{visitID}/refer", d.Consultation.CreateReferral)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureConsultation),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermConsultationView)).
					Get("/visits/{visitID}/referrals", d.Consultation.ListReferrals)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureConsultation),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermConsultationView)).
					Get("/diagnosis-catalog", d.Consultation.ListDiagnosisCatalog)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureConsultation),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermConsultationManage)).
					Post("/diagnosis-catalog", d.Consultation.CreateDiagnosisEntry)
			}

			// Sprint 5 core — Billing ledger. FeatureBilling is in every Afya tier including
			// Chemist, so this gate never blocks a standalone chemist's own walk-in billing.
			// Permission checks for collect/override are partly in-handler (collect_own depends
			// on the specific charge's source_module, resolved after the request body/URL param
			// are known) — see BillingHandler.CollectCharge.
			if d.Billing != nil {
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureBilling),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermBillingView)).
					Get("/visits/{visitID}/account", d.Billing.GetAccountByVisit)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureBilling),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermBillingCollectAny)).
					Get("/billing/queue", d.Billing.ListPendingCharges)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureBilling),
					outletmw.RequireServicePermission(d.RBACSvc,
						rbacmodule.PermBillingCollectOwn, rbacmodule.PermBillingCollectAny)).
					Post("/billing/charges/{chargeID}/collect", d.Billing.CollectCharge)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureBilling),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermBillingManage)).
					Post("/billing/charges/{chargeID}/waive", d.Billing.WaiveCharge)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureBilling),
					outletmw.RequireServicePermission(d.RBACSvc,
						rbacmodule.PermBillingCollectOwn, rbacmodule.PermBillingCollectAny)).
					Post("/billing/accounts/{accountID}/settle", d.Billing.SettleAccount)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureBilling),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermBillingOverrideSettlement)).
					Post("/billing/accounts/{accountID}/override-settlement", d.Billing.OverrideSettlement)

				// PatientNextOfKin (2026-08-30) — the person settling an account needs to record
				// who's settling/collecting on the patient's behalf, so this carries the SAME
				// permission as Settle Account itself, not a records permission (the cashier
				// calling this typically holds neither PermRecordsView nor PermRecordsAdd).
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureBilling),
					outletmw.RequireServicePermission(d.RBACSvc,
						rbacmodule.PermBillingCollectOwn, rbacmodule.PermBillingCollectAny)).
					Get("/patients/{patientID}/next-of-kin", d.Billing.ListNextOfKin)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureBilling),
					outletmw.RequireServicePermission(d.RBACSvc,
						rbacmodule.PermBillingCollectOwn, rbacmodule.PermBillingCollectAny)).
					Post("/patients/{patientID}/next-of-kin", d.Billing.CreateNextOfKin)

				// BillableItemCatalog admin CRUD (Gap 3, 2026-08-29) — so a tenant admin isn't
				// stuck editing the seeded starter price list via direct DB access. Reads sit
				// behind the broad billing view permission; mutations behind the narrower
				// manage_catalog permission (pricing/policy config, not day-to-day collection).
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureBilling),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermBillingView)).
					Get("/billing/catalog", d.Billing.ListCatalog)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureBilling),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermBillingManageCatalog)).
					Post("/billing/catalog", d.Billing.CreateCatalogItem)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureBilling),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermBillingManageCatalog)).
					Put("/billing/catalog/{itemID}", d.Billing.UpdateCatalogItem)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureBilling),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermBillingManageCatalog)).
					Post("/billing/catalog/{itemID}/deactivate", d.Billing.DeactivateCatalogItem)

				// Insurance (Sprint 5 remainder, 2026-08-29) — thin proxies to treasury-api's
				// already-built eligibility/claims connector (see docs/sprints/sprint-5-billing-
				// insurance.md). check-eligibility is read-only (PermBillingView); submit-claim
				// moves a charge from pending to exempted so it carries the same collect
				// permission as the cash path (billing/charges/{id}/collect).
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureBilling),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermBillingView)).
					Post("/visits/{visitID}/insurance/check-eligibility", d.Billing.CheckEligibility)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureBilling),
					outletmw.RequireServicePermission(d.RBACSvc,
						rbacmodule.PermBillingCollectOwn, rbacmodule.PermBillingCollectAny)).
					Post("/visits/{visitID}/insurance/submit-claim", d.Billing.SubmitInsuranceClaim)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureBilling),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermBillingView)).
					Get("/insurance/claims/{claimID}/status", d.Billing.PollInsuranceClaim)
				// Shared picker source (2026-08-30) — Lab and Pharmacy's own insurance-claim UI
				// need this exact same list, mounted once here rather than duplicated per module.
				// Gated on ANY permission that lets a role submit SOME insurance-claim action
				// (not just PermBillingView) — a doctor billing a lab order to insurance holds
				// PermLabAdd but not PermBillingView, and would otherwise 403 on the picker alone.
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureBilling),
					outletmw.RequireServicePermission(d.RBACSvc,
						rbacmodule.PermBillingView, rbacmodule.PermLabAdd, rbacmodule.PermPharmacyPrescribe,
						rbacmodule.PermBillingCollectOwn, rbacmodule.PermBillingCollectAny)).
					Get("/insurance/providers", d.Billing.ListInsuranceProviders)
			}

			// Sprint 3 — Laboratory.
			if d.Lab != nil {
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureLabRequestsBasic),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermLabAdd)).
					Post("/lab-orders", d.Lab.CreateOrder)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureLabRequestsBasic),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermLabView)).
					Get("/lab-orders", d.Lab.ListWorklist)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureLabRequestsBasic),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermLabView)).
					Get("/lab-orders/{orderID}", d.Lab.GetOrder)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureLabRequestsBasic),
					outletmw.RequireServicePermission(d.RBACSvc,
						rbacmodule.PermBillingCollectOwn, rbacmodule.PermBillingCollectAny)).
					Post("/lab-orders/{orderID}/activate", d.Lab.ActivateIfPaid)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureLabRequestsBasic),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermLabManage)).
					Post("/lab-orders/{orderID}/cancel", d.Lab.CancelOrder)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureLabRequestsBasic),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermLabChange)).
					Post("/lab-orders/lines/{lineID}/result", d.Lab.EnterResult)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureLabRequestsBasic),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermLabView)).
					Get("/lab-test-catalog", d.Lab.ListCatalog)
				// Tenant Lab Test Catalog admin CRUD (2026-08-30) — mirrors Billing's catalog
				// CRUD gating shape: reads behind the broad view permission, mutations behind the
				// narrower manage permission.
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureLabRequestsBasic),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermLabView)).
					Get("/lab-test-catalog/entries", d.Lab.ListTenantCatalogEntries)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureLabRequestsBasic),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermLabManage)).
					Post("/lab-test-catalog/entries", d.Lab.CreateLabTestEntry)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureLabRequestsBasic),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermLabManage)).
					Put("/lab-test-catalog/entries/{entryID}", d.Lab.UpdateLabTestEntry)
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureLabRequestsBasic),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermLabManage)).
					Post("/lab-test-catalog/entries/{entryID}/deactivate", d.Lab.DeactivateLabTestEntry)
				// Insurance-path alternative to CollectCharge+activate — same permission gate as
				// /lab-orders/{orderID}/activate since it's that same payment-gate action's
				// insurance leg (see lab.Service.SubmitInsuranceClaim).
				prot.With(subscriptions.RequireFeature(subscriptions.FeatureLabRequestsBasic),
					outletmw.RequireServicePermission(d.RBACSvc,
						rbacmodule.PermBillingCollectOwn, rbacmodule.PermBillingCollectAny)).
					Post("/lab-orders/{orderID}/insurance-claim", d.Lab.SubmitInsuranceClaim)
			}

			// Sprint 4 — Pharmacy / Dispensing (the core migration target). FeaturePharmacyDispense
			// is in every Afya tier including Chemist, so this never blocks a standalone chemist.
			if d.Pharmacy != nil {
				prot.With(subscriptions.RequireFeature(subscriptions.FeaturePharmacyDispense),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermPharmacyPrescribe)).
					Post("/prescriptions", d.Pharmacy.CreatePrescription)
				prot.With(subscriptions.RequireFeature(subscriptions.FeaturePharmacyDispense),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermPharmacyView)).
					Get("/prescriptions", d.Pharmacy.ListPrescriptions)
				prot.With(subscriptions.RequireFeature(subscriptions.FeaturePharmacyDispense),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermPharmacyView)).
					Get("/prescriptions/{prescriptionID}", d.Pharmacy.GetPrescription)
				prot.With(subscriptions.RequireFeature(subscriptions.FeaturePharmacyDispense),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermPharmacyManage)).
					Post("/prescriptions/{prescriptionID}/approve", d.Pharmacy.ApprovePrescription)
				// Re-check drug interactions on demand (2026-08-30) — e.g. a late-disclosed
				// allergy after the initial creation-time check. Same permission as Approve: both
				// are pharmacist clinical-safety decisions on the prescription.
				prot.With(subscriptions.RequireFeature(subscriptions.FeaturePharmacyDispense),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermPharmacyManage)).
					Post("/prescriptions/{prescriptionID}/recheck-interactions", d.Pharmacy.RecheckInteractions)
				prot.With(subscriptions.RequireFeature(subscriptions.FeaturePharmacyDispense),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermPharmacyManage)).
					Post("/prescriptions/{prescriptionID}/lock", d.Pharmacy.LockPrescription)
				prot.With(subscriptions.RequireFeature(subscriptions.FeaturePharmacyDispense),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermPharmacyManage)).
					Post("/prescriptions/{prescriptionID}/reject", d.Pharmacy.RejectPrescription)
				prot.With(subscriptions.RequireFeature(subscriptions.FeaturePharmacyDispense),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermPharmacyManage)).
					Post("/prescriptions/{prescriptionID}/cancel", d.Pharmacy.CancelPrescription)
				prot.With(subscriptions.RequireFeature(subscriptions.FeaturePharmacyDispense),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermPharmacyDispense)).
					Post("/prescriptions/{prescriptionID}/dispense", d.Pharmacy.Dispense)
				// Dispensing label PDF (2026-08-30) — view-only, same permission as viewing the
				// prescription itself (nothing sensitive beyond what's already on the record).
				prot.With(subscriptions.RequireFeature(subscriptions.FeaturePharmacyDispense),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermPharmacyView)).
					Get("/prescriptions/{prescriptionID}/lines/{lineID}/label.pdf", d.Pharmacy.PrintLabel)
				// Controlled-substance dual-witness step-up (2026-08-29 fix): re-authenticates a
				// witness with THEIR OWN credentials before Dispense will accept them — gated the
				// same as Dispense itself (only someone who could initiate a dispense should be
				// calling this at all; the actual witness identity/tenant/distinct-person/
				// permission checks happen inside pharmacy.Service.VerifyWitness, keyed off the
				// witness's OWN re-authenticated claims, not this route's permission gate).
				prot.With(subscriptions.RequireFeature(subscriptions.FeaturePharmacyDispense),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermPharmacyDispense)).
					Post("/pharmacy/verify-witness", d.Pharmacy.VerifyWitness)
				prot.With(subscriptions.RequireFeature(subscriptions.FeaturePharmacyDispense),
					outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermPharmacyManage)).
					Get("/pharmacy/controlled-substances", d.Pharmacy.ListControlledSubstanceLogs)
				// Insurance-settlement alternative to a cash billing/charges/{id}/collect for a
				// dispensed line — a billing action, so it carries the collect permission rather
				// than a pharmacy one.
				prot.With(subscriptions.RequireFeature(subscriptions.FeaturePharmacyDispense),
					outletmw.RequireServicePermission(d.RBACSvc,
						rbacmodule.PermBillingCollectOwn, rbacmodule.PermBillingCollectAny)).
					Post("/prescriptions/{prescriptionID}/insurance-claim", d.Pharmacy.SubmitInsuranceClaim)
			}

			// Users / Config admin — tenant staff role-management and a read-only config view.
			// No subscriptions.RequireFeature gate: unlike the clinical modules above, these are
			// baseline tenant administration available at every facility tier, not a paywalled
			// Afya feature — there is no matching feature code in subscriptions-api's catalog and
			// none should be invented for this (mirrors how /auth/me and /ping are mounted with
			// only the RBAC gate, no feature gate).
			if d.Users != nil {
				prot.With(outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermUsersView)).
					Get("/users", d.Users.ListUsers)
				prot.With(outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermUsersView)).
					Get("/roles", d.Users.ListRoles)
				prot.With(outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermUsersManage)).
					Put("/users/{userID}/role", d.Users.SetUserRole)
			}
			if d.Config != nil {
				prot.With(outletmw.RequireServicePermission(d.RBACSvc, rbacmodule.PermConfigView)).
					Get("/config", d.Config.GetConfig)
			}
		})
	})

	return r
}
