package rbac

// Permission codes follow the hospital.{module}.{action} pattern.
// These match what's seeded in seed.go and stored in hospital_permissions.
const (
	// consultation (outpatient consult, diagnosis notes, vitals)
	PermConsultationView   = "hospital.consultation.view"
	PermConsultationAdd    = "hospital.consultation.add"
	PermConsultationChange = "hospital.consultation.change"
	PermConsultationManage = "hospital.consultation.manage"

	// triage
	PermTriageView   = "hospital.triage.view"
	PermTriageAdd    = "hospital.triage.add"
	PermTriageChange = "hospital.triage.change"
	PermTriageManage = "hospital.triage.manage"

	// lab (lab requests + results)
	PermLabView   = "hospital.lab.view"
	PermLabAdd    = "hospital.lab.add"
	PermLabChange = "hospital.lab.change"
	PermLabManage = "hospital.lab.manage"

	// pharmacy (prescriptions + dispensing)
	PermPharmacyView      = "hospital.pharmacy.view"
	PermPharmacyPrescribe = "hospital.pharmacy.prescribe"
	PermPharmacyDispense  = "hospital.pharmacy.dispense"
	PermPharmacyManage    = "hospital.pharmacy.manage"

	// billing
	PermBillingView   = "hospital.billing.view"
	PermBillingAdd    = "hospital.billing.add"
	PermBillingChange = "hospital.billing.change"
	PermBillingManage = "hospital.billing.manage"
	// collect_own = a department may collect payment for a charge IT created (see
	// docs/architecture.md "Distributed Billing & Patient Accounts"); collect_any = the Billing
	// desk's universal fallback across every department/patient; override_settlement = release a
	// patient/body with an outstanding balance (audited escape hatch, requires a reason).
	PermBillingCollectOwn         = "hospital.billing.collect_own"
	PermBillingCollectAny         = "hospital.billing.collect_any"
	PermBillingOverrideSettlement = "hospital.billing.override_settlement"
	// manage_catalog = create/edit/deactivate BillableItemCatalog rows (the tenant-configured
	// price list a facility is seeded with at provisioning time — see
	// refdata.SeedFacilityBillableItems). Separate from PermBillingManage (general billing
	// records) since catalog/pricing configuration is a narrower, more sensitive admin action.
	PermBillingManageCatalog = "hospital.billing.manage_catalog"

	// inpatient (admissions, wards, discharge)
	PermInpatientView   = "hospital.inpatient.view"
	PermInpatientAdd    = "hospital.inpatient.add"
	PermInpatientChange = "hospital.inpatient.change"
	PermInpatientManage = "hospital.inpatient.manage"

	// records (patient records + reception/registration)
	PermRecordsView   = "hospital.records.view"
	PermRecordsAdd    = "hospital.records.add"
	PermRecordsChange = "hospital.records.change"
	PermRecordsManage = "hospital.records.manage"

	// reception (front-desk queuing/scheduling)
	PermReceptionView   = "hospital.reception.view"
	PermReceptionAdd    = "hospital.reception.add"
	PermReceptionChange = "hospital.reception.change"
	PermReceptionManage = "hospital.reception.manage"

	// theatre (surgical scheduling)
	PermTheatreView   = "hospital.theatre.view"
	PermTheatreAdd    = "hospital.theatre.add"
	PermTheatreChange = "hospital.theatre.change"
	PermTheatreManage = "hospital.theatre.manage"

	// users (tenant staff/role management)
	PermUsersView   = "hospital.users.view"
	PermUsersManage = "hospital.users.manage"

	// config (tenant hospital settings)
	PermConfigView   = "hospital.config.view"
	PermConfigManage = "hospital.config.manage"
)

// Role codes seeded platform-wide (global — see HospitalRole schema doc).
const (
	RoleAdmin        = "admin"
	RoleDoctor       = "doctor"
	RoleNurse        = "nurse"
	RolePharmacist   = "pharmacist"
	RoleRecordsClerk = "records_clerk"
	RoleManager      = "manager"
	// RoleLabTech (2026-09-02) closes a real out-of-the-box gap: no seeded role except Admin's
	// wildcard held PermLabChange, so nobody could enter lab results or activate a paid lab order
	// without an admin first customizing a role. See docs/architecture.md's role list.
	RoleLabTech = "lab_technician"
	// RoleCashier is the Billing desk — the universal fallback collection point for any
	// department that doesn't (or can't) collect its own charges directly. See
	// docs/architecture.md "Distributed Billing & Patient Accounts".
	RoleCashier = "cashier"
)

// WildcardPermission grants every permission when held by a role. Expanded into every
// concrete permission code at seed time (see seed.go's resolvePermissions) — the admin
// role declares it as shorthand rather than enumerating every code by hand.
const WildcardPermission = "*"
