package events

import (
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/bengobox/hospital-service/internal/config"
)

// Connect opens a resilient NATS connection (infinite reconnect).
func Connect(cfg config.EventsConfig) (*nats.Conn, error) {
	opts := []nats.Option{
		nats.Name("hospital-api"),
		nats.Timeout(5 * time.Second),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(-1),
	}

	return nats.Connect(cfg.NATSURL, opts...)
}

// EnsureStream creates/updates the hospital JetStream stream that carries all
// hospital.* domain events. Subjects follow {aggregate_type}.{event_type};
// the aggregate_type for this service is always "hospital"
// (see shared-docs/event-architecture.md).
func EnsureStream(nc *nats.Conn, cfg config.EventsConfig) error {
	if nc == nil {
		return fmt.Errorf("nats connection is nil")
	}

	js, err := nc.JetStream()
	if err != nil {
		return fmt.Errorf("jetstream init: %w", err)
	}

	desiredSubjects := []string{"hospital.>"}

	info, err := js.StreamInfo(cfg.StreamName)
	if err == nil {
		if len(info.Config.Subjects) != len(desiredSubjects) || info.Config.Subjects[0] != desiredSubjects[0] {
			info.Config.Subjects = desiredSubjects
			if _, updateErr := js.UpdateStream(&info.Config); updateErr != nil {
				return fmt.Errorf("update stream subjects: %w", updateErr)
			}
		}
		return nil
	}

	_, err = js.AddStream(&nats.StreamConfig{
		Name:     cfg.StreamName,
		Subjects: desiredSubjects,
		Replicas: 1,
	})
	return err
}
