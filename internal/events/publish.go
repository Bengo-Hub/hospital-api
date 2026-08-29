// Package events provides the transactional-outbox publish helper. Publishing = inserting an
// outbox_events row (within the domain's Ent transaction); the shared-events OutboxPoller wired
// in app.go drains it to NATS. Subject = {aggregate_type}.{event_type}; aggregate_type for this
// service is always "hospital". Ported from library-api's internal/events/publish.go, the
// platform's standard shape for this exact need — first real use of shared-events in this
// service (Sprint 1).
//
// The payload column MUST hold the FULL shared-events envelope (Event.ToJSON()): the poller
// rebuilds the event solely via FromJSON(payload) and derives the subject from the deserialized
// envelope — a bare business payload publishes to subject "." and is lost.
package events

import (
	"context"
	"encoding/json"
	"fmt"

	eventslib "github.com/Bengo-Hub/shared-events"
	"github.com/google/uuid"

	"github.com/bengobox/hospital-service/internal/ent"
)

// AggregateType is the constant aggregate_type for every hospital event.
const AggregateType = "hospital"

// Hospital event types (the {event_type} half of the subject).
const (
	EventPatientCreated   = "patient.created"
	EventVisitAdmitted    = "visit.admitted"
	EventVisitDischarged  = "visit.discharged"
	EventLabOrderResulted = "lab_order.resulted"
)

// Publisher inserts outbox rows. oc is either client.OutboxEvent or tx.OutboxEvent.
type Publisher interface {
	Create() *ent.OutboxEventCreate
}

// Publish writes one outbox event row. Pass tx.OutboxEvent to publish atomically with the
// domain write, or client.OutboxEvent for a standalone publish. aggregateID should be the
// aggregate's UUID string; a non-UUID ref is mapped to a stable tenant-namespaced SHA1 UUID.
func Publish(ctx context.Context, oc Publisher, tenantID uuid.UUID, aggregateID, eventType string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("events: marshal payload: %w", err)
	}
	var payloadMap map[string]interface{}
	if err := json.Unmarshal(b, &payloadMap); err != nil {
		return fmt.Errorf("events: payload must be a JSON object: %w", err)
	}

	aggUUID, err := uuid.Parse(aggregateID)
	if err != nil {
		aggUUID = uuid.NewSHA1(tenantID, []byte(aggregateID))
	}

	ev := eventslib.NewEvent(eventType, AggregateType, aggUUID, tenantID, payloadMap)
	raw, err := ev.ToJSON()
	if err != nil {
		return fmt.Errorf("events: marshal envelope: %w", err)
	}

	_, err = oc.Create().
		SetID(ev.ID).
		SetTenantID(tenantID).
		SetAggregateType(AggregateType).
		SetAggregateID(aggUUID.String()).
		SetEventType(eventType).
		SetPayload(json.RawMessage(raw)).
		Save(ctx)
	return err
}
