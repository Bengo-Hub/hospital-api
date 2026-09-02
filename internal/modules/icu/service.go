// Package icu implements Sprint 7's critical-care monitoring half (Afya Hospital tier): severity-
// flag episodes tied to an admission. Billing for the elevated ICU rate is NOT this package's
// concern — see icu_episode.go's schema doc comment: an ICU bed's Ward carries its own
// billable_item_code (Sprint 6), so discharge-time billing already charges the elevated rate with
// zero ICU-specific logic here.
package icu

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/admission"
	"github.com/bengobox/hospital-service/internal/ent/icuepisode"
)

// Service implements ICU-episode business logic.
type Service struct {
	client *ent.Client
	log    *zap.Logger
}

// NewService creates a new icu Service.
func NewService(client *ent.Client, log *zap.Logger) *Service {
	return &Service{client: client, log: log.Named("icu.service")}
}

// StartEpisodeRequest is the input to StartEpisode.
type StartEpisodeRequest struct {
	AdmissionID     uuid.UUID
	SeverityFlag    string
	MonitoringNotes string
	StartedBy       uuid.UUID
}

// StartEpisode opens a new critical-care episode for an active admission. Snapshots the
// admission's CURRENT bed_id — not auto-updated on a later Transfer, see the schema's doc comment.
func (s *Service) StartEpisode(ctx context.Context, tenantID uuid.UUID, req StartEpisodeRequest) (*ent.ICUEpisode, error) {
	adm, err := s.client.Admission.Query().
		Where(admission.ID(req.AdmissionID), admission.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("icu: admission not found: %w", err)
	}
	if adm.Status != admission.StatusActive {
		return nil, fmt.Errorf("icu: admission is not active (status=%s)", adm.Status)
	}
	alreadyActive, err := s.client.ICUEpisode.Query().
		Where(icuepisode.TenantID(tenantID), icuepisode.AdmissionID(req.AdmissionID), icuepisode.EndedAtIsNil()).
		Exist(ctx)
	if err != nil {
		return nil, fmt.Errorf("icu: check existing episode: %w", err)
	}
	if alreadyActive {
		return nil, fmt.Errorf("icu: admission already has an active ICU episode")
	}

	severity := icuepisode.SeverityFlagStable
	if req.SeverityFlag != "" {
		severity = icuepisode.SeverityFlag(req.SeverityFlag)
	}
	create := s.client.ICUEpisode.Create().
		SetTenantID(tenantID).
		SetAdmissionID(req.AdmissionID).
		SetBedID(adm.BedID).
		SetSeverityFlag(severity).
		SetMonitoringNotes(req.MonitoringNotes)
	if req.StartedBy != uuid.Nil {
		create = create.SetStartedBy(req.StartedBy)
	}
	episode, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("icu: start episode: %w", err)
	}
	return episode, nil
}

// GetEpisode fetches an episode by ID, tenant-scoped.
func (s *Service) GetEpisode(ctx context.Context, tenantID, episodeID uuid.UUID) (*ent.ICUEpisode, error) {
	return s.client.ICUEpisode.Query().
		Where(icuepisode.ID(episodeID), icuepisode.TenantID(tenantID)).
		Only(ctx)
}

// ListEpisodes returns episodes for the ICU board. activeOnly=true (the default board view)
// returns only episodes with no ended_at.
func (s *Service) ListEpisodes(ctx context.Context, tenantID uuid.UUID, activeOnly bool) ([]*ent.ICUEpisode, error) {
	q := s.client.ICUEpisode.Query().Where(icuepisode.TenantID(tenantID))
	if activeOnly {
		q = q.Where(icuepisode.EndedAtIsNil())
	}
	return q.Order(ent.Desc(icuepisode.FieldStartedAt)).Limit(200).All(ctx)
}

// UpdateEpisodeRequest is the input to UpdateEpisode — pointers so only sent fields change.
type UpdateEpisodeRequest struct {
	SeverityFlag    *string
	MonitoringNotes *string
}

// UpdateEpisode updates an active episode's severity flag and/or monitoring notes.
func (s *Service) UpdateEpisode(ctx context.Context, tenantID, episodeID uuid.UUID, req UpdateEpisodeRequest) (*ent.ICUEpisode, error) {
	episode, err := s.GetEpisode(ctx, tenantID, episodeID)
	if err != nil {
		return nil, fmt.Errorf("icu: episode not found: %w", err)
	}
	if episode.EndedAt != nil {
		return nil, fmt.Errorf("icu: episode has already ended")
	}
	upd := s.client.ICUEpisode.UpdateOneID(episodeID)
	if req.SeverityFlag != nil {
		upd = upd.SetSeverityFlag(icuepisode.SeverityFlag(*req.SeverityFlag))
	}
	if req.MonitoringNotes != nil {
		upd = upd.SetMonitoringNotes(*req.MonitoringNotes)
	}
	updated, err := upd.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("icu: update episode: %w", err)
	}
	return updated, nil
}

// EndEpisode closes an active episode.
func (s *Service) EndEpisode(ctx context.Context, tenantID, episodeID uuid.UUID) (*ent.ICUEpisode, error) {
	episode, err := s.GetEpisode(ctx, tenantID, episodeID)
	if err != nil {
		return nil, fmt.Errorf("icu: episode not found: %w", err)
	}
	if episode.EndedAt != nil {
		return episode, nil
	}
	now := time.Now()
	return s.client.ICUEpisode.UpdateOneID(episodeID).SetEndedAt(now).Save(ctx)
}
