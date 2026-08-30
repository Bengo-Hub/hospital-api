package identity

import (
	"context"
	"fmt"
	"time"

	sharedevents "github.com/Bengo-Hub/shared-events"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/hospitaluser"
	"github.com/bengobox/hospital-service/internal/ent/userroleassignment"
	"github.com/bengobox/hospital-service/internal/modules/rbac"
)

// mapSSORoleToHospital converts SSO-level global roles to a hospital-api role code.
// Returns "" for roles that are not hospital-relevant. Delegates to
// rbac.MapGlobalRolesToServiceRole — the SINGLE source of truth for this mapping. This used
// to be an independently-maintained duplicate of that switch, and the two silently drifted
// apart: this copy was missing a "cashier" case that the other had, so a cashier-role SSO
// login got zero hospital permissions with no error anywhere. Never re-duplicate this table.
func mapSSORoleToHospital(roles []string) string {
	return rbac.MapGlobalRolesToServiceRole(roles)
}

// AuthEventHandler handles auth-service user events for proactive user sync.
type AuthEventHandler struct {
	client       *ent.Client
	tenantSyncer interface {
		SyncTenant(ctx context.Context, slug string) (uuid.UUID, error)
		SyncOutlets(ctx context.Context, tenantID uuid.UUID, tenantSlug string) error
	}
	rbacService *rbac.Service
	logger      *zap.Logger
}

// NewAuthEventHandler creates a new auth event handler.
func NewAuthEventHandler(client *ent.Client, svc *Service, logger *zap.Logger) *AuthEventHandler {
	return &AuthEventHandler{
		client:       client,
		tenantSyncer: svc.tenantSyncer,
		rbacService:  svc.rbacService,
		logger:       logger.Named("identity.auth_events"),
	}
}

// SubscribeToAuthEvents subscribes to auth-service user events via JetStream with durable consumers.
func (h *AuthEventHandler) SubscribeToAuthEvents(nc *nats.Conn) error {
	if nc == nil {
		h.logger.Warn("NATS connection not available, skipping auth event subscriptions")
		return nil
	}

	js, err := nc.JetStream()
	if err != nil {
		return fmt.Errorf("auth user events: jetstream init: %w", err)
	}

	// Ensure auth stream exists (guard against startup race with auth-api).
	if _, err := js.StreamInfo(authStream); err != nil {
		if _, addErr := js.AddStream(&nats.StreamConfig{
			Name:      authStream,
			Subjects:  []string{"auth.>"},
			Retention: nats.LimitsPolicy,
			MaxAge:    72 * time.Hour,
			Storage:   nats.FileStorage,
		}); addErr != nil && addErr != nats.ErrStreamNameAlreadyInUse {
			h.logger.Warn("auth user events: ensure auth stream failed", zap.Error(addErr))
		}
	}

	type sub struct {
		subject string
		durable string
		handler func(context.Context, *sharedevents.Event) error
	}
	subs := []sub{
		{"auth.user.created", "hospital-auth-user-created", h.handleUserCreated},
		{"auth.user.updated", "hospital-auth-user-updated", h.handleUserUpdated},
		{"auth.user.deleted", "hospital-auth-user-deleted", h.handleUserDeleted},
	}

	for _, s := range subs {
		s := s
		sharedevents.SubscribeQueueWithRebind(h.logger, js, "auth", s.subject, s.durable, func(msg *nats.Msg) {
			evt, err := sharedevents.FromJSON(msg.Data)
			if err != nil {
				h.logger.Error("failed to unmarshal auth user event",
					zap.String("subject", s.subject), zap.Error(err))
				_ = msg.Nak()
				return
			}
			ctx := context.Background()
			if err := s.handler(ctx, evt); err != nil {
				h.logger.Error("failed to handle auth user event",
					zap.String("subject", s.subject), zap.Error(err))
				_ = msg.Nak()
				return
			}
			_ = msg.Ack()
		},
			nats.Durable(s.durable),
			nats.AckExplicit(),
			nats.AckWait(30*time.Second),
			nats.MaxDeliver(5),
			nats.DeliverAll(),
		)
	}

	h.logger.Info("auth event subscriptions active",
		zap.String("subjects", "auth.user.created, auth.user.updated"))
	return nil
}

// handleUserDeleted hard-deletes this user's local hospital-api rows after auth-api
// permanently deletes the account (AdminPurgeUser). UserRoleAssignment has a real
// OnDelete:NoAction FK to hospital_users, so it must go first.
func (h *AuthEventHandler) handleUserDeleted(ctx context.Context, evt *sharedevents.Event) error {
	userIDStr, _ := evt.Payload["user_id"].(string)
	authServiceUserID, err := uuid.Parse(userIDStr)
	if err != nil {
		return fmt.Errorf("invalid user_id %q: %w", userIDStr, err)
	}

	tx, err := h.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("start tx: %w", err)
	}

	if _, err := tx.UserRoleAssignment.Delete().Where(userroleassignment.UserID(authServiceUserID)).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete user role assignments: %w", err)
	}
	if _, err := tx.HospitalUser.Delete().Where(hospitaluser.AuthServiceUserID(authServiceUserID)).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete hospital user: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	h.logger.Info("user hard-deleted from auth.user.deleted event", zap.String("user_id", userIDStr))
	return nil
}

// eventRoles extracts the "roles" array from an event payload.
func eventRoles(payload map[string]interface{}) []string {
	var roles []string
	if raw, ok := payload["roles"].([]interface{}); ok {
		for _, r := range raw {
			if str, ok := r.(string); ok {
				roles = append(roles, str)
			}
		}
	}
	return roles
}

// eventOutletID extracts the primary outlet_id from an event payload, falling back to the
// first entry of outlet_ids for callers that only populate the plural field.
func eventOutletID(payload map[string]interface{}) string {
	if id, ok := payload["outlet_id"].(string); ok && id != "" {
		return id
	}
	if raw, ok := payload["outlet_ids"].([]interface{}); ok && len(raw) > 0 {
		if id, ok := raw[0].(string); ok {
			return id
		}
	}
	return ""
}

// resolveOutletUseCase reports the use_case of outletIDStr and whether it could be resolved
// at all. The local Outlet mirror only ever holds outlets accepted by
// tenant.HospitalAcceptedUseCases (auth_outlet_events.go's handleUpsert filters everything
// else before it's synced), so a local hit is authoritative. A miss is backfilled once via
// SyncOutlets to rule out the startup race where a user-created event for a genuine hospital
// outlet arrives before that outlet's own auth.outlet.created event — only after that retry
// still misses is the outlet trusted as confirmed non-hospital (or simply unknown/deleted).
func (h *AuthEventHandler) resolveOutletUseCase(ctx context.Context, tenantID uuid.UUID, tenantSlug, outletIDStr string) (useCase string, resolved bool) {
	outletID, err := uuid.Parse(outletIDStr)
	if err != nil {
		return "", false
	}
	if o, err := h.client.Outlet.Get(ctx, outletID); err == nil {
		if o.UseCase != nil {
			return *o.UseCase, true
		}
		return "", true
	}
	if h.tenantSyncer != nil {
		_ = h.tenantSyncer.SyncOutlets(ctx, tenantID, tenantSlug)
	}
	if o, err := h.client.Outlet.Get(ctx, outletID); err == nil {
		if o.UseCase != nil {
			return *o.UseCase, true
		}
		return "", true
	}
	return "", false
}

// isHospitalRelevant decides whether a JIT-provisioning event should create/update a
// HospitalUser row and assign a hospital role. codevertex-demo (and any other tenant with
// staff across several verticals) hosts every use case under ONE auth-api tenant, and role
// names like admin/manager/cashier/receptionist are reused verbatim by every vertical, so
// neither the tenant nor the role name alone can prove hospital relevance — outlet evidence
// is checked first because it is the only signal specific enough to separate "this tenant's
// hospital outlet" from "this tenant's retail/hospitality/pharmacy/... outlet".
//
//   - outlet resolved -> trust it exclusively (use_case == "hospital").
//   - outlet unresolvable (no outlet_id in the event, or the outlet is unknown/deleted) ->
//     fall back to the role name, trusting only names unique to the hospital vertical
//     (rbac.HasUnambiguousHospitalRole) or a tenant whose own primary use_case is "hospital"
//     (a genuinely single-vertical hospital tenant's founding admin, before any outlet exists).
func isHospitalRelevant(outletUseCase string, outletResolved bool, tenantUseCase *string, roles []string) bool {
	if outletResolved {
		return outletUseCase == "hospital"
	}
	if rbac.HasUnambiguousHospitalRole(roles) {
		return true
	}
	return tenantUseCase != nil && *tenantUseCase == "hospital"
}

// shouldProvisionForHospital wires isHospitalRelevant up to this handler's outlet/tenant
// lookups for one event payload. See isHospitalRelevant for the decision policy.
func (h *AuthEventHandler) shouldProvisionForHospital(ctx context.Context, tenantID uuid.UUID, tenantSlug string, payload map[string]interface{}) bool {
	var (
		outletUseCase string
		resolved      bool
	)
	if outletIDStr := eventOutletID(payload); outletIDStr != "" {
		outletUseCase, resolved = h.resolveOutletUseCase(ctx, tenantID, tenantSlug, outletIDStr)
	}
	var tenantUseCase *string
	if t, err := h.client.Tenant.Get(ctx, tenantID); err == nil {
		tenantUseCase = t.UseCase
	}
	return isHospitalRelevant(outletUseCase, resolved, tenantUseCase, eventRoles(payload))
}

func (h *AuthEventHandler) handleUserCreated(ctx context.Context, evt *sharedevents.Event) error {
	userIDStr, _ := evt.Payload["user_id"].(string)
	email, _ := evt.Payload["email"].(string)
	fullName, _ := evt.Payload["full_name"].(string)
	tenantSlug, _ := evt.Payload["tenant_slug"].(string)

	authServiceUserID, err := uuid.Parse(userIDStr)
	if err != nil {
		return fmt.Errorf("invalid user_id %q: %w", userIDStr, err)
	}

	exists, _ := h.client.HospitalUser.Query().
		Where(hospitaluser.AuthServiceUserIDEQ(authServiceUserID)).
		Exist(ctx)
	if exists {
		h.logger.Debug("user already exists, skipping", zap.String("user_id", userIDStr))
		return nil
	}

	// Resolve tenant ID — prefer tenant_slug from payload, fallback to event tenant_id.
	var tenantID uuid.UUID
	if tenantSlug != "" && h.tenantSyncer != nil {
		tenantID, err = h.tenantSyncer.SyncTenant(ctx, tenantSlug)
		if err != nil {
			h.logger.Warn("failed to sync tenant from slug, using event tenant_id",
				zap.String("slug", tenantSlug), zap.Error(err))
			tenantID = evt.TenantID
		}
	} else {
		tenantID = evt.TenantID
	}
	if tenantID == uuid.Nil {
		return fmt.Errorf("no tenant_id available for user %s", userIDStr)
	}

	if !h.shouldProvisionForHospital(ctx, tenantID, tenantSlug, evt.Payload) {
		h.logger.Info("skipping user: not hospital-relevant",
			zap.String("user_id", userIDStr), zap.String("tenant_slug", tenantSlug))
		return nil
	}

	created, err := h.client.HospitalUser.Create().
		SetID(authServiceUserID).
		SetAuthServiceUserID(authServiceUserID).
		SetTenantID(tenantID).
		SetEmail(email).
		SetName(fullName).
		SetStatus("active").
		SetSyncStatus("synced").
		SetLastSyncAt(time.Now()).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("create user from auth event: %w", err)
	}

	h.logger.Info("user created from auth.user.created event",
		zap.String("user_id", userIDStr),
		zap.String("tenant_id", tenantID.String()),
		zap.String("email", email))

	// Assign a default role mapped from the event's global roles.
	if h.rbacService != nil {
		if roleCode := mapSSORoleToHospital(eventRoles(evt.Payload)); roleCode != "" {
			if err := h.rbacService.AssignRoleByCode(ctx, tenantID, created.ID, authServiceUserID, roleCode); err != nil {
				h.logger.Warn("JIT role assignment failed on user.created",
					zap.String("role_code", roleCode), zap.Error(err))
			}
		}
	}

	return nil
}

func (h *AuthEventHandler) handleUserUpdated(ctx context.Context, evt *sharedevents.Event) error {
	userIDStr, _ := evt.Payload["user_id"].(string)
	email, _ := evt.Payload["email"].(string)
	fullName, _ := evt.Payload["full_name"].(string)

	authServiceUserID, err := uuid.Parse(userIDStr)
	if err != nil {
		return fmt.Errorf("invalid user_id %q: %w", userIDStr, err)
	}

	u, err := h.client.HospitalUser.Query().
		Where(hospitaluser.AuthServiceUserIDEQ(authServiceUserID)).
		Only(ctx)
	if err != nil {
		if !ent.IsNotFound(err) {
			return fmt.Errorf("query user: %w", err)
		}
		// User row doesn't exist (e.g. after a DB reset). Backfill-create it.
		tenantID := evt.TenantID
		tenantSlug, _ := evt.Payload["tenant_slug"].(string)
		if tenantSlug != "" && h.tenantSyncer != nil {
			if tid, syncErr := h.tenantSyncer.SyncTenant(ctx, tenantSlug); syncErr == nil {
				tenantID = tid
			}
		}
		if tenantID == uuid.Nil {
			h.logger.Debug("user not found and no tenant_id in updated event, skipping",
				zap.String("user_id", userIDStr))
			return nil
		}
		if !h.shouldProvisionForHospital(ctx, tenantID, tenantSlug, evt.Payload) {
			h.logger.Info("skipping backfill-create: not hospital-relevant",
				zap.String("user_id", userIDStr), zap.String("tenant_slug", tenantSlug))
			return nil
		}
		created, createErr := h.client.HospitalUser.Create().
			SetID(authServiceUserID).
			SetAuthServiceUserID(authServiceUserID).
			SetTenantID(tenantID).
			SetEmail(email).
			SetName(fullName).
			SetStatus("active").
			SetSyncStatus("synced").
			SetLastSyncAt(time.Now()).
			Save(ctx)
		if createErr != nil {
			return fmt.Errorf("create user from updated event (backfill): %w", createErr)
		}
		h.logger.Info("user created from auth.user.updated event (backfill)",
			zap.String("user_id", userIDStr))
		u = created
	} else {
		update := h.client.HospitalUser.UpdateOne(u)
		changed := false
		if email != "" {
			update = update.SetEmail(email)
			changed = true
		}
		if fullName != "" {
			update = update.SetName(fullName)
			changed = true
		}
		if changed {
			update = update.SetSyncStatus("synced").SetLastSyncAt(time.Now())
			if _, err := update.Save(ctx); err != nil {
				return fmt.Errorf("update user from auth event: %w", err)
			}
		}
	}

	// Re-sync role — corrects stale role mappings (e.g. a promotion from nurse to manager).
	// Gated the same as creation: an existing row (e.g. provisioned before this hardening
	// shipped) is never deleted here, but it must stop accruing hospital permissions once its
	// outlet/role evidence no longer supports them.
	tenantSlug, _ := evt.Payload["tenant_slug"].(string)
	if h.rbacService != nil && h.shouldProvisionForHospital(ctx, u.TenantID, tenantSlug, evt.Payload) {
		if roleCode := mapSSORoleToHospital(eventRoles(evt.Payload)); roleCode != "" {
			if err := h.rbacService.AssignRoleByCode(ctx, u.TenantID, u.ID, authServiceUserID, roleCode); err != nil {
				h.logger.Warn("JIT role re-sync failed on user.updated",
					zap.String("role_code", roleCode), zap.Error(err))
			}
		}
	}

	h.logger.Info("user updated from auth.user.updated event",
		zap.String("user_id", userIDStr),
		zap.String("email", email))
	return nil
}
