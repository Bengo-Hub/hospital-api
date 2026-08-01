package subscriptions

// Feature codes — the backend twin of hospital-ui's feature catalog. Every value MUST be a
// real code seeded by subscriptions-api (cmd/seed/plans_hospital.go, feature_catalog.go).
// Used with RequireFeature() to gate route groups.
const (
	// Tier 1 (AFYA_CLINIC)
	FeaturePatientRecords   = "patient_records"
	FeatureReceptionQueuing = "reception_queuing"
	FeatureConsultation     = "consultation"
	FeatureTriage           = "triage"
	FeaturePharmacyDispense = "pharmacy_dispensing"
	FeatureBilling          = "billing"
	FeatureLabRequestsBasic = "lab_requests_basic"

	// Tier 2 (AFYA_FACILITY, on top of Tier 1)
	FeatureInHouseLab             = "in_house_lab"
	FeatureInpatientModule        = "inpatient_module"
	FeatureControlledSubstanceReg = "controlled_substance_register"
	FeatureMultiCashier           = "multi_cashier"
	FeatureMultiDepartment        = "multi_department"
	FeatureDiagnosisLabCatalogues = "diagnosis_lab_catalogues"
	FeatureDischargeSummaries     = "discharge_summaries"
	FeatureInsuranceClaims        = "insurance_claims"
	FeatureInvoiceGeneration      = "invoice_generation"
	FeatureCreditNotes            = "credit_notes"
	FeatureVendorManagement       = "vendor_management"
	FeatureLedgerPosting          = "ledger_posting"
	FeatureTreasuryApprovals      = "treasury_approvals"

	// Tier 3 (AFYA_HOSPITAL, on top of Tier 1+2)
	FeatureTheatreModule         = "theatre_module"
	FeatureMaternityModule       = "maternity_module"
	FeatureMorgueModule          = "morgue_module"
	FeatureSpecializedProgrammes = "specialized_programmes"
	FeatureMultiBranch           = "multi_branch"
	FeatureAdvancedAnalytics     = "advanced_analytics"
	FeatureAPIAccess             = "api_access"
	FeaturePrioritySupport       = "priority_support"
	FeatureTaifaCareOnboarding   = "taifa_care_dedicated_onboarding"
	FeatureKHISDHIS2Reporting    = "khis_dhis2_reporting"
	FeatureHRManagement          = "hr_management"
	FeaturePayroll               = "payroll"
	FeatureLeaveManagement       = "leave_management"
	FeatureAttendance            = "attendance"
	FeatureAmbulanceDispatch     = "ambulance_dispatch"
	FeatureSmartTaxCompliance    = "smart_tax_compliance"
	FeatureReconciliation        = "reconciliation"
	FeatureAuditTrail            = "audit_trail"
	FeatureStockTake             = "stock_take"
	FeatureLowStockAlerts        = "low_stock_alerts"
	FeatureStockAlerts           = "stock_alerts"
	FeatureRFQs                  = "rfqs"
	FeatureProcurementContracts  = "procurement_contracts"

	// Base codes present at every tier
	FeatureStockTracking        = "stock_tracking"
	FeatureLotsBatches          = "lots_batches"
	FeatureBatchExpiryTracking  = "batch_expiry_tracking"
	FeatureExpiryAlerts         = "expiry_alerts"
	FeaturePurchaseOrders       = "purchase_orders"
	FeatureBasicReports         = "basic_reports"
	FeatureBasicInventoryAccess = "basic_inventory_access"
	FeatureWalletManagement     = "wallet_management"
	FeaturePaymentCollection    = "payment_collection"
	FeaturePaymentLinks         = "payment_links"
	FeatureTransactionReports   = "transaction_reports"
	FeatureCustomerManagement   = "customer_management"
	FeatureTaxCodes             = "tax_codes"
	FeatureETIMSIntegration     = "etims_integration"
	FeatureBasicTreasuryAccess  = "basic_treasury_access"
)
