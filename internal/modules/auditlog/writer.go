// Package auditlog records identity/RBAC mutations (role changes, role CRUD, user status
// changes) for this module. A leaf package (imports only ent/zap/uuid) so it can be imported
// from both rbac and identity with zero import-cycle risk.
package auditlog

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/rbacauditlog"
)

// Entry is one audit record. Before/After are optional free-form snapshots — populate whichever
// side is relevant to the action (e.g. only After for a create, both for an update).
type Entry struct {
	TenantID   uuid.UUID
	ActorID    uuid.UUID
	ActorEmail string
	Action     string
	TargetType string
	TargetID   uuid.UUID
	Before     map[string]any
	After      map[string]any
}

// Writer persists audit entries. Record is synchronous, best-effort, and NEVER surfaces an
// error to the caller — it logs and returns, mirroring how the RBAC service's own zap.Info
// calls around role mutations already behave (fire-and-forget, no error path back to the
// caller). An audit-log write failure must never block or fail the underlying RBAC mutation it
// describes.
type Writer struct {
	client *ent.Client
	logger *zap.Logger
}

// NewWriter creates a new Writer.
func NewWriter(client *ent.Client, logger *zap.Logger) *Writer {
	return &Writer{client: client, logger: logger.Named("auditlog")}
}

// Record persists one audit entry, logging (never returning) on failure.
func (w *Writer) Record(ctx context.Context, e Entry) {
	if w == nil || w.client == nil {
		return
	}
	builder := w.client.RbacAuditLog.Create().
		SetTenantID(e.TenantID).
		SetActorUserID(e.ActorID).
		SetAction(e.Action).
		SetTargetType(e.TargetType).
		SetTargetID(e.TargetID)
	if e.ActorEmail != "" {
		builder.SetActorEmail(e.ActorEmail)
	}
	if e.Before != nil {
		builder.SetBefore(e.Before)
	}
	if e.After != nil {
		builder.SetAfter(e.After)
	}
	if _, err := builder.Save(ctx); err != nil {
		w.logger.Warn("audit log write failed",
			zap.String("action", e.Action), zap.String("target_type", e.TargetType), zap.Error(err))
	}
}

// List returns audit entries for tenantID, newest first, for the Audit Log admin page.
func (w *Writer) List(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*ent.RbacAuditLog, int, error) {
	q := w.client.RbacAuditLog.Query().Where(rbacauditlog.TenantID(tenantID))
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := q.Order(ent.Desc(rbacauditlog.FieldCreatedAt)).Limit(limit).Offset(offset).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}
