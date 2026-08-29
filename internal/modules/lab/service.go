// Package lab implements Sprint 3: test ordering, sample tracking, and result capture/delivery.
// Ordering posts one BillableCharge per test via billing.Service; a test whose catalogue/policy
// requires prepayment holds the order at "awaiting_payment" until settled — the generalized
// form of pos-api's original ActivateLabOrderIfPaid gate (now driven by the shared billing
// ledger, not a bespoke payment_order_id field).
package lab

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/billablecharge"
	"github.com/bengobox/hospital-service/internal/ent/billableitemcatalog"
	"github.com/bengobox/hospital-service/internal/ent/laborder"
	"github.com/bengobox/hospital-service/internal/ent/laborderline"
	"github.com/bengobox/hospital-service/internal/ent/labtestcatalogdefault"
	"github.com/bengobox/hospital-service/internal/ent/labtestcatalogentry"
	"github.com/bengobox/hospital-service/internal/ent/patientvisit"
	events "github.com/bengobox/hospital-service/internal/events"
	"github.com/bengobox/hospital-service/internal/modules/billing"
)

// Service implements lab ordering/result-capture business logic.
type Service struct {
	client  *ent.Client
	billing *billing.Service
	log     *zap.Logger
}

// NewService creates a new lab service.
func NewService(client *ent.Client, billingSvc *billing.Service, log *zap.Logger) *Service {
	return &Service{client: client, billing: billingSvc, log: log.Named("lab.service")}
}

// catalogTest is a resolved test's snapshot fields, from either the tenant or global catalogue.
type catalogTest struct {
	Code         string
	Name         string
	Price        float64
	SpecimenType string
}

func (s *Service) resolveTest(ctx context.Context, tenantID uuid.UUID, code string) (*catalogTest, error) {
	if e, err := s.client.LabTestCatalogEntry.Query().
		Where(labtestcatalogentry.TenantID(tenantID), labtestcatalogentry.Code(code)).
		Only(ctx); err == nil {
		return &catalogTest{Code: e.Code, Name: e.Name, Price: e.Price, SpecimenType: e.SpecimenType}, nil
	}
	d, err := s.client.LabTestCatalogDefault.Query().
		Where(labtestcatalogdefault.Code(code)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("lab: unknown test code %q: %w", code, err)
	}
	return &catalogTest{Code: d.Code, Name: d.Name, Price: d.Price, SpecimenType: d.SpecimenType}, nil
}

// requiresPrepayment reports whether the tenant's lab department billing policy requires
// payment before a lab order may proceed. Fails open (false) when no policy is configured, so a
// tenant that hasn't set up BillableItemCatalog yet is never silently blocked.
func (s *Service) requiresPrepayment(ctx context.Context, tenantID uuid.UUID) bool {
	exists, err := s.client.BillableItemCatalog.Query().
		Where(
			billableitemcatalog.TenantID(tenantID),
			billableitemcatalog.DepartmentEQ(billableitemcatalog.DepartmentLab),
			billableitemcatalog.RequiresPrepayment(true),
			billableitemcatalog.IsActive(true),
		).
		Exist(ctx)
	if err != nil {
		s.log.Warn("lab: requires_prepayment lookup failed, failing open", zap.Error(err))
		return false
	}
	return exists
}

// CreateOrderRequest is the input to CreateOrder.
type CreateOrderRequest struct {
	VisitID       uuid.UUID
	ExaminationID *uuid.UUID
	OrderedBy     uuid.UUID
	TestCodes     []string
	Notes         string
}

// CreateOrder creates a LabOrder with one line per requested test, posting a BillableCharge for
// each line. The order starts "awaiting_payment" if the tenant's lab billing policy requires
// prepayment, else "requested" (immediately visible on the worklist).
func (s *Service) CreateOrder(ctx context.Context, tenantID uuid.UUID, req CreateOrderRequest) (*ent.LabOrder, error) {
	if req.VisitID == uuid.Nil || len(req.TestCodes) == 0 {
		return nil, fmt.Errorf("lab: visit_id and at least one test_code are required")
	}
	visit, err := s.client.PatientVisit.Query().
		Where(patientvisit.ID(req.VisitID), patientvisit.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("lab: visit not found: %w", err)
	}

	tests := make([]*catalogTest, 0, len(req.TestCodes))
	for _, code := range req.TestCodes {
		t, err := s.resolveTest(ctx, tenantID, code)
		if err != nil {
			return nil, err
		}
		tests = append(tests, t)
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("lab: begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	status := laborder.StatusRequested
	if s.requiresPrepayment(ctx, tenantID) {
		status = laborder.StatusAwaitingPayment
	}

	orderCreate := tx.LabOrder.Create().
		SetTenantID(tenantID).
		SetVisitID(req.VisitID).
		SetOrderedBy(req.OrderedBy).
		SetStatus(status).
		SetNotes(req.Notes)
	if req.ExaminationID != nil {
		orderCreate = orderCreate.SetExaminationID(*req.ExaminationID)
	}
	order, err := orderCreate.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("lab: create order: %w", err)
	}

	for _, t := range tests {
		line, lerr := tx.LabOrderLine.Create().
			SetTenantID(tenantID).
			SetLabOrderID(order.ID).
			SetTestCode(t.Code).
			SetTestName(t.Name).
			SetPrice(t.Price).
			SetSpecimenType(t.SpecimenType).
			Save(ctx)
		if lerr != nil {
			err = lerr
			return nil, fmt.Errorf("lab: create order line: %w", err)
		}
		if t.Price > 0 {
			if _, cerr := s.billing.PostCharge(ctx, tx, tenantID, billing.PostChargeRequest{
				PatientID:     visit.PatientID,
				VisitID:       req.VisitID,
				SourceModule:  "lab",
				SourceRefID:   &line.ID,
				Description:   "Lab test: " + t.Name,
				Amount:        t.Price,
				CreatedByUser: req.OrderedBy,
			}); cerr != nil {
				err = cerr
				return nil, fmt.Errorf("lab: post charge for %s: %w", t.Code, cerr)
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("lab: commit order: %w", err)
	}
	return order, nil
}

// GetOrder fetches a lab order with its lines, tenant-scoped.
func (s *Service) GetOrder(ctx context.Context, tenantID, orderID uuid.UUID) (*ent.LabOrder, []*ent.LabOrderLine, error) {
	order, err := s.client.LabOrder.Query().
		Where(laborder.ID(orderID), laborder.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("lab: order not found: %w", err)
	}
	lines, err := s.client.LabOrderLine.Query().
		Where(laborderline.TenantID(tenantID), laborderline.LabOrderID(orderID)).
		All(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("lab: list order lines: %w", err)
	}
	return order, lines, nil
}

// ListWorklist returns lab orders for a tenant, optionally filtered by status.
func (s *Service) ListWorklist(ctx context.Context, tenantID uuid.UUID, status string) ([]*ent.LabOrder, error) {
	q := s.client.LabOrder.Query().Where(laborder.TenantID(tenantID))
	if status != "" {
		q = q.Where(laborder.StatusEQ(laborder.Status(status)))
	} else {
		q = q.Where(laborder.StatusNEQ(laborder.StatusCancelled))
	}
	return q.Order(ent.Asc(laborder.FieldOrderedAt)).Limit(200).All(ctx)
}

// ActivateIfPaid flips an awaiting_payment order to "requested" once every one of its lines'
// charges is paid — re-checks live charge status each call rather than trusting the caller, so
// a premature call gets a clear rejection instead of silently activating unpaid work.
func (s *Service) ActivateIfPaid(ctx context.Context, tenantID, orderID uuid.UUID) (*ent.LabOrder, error) {
	order, lines, err := s.GetOrder(ctx, tenantID, orderID)
	if err != nil {
		return nil, err
	}
	if order.Status != laborder.StatusAwaitingPayment {
		return order, nil
	}
	for _, line := range lines {
		if line.Price <= 0 {
			continue
		}
		charge, cerr := s.chargeForLine(ctx, tenantID, line.ID)
		if cerr != nil || charge == nil {
			return nil, fmt.Errorf("lab: no charge found for line %s", line.TestCode)
		}
		// exempted = an insurance claim covered this charge in full (see
		// billing.Service.SubmitInsuranceClaim) — an equally valid "settled" outcome to paid.
		if charge.Status != billablecharge.StatusPaid && charge.Status != billablecharge.StatusExempted {
			return nil, fmt.Errorf("lab: test %s must be paid for (cash or insurance) before it can be run", line.TestName)
		}
	}
	return s.client.LabOrder.UpdateOneID(orderID).SetStatus(laborder.StatusRequested).Save(ctx)
}

func (s *Service) chargeForLine(ctx context.Context, tenantID, lineID uuid.UUID) (*ent.BillableCharge, error) {
	return s.client.BillableCharge.Query().
		Where(billablecharge.TenantID(tenantID), billablecharge.SourceReferenceID(lineID)).
		Only(ctx)
}

// SubmitInsuranceClaimRequest is the input to SubmitInsuranceClaim.
type SubmitInsuranceClaimRequest struct {
	ProviderID uuid.UUID
	CoverageID *uuid.UUID
	OutletID   *uuid.UUID
}

// SubmitInsuranceClaim is the insurance-path alternative to the cash CollectCharge +
// ActivateIfPaid flow for an awaiting_payment order: if the patient has a payer, the ordering
// clinician/records staff can submit an insurance claim covering this order's test charges
// instead of collecting cash. On acceptance the charges are marked exempted (via
// billing.Service.SubmitInsuranceClaim) and the order is activated in the same call — mirroring
// how the existing cash path is CollectCharge (billing) followed by a separate
// POST .../activate. A non-accepted claim (async payer adjudication) leaves the order
// awaiting_payment; the caller polls GET .../insurance/claims/{claimID}/status and resubmits once
// approved (see billing.Service.PollInsuranceClaim's doc comment for why resubmission, not a
// stored claim id, is the retry mechanism).
func (s *Service) SubmitInsuranceClaim(ctx context.Context, tenantID, orderID uuid.UUID, req SubmitInsuranceClaimRequest) (*ent.LabOrder, *billing.InsuranceClaimResult, error) {
	order, lines, err := s.GetOrder(ctx, tenantID, orderID)
	if err != nil {
		return nil, nil, err
	}
	visit, err := s.client.PatientVisit.Query().
		Where(patientvisit.ID(order.VisitID), patientvisit.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("lab: visit not found: %w", err)
	}
	acct, _, err := s.billing.GetAccountByVisit(ctx, tenantID, visit.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("lab: account not found: %w", err)
	}

	chargeIDs := make([]uuid.UUID, 0, len(lines))
	for _, line := range lines {
		if line.Price <= 0 {
			continue
		}
		if charge, cerr := s.chargeForLine(ctx, tenantID, line.ID); cerr == nil && charge != nil &&
			charge.Status == billablecharge.StatusPending {
			chargeIDs = append(chargeIDs, charge.ID)
		}
	}
	if len(chargeIDs) == 0 {
		return order, nil, fmt.Errorf("lab: no pending charges to claim for this order")
	}

	result, err := s.billing.SubmitInsuranceClaim(ctx, tenantID, acct.ID, billing.SubmitInsuranceClaimRequest{
		ProviderID: req.ProviderID,
		CoverageID: req.CoverageID,
		OutletID:   req.OutletID,
		OrderID:    &orderID,
		ChargeIDs:  chargeIDs,
	})
	if err != nil {
		return order, nil, err
	}
	if !result.Accepted {
		return order, result, nil
	}
	activated, aerr := s.ActivateIfPaid(ctx, tenantID, orderID)
	if aerr != nil {
		return order, result, aerr
	}
	return activated, result, nil
}

// EnterResultRequest is the input to EnterResult.
type EnterResultRequest struct {
	ResultValue    string
	Unit           string
	ReferenceRange string
	Flag           string // normal|abnormal|critical
	Notes          string
	ResultedBy     uuid.UUID
}

// EnterResult records a result for one line. When every line in the order has a result, the
// order is marked "resulted" and a hospital.lab_order.resulted event is published.
func (s *Service) EnterResult(ctx context.Context, tenantID, lineID uuid.UUID, req EnterResultRequest) (*ent.LabOrderLine, error) {
	line, err := s.client.LabOrderLine.Query().
		Where(laborderline.ID(lineID), laborderline.TenantID(tenantID)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("lab: order line not found: %w", err)
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("lab: begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	flag := laborderline.FlagNormal
	if v := laborderline.Flag(req.Flag); v != "" {
		flag = v
	}
	now := time.Now()
	updated, err := tx.LabOrderLine.UpdateOneID(lineID).
		SetResultValue(req.ResultValue).
		SetUnit(req.Unit).
		SetReferenceRange(req.ReferenceRange).
		SetFlag(flag).
		SetNotes(req.Notes).
		SetResultedBy(req.ResultedBy).
		SetResultedAt(now).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("lab: enter result: %w", err)
	}

	allLines, err := tx.LabOrderLine.Query().
		Where(laborderline.TenantID(tenantID), laborderline.LabOrderID(line.LabOrderID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("lab: list order lines: %w", err)
	}
	allResulted := true
	for _, l := range allLines {
		if l.ID == lineID {
			continue
		}
		if l.ResultedAt == nil {
			allResulted = false
			break
		}
	}
	if allResulted {
		if _, uerr := tx.LabOrder.UpdateOneID(line.LabOrderID).
			SetStatus(laborder.StatusResulted).
			SetCompletedAt(now).
			Save(ctx); uerr != nil {
			return nil, fmt.Errorf("lab: mark order resulted: %w", uerr)
		}
		if pubErr := events.Publish(ctx, tx.OutboxEvent, tenantID, line.LabOrderID.String(), events.EventLabOrderResulted, map[string]any{
			"lab_order_id": line.LabOrderID.String(),
		}); pubErr != nil {
			s.log.Warn("publish lab_order.resulted failed", zap.Error(pubErr))
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("lab: commit result: %w", err)
	}
	return updated, nil
}

// CatalogTest is the merged global+tenant lab test catalogue row returned to callers.
type CatalogTest struct {
	Code         string  `json:"code"`
	Name         string  `json:"name"`
	SpecimenType string  `json:"specimen_type,omitempty"`
	Price        float64 `json:"price"`
	IsGlobal     bool    `json:"is_global"`
}

// ListCatalog returns the global default lab-test catalogue UNIONed with the tenant's own
// custom entries.
func (s *Service) ListCatalog(ctx context.Context, tenantID uuid.UUID) ([]CatalogTest, error) {
	defaults, err := s.client.LabTestCatalogDefault.Query().
		Where(labtestcatalogdefault.IsActive(true)).
		Order(ent.Asc(labtestcatalogdefault.FieldName)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("lab: list default catalogue: %w", err)
	}
	entries, err := s.client.LabTestCatalogEntry.Query().
		Where(labtestcatalogentry.TenantID(tenantID), labtestcatalogentry.IsActive(true)).
		Order(ent.Asc(labtestcatalogentry.FieldName)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("lab: list tenant catalogue: %w", err)
	}
	out := make([]CatalogTest, 0, len(defaults)+len(entries))
	for _, d := range defaults {
		out = append(out, CatalogTest{Code: d.Code, Name: d.Name, SpecimenType: d.SpecimenType, Price: d.Price, IsGlobal: true})
	}
	for _, e := range entries {
		out = append(out, CatalogTest{Code: e.Code, Name: e.Name, SpecimenType: e.SpecimenType, Price: e.Price, IsGlobal: false})
	}
	return out, nil
}
