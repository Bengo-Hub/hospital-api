package rbac

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// defaultPermissions is the canonical set of hospital RBAC permission codes.
var defaultPermissions = []HospitalPermission{
	{PermissionCode: PermConsultationView, Name: "View Consultations", Module: "consultation", Action: "view"},
	{PermissionCode: PermConsultationAdd, Name: "Add Consultations", Module: "consultation", Action: "add"},
	{PermissionCode: PermConsultationChange, Name: "Edit Consultations", Module: "consultation", Action: "change"},
	{PermissionCode: PermConsultationManage, Name: "Manage Consultations", Module: "consultation", Action: "manage"},

	{PermissionCode: PermTriageView, Name: "View Triage", Module: "triage", Action: "view"},
	{PermissionCode: PermTriageAdd, Name: "Add Triage Records", Module: "triage", Action: "add"},
	{PermissionCode: PermTriageChange, Name: "Edit Triage Records", Module: "triage", Action: "change"},
	{PermissionCode: PermTriageManage, Name: "Manage Triage", Module: "triage", Action: "manage"},

	{PermissionCode: PermLabView, Name: "View Lab Requests/Results", Module: "lab", Action: "view"},
	{PermissionCode: PermLabAdd, Name: "Add Lab Requests", Module: "lab", Action: "add"},
	{PermissionCode: PermLabChange, Name: "Edit Lab Requests/Results", Module: "lab", Action: "change"},
	{PermissionCode: PermLabManage, Name: "Manage Lab", Module: "lab", Action: "manage"},

	{PermissionCode: PermPharmacyView, Name: "View Prescriptions/Dispensing", Module: "pharmacy", Action: "view"},
	{PermissionCode: PermPharmacyPrescribe, Name: "Prescribe Medication", Module: "pharmacy", Action: "prescribe"},
	{PermissionCode: PermPharmacyDispense, Name: "Dispense Medication", Module: "pharmacy", Action: "dispense"},
	{PermissionCode: PermPharmacyManage, Name: "Manage Pharmacy", Module: "pharmacy", Action: "manage"},

	{PermissionCode: PermBillingView, Name: "View Billing", Module: "billing", Action: "view"},
	{PermissionCode: PermBillingAdd, Name: "Add Billing Records", Module: "billing", Action: "add"},
	{PermissionCode: PermBillingChange, Name: "Edit Billing Records", Module: "billing", Action: "change"},
	{PermissionCode: PermBillingManage, Name: "Manage Billing", Module: "billing", Action: "manage"},
	{PermissionCode: PermBillingCollectOwn, Name: "Collect Own Charges", Module: "billing", Action: "collect_own"},
	{PermissionCode: PermBillingCollectAny, Name: "Collect Any Charge (Cashier)", Module: "billing", Action: "collect_any"},
	{PermissionCode: PermBillingOverrideSettlement, Name: "Override Settlement Requirement", Module: "billing", Action: "override_settlement"},
	{PermissionCode: PermBillingManageCatalog, Name: "Manage Billable Item Catalog", Module: "billing", Action: "manage_catalog"},

	{PermissionCode: PermInpatientView, Name: "View Inpatient/Wards", Module: "inpatient", Action: "view"},
	{PermissionCode: PermInpatientAdd, Name: "Admit Patients", Module: "inpatient", Action: "add"},
	{PermissionCode: PermInpatientChange, Name: "Edit Inpatient Records", Module: "inpatient", Action: "change"},
	{PermissionCode: PermInpatientManage, Name: "Manage Inpatient/Discharge", Module: "inpatient", Action: "manage"},

	{PermissionCode: PermRecordsView, Name: "View Patient Records", Module: "records", Action: "view"},
	{PermissionCode: PermRecordsAdd, Name: "Add Patient Records", Module: "records", Action: "add"},
	{PermissionCode: PermRecordsChange, Name: "Edit Patient Records", Module: "records", Action: "change"},
	{PermissionCode: PermRecordsManage, Name: "Manage Patient Records", Module: "records", Action: "manage"},

	{PermissionCode: PermReceptionView, Name: "View Reception Queue", Module: "reception", Action: "view"},
	{PermissionCode: PermReceptionAdd, Name: "Add Reception/Registration", Module: "reception", Action: "add"},
	{PermissionCode: PermReceptionChange, Name: "Edit Reception/Registration", Module: "reception", Action: "change"},
	{PermissionCode: PermReceptionManage, Name: "Manage Reception", Module: "reception", Action: "manage"},

	{PermissionCode: PermTheatreView, Name: "View Theatre Schedule", Module: "theatre", Action: "view"},
	{PermissionCode: PermTheatreAdd, Name: "Add Theatre Bookings", Module: "theatre", Action: "add"},
	{PermissionCode: PermTheatreChange, Name: "Edit Theatre Bookings", Module: "theatre", Action: "change"},
	{PermissionCode: PermTheatreManage, Name: "Manage Theatre", Module: "theatre", Action: "manage"},

	{PermissionCode: PermUsersView, Name: "View Users", Module: "users", Action: "view"},
	{PermissionCode: PermUsersManage, Name: "Manage Users", Module: "users", Action: "manage"},

	{PermissionCode: PermConfigView, Name: "View Config", Module: "config", Action: "view"},
	{PermissionCode: PermConfigManage, Name: "Manage Config", Module: "config", Action: "manage"},
}

type roleDefinition struct {
	Code        string
	Name        string
	Description string
	// Permissions may include exact codes ("hospital.lab.view"), module wildcards
	// ("hospital.pharmacy.*"), or the global wildcard ("*") — resolved by resolvePermissions.
	Permissions []string
}

var defaultRoles = []roleDefinition{
	{
		Code:        RoleAdmin,
		Name:        "Admin",
		Description: "Full access to all hospital functions",
		Permissions: []string{WildcardPermission},
	},
	{
		Code:        RoleDoctor,
		Name:        "Doctor",
		Description: "Consultations, diagnosis, prescribing, and lab requests",
		Permissions: []string{
			"hospital.consultation.*",
			PermTriageView,
			PermLabView, PermLabAdd,
			PermPharmacyView, PermPharmacyPrescribe,
			PermRecordsView,
			PermInpatientView, PermInpatientChange,
			PermBillingCollectOwn,
		},
	},
	{
		Code:        RoleNurse,
		Name:        "Nurse",
		Description: "Triage and inpatient/ward care",
		Permissions: []string{
			"hospital.triage.*",
			PermInpatientView, PermInpatientChange,
			PermConsultationView,
			PermRecordsView,
			PermBillingCollectOwn,
		},
	},
	{
		Code:        RolePharmacist,
		Name:        "Pharmacist",
		Description: "Dispensing and pharmacy stock/controlled substance handling",
		Permissions: []string{
			"hospital.pharmacy.*",
			PermBillingView,
			PermBillingCollectOwn,
			// Read-only patient lookup — a clinical/hospital-tier pharmacist dispensing against a
			// real prescription needs to be able to check the patient's chart (allergies, other
			// current medications) before handing drugs over, standard real-world pharmacy
			// practice. Deliberately view-only: add/change/manage stay reserved for records staff.
			// At Chemist tier this permission is inert — 'patients' isn't in CHEMIST_MODULES
			// (facility-nomenclature.ts), so the Patients nav item stays hidden there regardless;
			// this only actually surfaces for a Clinic/Facility/Hospital-tier pharmacist. Found
			// live 2026-09-02: a hospital pharmacist's sidebar was identical to a chemist's.
			PermRecordsView,
		},
	},
	{
		Code:        RoleLabTech,
		Name:        "Lab Technician",
		Description: "Enter and edit lab results, activate paid lab orders",
		Permissions: []string{
			PermLabView, PermLabAdd, PermLabChange,
		},
	},
	{
		Code:        RoleRecordsClerk,
		Name:        "Records Clerk",
		Description: "Patient records, registration, and reception queuing",
		Permissions: []string{
			"hospital.records.*",
			"hospital.reception.*",
			PermBillingView, PermBillingAdd,
			PermBillingCollectOwn,
		},
	},
	{
		Code:        RoleCashier,
		Name:        "Cashier",
		Description: "Billing desk — the universal fallback payment-collection point across every department/patient",
		Permissions: []string{
			PermBillingView, PermBillingAdd, PermBillingChange,
			PermBillingCollectAny,
			PermBillingOverrideSettlement,
			PermRecordsView,
		},
	},
	{
		Code:        RoleManager,
		Name:        "Manager",
		Description: "Broad view/manage access across clinical and billing modules; excludes admin-only settings (users/config)",
		Permissions: []string{
			PermConsultationView,
			PermTriageView,
			PermLabView,
			PermPharmacyView,
			"hospital.billing.*",
			"hospital.reception.*",
			PermRecordsView, PermRecordsChange,
			PermInpatientView, PermInpatientChange,
			PermTheatreView,
			PermUsersView,
			PermConfigView,
		},
	},
}

// resolvePermissions expands wildcard patterns ("*", "hospital.pharmacy.*") into concrete
// permission IDs from the seeded catalog.
func resolvePermissions(patterns []string, permByCode map[string]uuid.UUID) []uuid.UUID {
	ids := make(map[uuid.UUID]bool)
	for _, pattern := range patterns {
		if pattern == WildcardPermission {
			for _, id := range permByCode {
				ids[id] = true
			}
			continue
		}
		if len(pattern) > 2 && pattern[len(pattern)-1] == '*' {
			prefix := pattern[:len(pattern)-1]
			for code, id := range permByCode {
				if strings.HasPrefix(code, prefix) {
					ids[id] = true
				}
			}
			continue
		}
		if id, ok := permByCode[pattern]; ok {
			ids[id] = true
		}
	}
	result := make([]uuid.UUID, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	return result
}

// SeedRoles creates the global permission catalog and default roles if they don't already
// exist. Fully idempotent — safe to call on every JIT provisioning / service startup.
// Roles are GLOBAL (no tenant scoping), so this seeds the catalog exactly once platform-wide.
func (s *Service) SeedRoles(ctx context.Context) error {
	// No fast-path early return here on purpose: a prior "if admin role exists, return nil"
	// check meant any permission code added to defaultPermissions AFTER the catalog's first
	// seed (e.g. the 2026-08-29 billing collect_own/collect_any/override_settlement codes)
	// would silently never get created in an already-provisioned environment, including prod.
	// Steps 1/2 below are each individually idempotent (existence-checked per item), so running
	// them every time is safe and correctly incremental.

	// Step 1: ensure every permission exists.
	permByCode := make(map[string]uuid.UUID, len(defaultPermissions))
	for _, def := range defaultPermissions {
		existing, err := s.repo.GetPermissionByCode(ctx, def.PermissionCode)
		if err == nil && existing != nil {
			permByCode[def.PermissionCode] = existing.ID
			continue
		}
		perm := &HospitalPermission{
			ID:             uuid.New(),
			PermissionCode: def.PermissionCode,
			Name:           def.Name,
			Module:         def.Module,
			Action:         def.Action,
		}
		if createErr := s.repo.CreatePermission(ctx, perm); createErr != nil {
			// Race: another request may have just created it.
			if existing2, err2 := s.repo.GetPermissionByCode(ctx, def.PermissionCode); err2 == nil && existing2 != nil {
				permByCode[def.PermissionCode] = existing2.ID
				continue
			}
			return fmt.Errorf("seed: create permission %q: %w", def.PermissionCode, createErr)
		}
		permByCode[def.PermissionCode] = perm.ID
	}

	// Step 2: create each role and assign its resolved permissions.
	for _, roleDef := range defaultRoles {
		role, err := s.repo.GetGlobalRoleByCode(ctx, roleDef.Code)
		if err != nil {
			desc := roleDef.Description
			role = &HospitalRole{
				ID:           uuid.New(),
				RoleCode:     roleDef.Code,
				Name:         roleDef.Name,
				Description:  &desc,
				IsSystemRole: true,
			}
			if createErr := s.repo.CreateRole(ctx, role); createErr != nil {
				if existing2, err2 := s.repo.GetGlobalRoleByCode(ctx, roleDef.Code); err2 == nil && existing2 != nil {
					role = existing2
				} else {
					s.logger.Warn("seed: create role failed", zap.String("role", roleDef.Code), zap.Error(createErr))
					continue
				}
			}
		}

		existingPerms, _ := s.repo.GetRolePermissions(ctx, role.ID)
		existingSet := make(map[string]struct{}, len(existingPerms))
		for _, ep := range existingPerms {
			existingSet[ep.PermissionCode] = struct{}{}
		}

		for _, permID := range resolvePermissions(roleDef.Permissions, permByCode) {
			// Skip permissions already attached to this role.
			already := false
			for code, id := range permByCode {
				if id == permID {
					if _, ok := existingSet[code]; ok {
						already = true
					}
					break
				}
			}
			if already {
				continue
			}
			if assignErr := s.repo.AssignPermissionToRole(ctx, role.ID, permID); assignErr != nil {
				s.logger.Warn("seed: assign permission failed",
					zap.String("role", roleDef.Code), zap.Error(assignErr))
			}
		}
	}

	s.logger.Info("seeded global hospital RBAC roles/permissions")
	return nil
}
