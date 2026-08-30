package handlers

import (
	"net/http"

	"github.com/Bengo-Hub/pagination"

	"github.com/bengobox/hospital-service/internal/modules/auditlog"
)

// AuditLogHandler exposes a read-only view of RBAC mutations (role assigned/changed,
// role created/customized/edited, user status changed) for this tenant.
type AuditLogHandler struct {
	audit *auditlog.Writer
}

// NewAuditLogHandler creates a new AuditLogHandler.
func NewAuditLogHandler(audit *auditlog.Writer) *AuditLogHandler {
	return &AuditLogHandler{audit: audit}
}

type auditLogEntryDTO struct {
	ID         string         `json:"id"`
	ActorID    string         `json:"actor_user_id"`
	ActorEmail string         `json:"actor_email,omitempty"`
	Action     string         `json:"action"`
	TargetType string         `json:"target_type"`
	TargetID   string         `json:"target_id"`
	Before     map[string]any `json:"before,omitempty"`
	After      map[string]any `json:"after,omitempty"`
	CreatedAt  string         `json:"created_at"`
}

// List handles GET /{tenant}/hospital/audit-log
func (h *AuditLogHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := tenantFromRequest(r)
	if !ok {
		respondError(w, http.StatusBadRequest, "tenant context required")
		return
	}
	params := pagination.Parse(r)
	rows, total, err := h.audit.List(r.Context(), tenantID, params.Limit, (params.Page-1)*params.Limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list audit log")
		return
	}
	out := make([]auditLogEntryDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, auditLogEntryDTO{
			ID:         row.ID.String(),
			ActorID:    row.ActorUserID.String(),
			ActorEmail: row.ActorEmail,
			Action:     row.Action,
			TargetType: row.TargetType,
			TargetID:   row.TargetID.String(),
			Before:     row.Before,
			After:      row.After,
			CreatedAt:  row.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	respondJSON(w, http.StatusOK, pagination.NewResponse(out, total, params))
}
