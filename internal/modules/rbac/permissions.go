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
)

// WildcardPermission grants every permission when held by a role. Expanded into every
// concrete permission code at seed time (see seed.go's resolvePermissions) — the admin
// role declares it as shorthand rather than enumerating every code by hand.
const WildcardPermission = "*"
