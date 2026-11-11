package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	entpatient "github.com/bengobox/hospital-service/internal/ent/patient"
	enttenant "github.com/bengobox/hospital-service/internal/ent/tenant"
)

// CRMLinksHandler tells marketflow whether the hospital still links a CRM contact, so marketflow
// can decide between deleting an erased person's contact and stripping it to a minimal record.
// Patients are never hard deleted (records retention), so a linked patient keeps the contact.
type CRMLinksHandler struct {
	db  *ent.Client
	log *zap.Logger
}

// NewCRMLinksHandler builds the handler.
func NewCRMLinksHandler(db *ent.Client, log *zap.Logger) *CRMLinksHandler {
	if log == nil {
		log = zap.NewNop()
	}
	return &CRMLinksHandler{db: db, log: log}
}

// Referenced answers whether any patient, in any tenant, carries the contact id. {tenant} is the
// tenant UUID or slug and must be known here. The answer is the flag only: no record numbers, names or ids.
// GET /api/v1/s2s/{tenant}/crm-links/{contactID} (X-API-Key) -> {"referenced": bool}
func (h *CRMLinksHandler) Referenced(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	contactID, err := uuid.Parse(chi.URLParam(r, "contactID"))
	if err != nil || contactID == uuid.Nil {
		respondError(w, http.StatusBadRequest, "contact id must be a UUID")
		return
	}
	// The tenant must be known here, by UUID or slug. An unknown UUID (a tenant this service holds
	// under another id after a sync drift) answers 404, never "not referenced": marketflow then
	// keeps the contact instead of deleting it.
	raw := chi.URLParam(r, "tenant")
	pred := enttenant.Slug(raw)
	if id, perr := uuid.Parse(raw); perr == nil {
		pred = enttenant.ID(id)
	}
	t, err := h.db.Tenant.Query().Where(pred).Only(ctx)
	if ent.IsNotFound(err) {
		respondError(w, http.StatusNotFound, "tenant not found")
		return
	}
	if err != nil {
		h.log.Error("crm links: tenant lookup failed", zap.Error(err))
		respondError(w, http.StatusInternalServerError, "failed to check links")
		return
	}
	tenantID := t.ID
	// The contact id is matched in every tenant: CRM contact ids are random UUIDs minted by
	// marketflow, so a match anywhere is this person, and a tenant mismatch can only answer
	// "referenced", which keeps the contact.
	found, err := h.db.Patient.Query().Where(entpatient.CrmContactID(contactID)).Exist(ctx)
	if err != nil {
		// Ids only in the log, never the person's details.
		h.log.Error("crm links check failed", zap.String("tenant_id", tenantID.String()),
			zap.String("contact_id", contactID.String()), zap.Error(err))
		respondError(w, http.StatusInternalServerError, "failed to check links")
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"referenced": found})
}
