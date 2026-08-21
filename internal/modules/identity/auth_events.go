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
// Returns "" for roles that are not hospital-relevant.
func mapSSORoleToHospital(roles []string) string {
	for _, r := range roles {
		switch r {
		case "admin", "superuser":
			return rbac.RoleAdmin // wildcard — unrestricted tenant access
		case "manager":
			return rbac.RoleManager
		case "doctor", "clinician", "physician":
			return rbac.RoleDoctor
		case "nurse":
			return rbac.RoleNurse
		case "pharmacist":
			return rbac.RolePharmacist
		case "records_clerk", "receptionist":
			return rbac.RoleRecordsClerk
		}
	}
	return ""
}

// AuthEventHandler handles auth-service user events for proactive user sync.
type AuthEventHandler struct {
	client       *ent.Client
	tenantSyncer interface {
		SyncTenant(ctx context.Context, slug string) (uuid.UUID, error)
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
	if h.rbacService != nil {
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
