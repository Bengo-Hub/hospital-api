package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// DocumentSequence is the per-tenant monotonic counter behind human-readable numbers such as
// mrn and visit_number. Allocation happens in a transaction with a row lock so concurrent
// registrations/check-ins never collide. Ported from library-api's DocumentSequence (adapted
// to treasury-api's original document-sequence pattern) rather than reinvented — the platform's
// established convention for exactly this need, per sprint-1-patient-opd-triage.md.
type DocumentSequence struct{ ent.Schema }

func (DocumentSequence) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New).Immutable(),
		field.UUID("tenant_id", uuid.UUID{}),
		field.String("kind").NotEmpty().Comment("mrn | visit_number"),
		field.String("prefix").Optional(),
		field.Int64("next_value").Default(1),
		field.Int("pad_width").Default(5),
		field.String("format").Optional().
			Comment("Template: {prefix} {seq} {yy} {yyyy} {mm}. Empty = {prefix}{seq}."),
		field.String("reset_period").Default("none").
			Comment("none | yearly | monthly — when the counter restarts at 1"),
		field.String("period_key").Optional().
			Comment("Current period marker (e.g. 2026 or 2026-06); a change triggers a reset"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (DocumentSequence) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "kind").Unique(),
	}
}
