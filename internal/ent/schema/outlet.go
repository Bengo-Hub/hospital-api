package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Outlet holds the schema definition for the Outlet entity — a local projection of
// auth-service's outlet (branch/facility). Mirrors pos-api's Outlet shape.
type Outlet struct {
	ent.Schema
}

// Fields of the Outlet.
func (Outlet) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.String("tenant_slug").
			NotEmpty(),
		field.String("code").
			NotEmpty(),
		field.String("name").
			NotEmpty(),
		field.JSON("address_json", map[string]any{}).
			Optional(),
		field.String("status").
			Default("active"),
		field.String("use_case").
			Optional().
			Nillable().
			Comment("Use case for this outlet, e.g. hospital"),
		field.String("facility_type").
			Optional().
			Nillable().
			Comment("Afya facility tier for THIS outlet — chemist|clinic|facility|hospital. Sourced " +
				"from auth-api's Outlet.metadata (no schema change there), synced the same way " +
				"use_case already is. Presentation-only (which nav hospital-ui's adaptive sidebar " +
				"shows), NOT a licensing/feature-gating field — that stays the subscription plan's " +
				"job. 2026-09-02: replaces deriving nav tier from the tenant-wide subscription, " +
				"which couldn't distinguish two outlets under the same tenant and needed a real " +
				"paid/assigned plan to resolve to anything but the safe default."),
		field.Bool("is_hq").
			Default(false).
			Comment("HQ outlets bypass outlet-scoped data filtering — staff see all outlets"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the Outlet.
func (Outlet) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).
			Ref("outlets").
			Field("tenant_id").
			Unique().
			Required(),
	}
}

// Indexes of the Outlet.
func (Outlet) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "code").Unique(),
		index.Fields("tenant_slug"),
	}
}
