package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// HospitalUser holds the schema definition for hospital service users — a JIT-provisioned
// local projection of an auth-service user. ID is a LOCALLY generated UUID (ent's default
// generator, see the "id" field below); auth_service_user_id references the auth-service
// account and is unique only WITHIN a tenant (see the composite index below) — the same
// auth-service user may hold one HospitalUser row per tenant they belong to (e.g. someone who
// is staff at two separate hospital tenants). A prior version of this schema made
// auth_service_user_id globally unique and set it as the row's own ID, which meant one
// auth-service user could only ever have a single HospitalUser row across the ENTIRE
// platform — visiting a second tenant silently reused/corrupted the first tenant's row. Fixed
// 2026-08-30; see the user-management addendum in docs/plan.md.
type HospitalUser struct {
	ent.Schema
}

// Fields of the HospitalUser.
func (HospitalUser) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}).
			Comment("Tenant identifier"),
		field.UUID("auth_service_user_id", uuid.UUID{}).
			Comment("Reference to auth-service user; unique per-tenant (see the composite index below), not platform-wide"),
		field.String("email").
			NotEmpty().
			Comment("Denormalized email for convenience"),
		field.String("name").
			Optional().
			Comment("Denormalized full name (from the auth user profile) for display"),
		field.String("status").
			Default("active").
			Comment("Status: active, inactive, suspended"),
		field.String("professional_registration_number").
			Optional().
			Comment("KMPDC/Nursing Council/Pharmacy and Poisons Board etc. registration number for internal clinical staff — distinct from facility-level KMPDC tracking (tenant metadata) and from Prescription.prescriber_license (a free-text field for an external/chemist walk-in prescriber)"),
		field.String("professional_registration_body").
			Optional().
			Comment(`The issuing body, e.g. "KMPDC", "Nursing Council of Kenya", "Pharmacy and Poisons Board" — free text, not a closed enum, since Kenya has other regulatory bodies too`),
		field.String("sync_status").
			Default("synced").
			Comment("Sync status: synced, pending, failed"),
		field.Time("last_sync_at").
			Optional().
			Nillable(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the HospitalUser.
func (HospitalUser) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).
			Ref("users").
			Field("tenant_id").
			Unique().
			Required(),
		edge.From("role_assignments", UserRoleAssignment.Type).Ref("user"),
	}
}

// Indexes of the HospitalUser.
func (HospitalUser) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id"),
		index.Fields("tenant_id", "auth_service_user_id").Unique(),
		index.Fields("status"),
		index.Fields("sync_status"),
	}
}
