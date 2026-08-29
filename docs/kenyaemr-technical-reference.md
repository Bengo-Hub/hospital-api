# KenyaEMR / OpenMRS — Technical Architecture Reference

**Last updated:** 2026-08-29, based on a dedicated technical audit (GitHub repository inspection,
README/source reads, and web research) of KenyaEMR's real codebase, not just its market position.
`docs/market-and-competitive-landscape.md` already covers KenyaEMR as a competitor; this document
goes one level deeper, into how it is actually built, so Codevertex Afya can borrow proven patterns
and spot real gaps rather than guessing. Sourcing discipline matches `docs/compliance-kenya.md`:
every non-obvious claim states whether it came from a file this research actually read, or is
inferred/unconfirmed.

---

## 1. Where the real code lives

KenyaEMR's GitHub footprint is split across three places, and the org a search engine points to
first is not where active development happens:

- **`palladiumkenya`** is the org of record (Palladium is a long-standing KenyaEMR implementing
  partner), but most of its architecturally central repositories (`openmrs-module-kenyaemr`,
  `openmrs-esm-patient-management`, `openmrs-module-commodity`, `openmrs-module-initializer`,
  `openmrs-esm-stock-management`) are archived forks, not live development.
- **Active development happens in personal contributor forks**: `Murithijoshua/kenyaemr-esm-3.x`
  (the O3 frontend monorepo), `njorocs/openmrs-config-kenyaemr` (the backend config bundle, actively
  updated as recently as November 2025), and `facilitywide/openmrs-module-insuranceclaims` (the
  claims module, pushed as recently as January 2026).
- **`Palladium-hub`** is a second, newer org (created February 2026) holding mostly infrastructure:
  a Terraform "Multi-Tenant Infrastructure as Code" repo, a releases repo, and — notably —
  `DRCEMR-Release`, confirming the same platform is white-labeled for the Democratic Republic of
  Congo. `njorocs/openmrs-config-kenyaemr`'s own build scripts (`dha_build.sh`, `ethipioa_build.sh`,
  `taifa_care_build.sh`) confirm one shared config codebase produces multiple country/brand builds.

There is no separate `kenyaemr`-named GitHub org, and no `kenyaemr` repository under the `openmrs`
org itself. Not independently confirmed: the true current repo footprint of `facilitywide` and
`Palladium-hub` beyond their top-level repo listings.

---

## 2. Module architecture: a generational shift, both generations still live

KenyaEMR has gone through two architectures, and Kenyan facilities run a mix of both today:

- **Legacy (1.x/2.x)**: a single monolithic Java OpenMRS module (`.omod`), distributed as a
  versioned `.zip` (e.g. `kenyaemr-13.3-distro.zip`), built with Maven. The module's own README is
  minimal and gives no real module manifest.
- **Current (3.x, "O3")**: OpenMRS's newer microfrontend architecture. The backend is
  **config-only** (`openmrs-config-kenyaemr`), loaded by the community **Initializer** module and
  the **Address Hierarchy** module — Kenya-specific behavior is expressed as configuration
  (concept sources, form definitions, payment modes, queues/queue-rooms, order frequencies/types,
  dispositions), not custom Java code. Confirmed config domains: `addresshierarchy`, `ampathforms`
  (the AMPATH form-rendering engine, used for structured clinical forms), `conceptsources`,
  `fhirconceptsources`, `ocl` (Open Concept Lab, a shared terminology-dictionary service), and
  `metadatasharing`.
- The O3 frontend is a monorepo of independently-versioned packages, confirmed to include:
  `esm-billing-app`, `esm-pharmacy-app`, `esm-lab-manifest-app`, `esm-morgue-app`,
  `esm-care-panel-app`, `esm-patient-flags-app`, `esm-providers-app`, `esm-shr-app`, and
  `esm-version-app`. Ward and order-entry live in separately-forked O3 packages
  (`kenyaemr-esm-ward`, `kenyaemr-esm-orders`) under other contributor accounts.
- Kenya-specific patches to upstream OpenMRS packages are tracked as named branches, not forks with
  diverged history: a `feat/client-registry` branch of `openmrs/openmrs-esm-patient-management`, and
  a `KenyaEMR` branch of the core login package. This is a lighter-weight customization pattern than
  Codevertex Afya's own model (a dedicated Go microservice), reflecting OpenMRS's config-driven
  design philosophy rather than a difference worth copying.

---

## 3. Billing and claims — the most consequential finding

**KenyaEMR's production billing backend is the legacy OpenHMIS "Cashier" module**
(`/ws/rest/v1/cashier/*`), not the newer official community `openmrs-module-billing` successor.
Its real data model, read directly from the O3 frontend's TypeScript types:

- A **`PatientInvoice` ("Bill")** belongs to one `CashPoint` (a physical location) and one
  `Cashier` (a `Provider`). It carries `lineItems[]` and `payments[]`.
- Each **`LineItem`** references either an `Item` or a `BillableService`, with its own `quantity`,
  `price`, and — importantly — its **own `paymentStatus`**, independent of the bill's overall status.
- **`PaymentStatus`** values: `POSTED | PENDING | PAID | CREDITED | CANCELLED | ADJUSTED | EXEMPTED`.
  `EXEMPTED` (for insurance-covered items) and `ADJUSTED`/`CREDITED` (for corrections) are worth
  comparing against our own `BillableCharge.status` enum (`pending|invoiced|paid|waived|
  written_off`, see `docs/erd.md`) — `exempted` in particular names a real, distinct outcome
  (insurance covers the full cost) that our current `waived` status currently has to stand in for.
  Worth adding as a distinct status value when Sprint 5 is implemented, for audit clarity between
  "we chose not to charge" and "insurance paid it in full."

**This confirms KenyaEMR's billing architecture is cash-point/cashier-centric, not a distributed
ledger** — a bill is tied to one physical collection point and one cashier at posting time, the
opposite of Codevertex Afya's "any department can charge, Billing desk is the fallback" design (see
`docs/architecture.md` "Distributed Billing & Patient Accounts"). This is real, concrete validation
that the distributed-ledger model is a genuine architectural advance over the market's most-deployed
open-source system, not just a marketing claim. Caveat: this reflects the specific fork analyzed
(last pushed October 2024); whether a more recent production deployment has since layered a
distributed model on top was not confirmed.

**A real, actively-maintained insurance-claims module exists**: `openmrs/openmrs-module-insuranceclaims`,
kept current by the `facilitywide` fork (pushed January 2026). Its claim-submission flow builds a
claim from data already on the books, not a parallel re-entry:

- A claim is assembled by **selecting specific already-posted bill line-items**, plus:
  `claimCode`, `guaranteeId` (a pre-authorization/guarantee reference number), `claimExplanation`,
  `claimJustification` (free-text clinical justification), one or more `providerName`, `diagnoses[]`
  (pulled from the visit's own coded encounter diagnoses — not re-typed), `visitType`, `facility`,
  and a `treatmentStart`/`treatmentEnd` date range.

This is useful, concrete prior art: it validates keeping `BillableCharge` as the ledger's source of
truth and building an insurance claim as a reference-only selection over already-posted charges
(exactly what `docs/integrations.md` §2.2 and `sprint-5-billing-insurance.md` already assume), rather
than re-deriving billing data at claim-submission time.

A `benefits-package` (SHA/NHIF live benefit-balance) UI component exists in the analyzed fork but
was still a hardcoded placeholder, not wired to a real eligibility API — real-time benefit checking
was evidently not production-complete as of that snapshot. No evidence was found of any KenyaEMR
module directly implementing DHA's newer FHIR Claim Bundle spec (`docs/sha-taifacare-api-specs/`) —
plausible given the September 2026 DHA-certification deadline is still approaching, but not
confirmed either way.

---

## 4. Specialized programme coverage and the real MOH register mapping

KenyaEMR tracks more named clinical programmes than ANC/PNC/ART/TB/Immunization alone. Confirmed as
real, named modules or job-aids (via the official `kenyahmis.org` knowledge base, titles/snippets
only — the site's full pages sit behind a bot-check that blocked direct reads):

- **HTS** (HIV Testing Services) — a dedicated module, `openmrs-module-kenyaemrhivtesting`.
- **VMMC** (Voluntary Medical Male Circumcision) — a dedicated module for documentation/reporting.
- **OTZ** (Operation Triple Zero — adolescents living with HIV) — a dedicated job-aid, plus an
  adjacent **OVC** (Orphans and Vulnerable Children) knowledge-base category.
- **PrEP and PMTCT/EID** (HIV-Exposed Infant follow-up) — confirmed as first-class tracked event
  types in a real-time surveillance pipeline (Kafka topics `prep_events` and `hei_events`, with
  event subtypes like `prep_uptake`, `hei_at_6_to_8_weeks`, `hei_at_24_weeks` — the standard EID PCR
  testing schedule).
- **Cancer screening** — an "Enhanced Cancer Screening Module" covering **both cervical and
  prostate** cancer, broader than a cervical-only assumption.
- **Mental health** — a PHQ-9 depression score captured and persisted to the national data
  warehouse, evidently as a data element inside general consultation rather than a standalone module.
- **Not confirmed**: dedicated modules specifically for KVP (key/vulnerable populations),
  standalone family planning, nutrition, or COVID-19 — either folded into other forms, or simply not
  publicly documented. Treat their absence here as "not found," not "confirmed absent."

**Corrected MOH register/form mapping** (an earlier internal assumption had this wrong — the real
mapping, from NASCOP/MOH/NPHI sourcing):

| Register / Form | Actual subject |
|---|---|
| MOH 731 | Comprehensive HIV/AIDS (ART) report |
| **MOH 362** | HTS Lab, Referral & Linkage Register — **this is the real HIV-testing register**, not 705A/705B |
| MOH 705A | OPD summary, under-5s |
| MOH 705B | OPD morbidity summary, over-5s |
| MOH 711 | RMNCH (Reproductive, Maternal, Newborn & Child Health) summary |
| MOH 502 | IDSR case-based reporting for priority/notifiable diseases |

MOH 502 (IDSR) is a distinct register family from the ART/OPD/RMNCH ones above — see `docs/erd.md`
and `sprint-10-specialized-programmes-khis.md` for where this now threads into the programme/export
design.

---

## 5. Reporting and cohort architecture: three generations, still all live

1. **Ad-hoc / individual-level**: stock OpenMRS **Reporting** + **ReportingREST** modules feeding an
   O3 **Cohort Builder** app — inherited unmodified from OpenMRS core, not a KenyaEMR invention.
2. **Standard aggregate → national reporting**: a named mechanism, **AIR (Automated Indicator
   Reporting)** — generates electronic MOH aggregate reports directly from the EMR and submits them
   to DHIS2 using the ADX standard, "enforced through an Interoperability Layer (IL)." A published
   field test found AIR-submitted MOH-731 data was **100% complete and accurate**, versus **89%
   complete / 71% accurate** for manually entered data over the same period — a strong, citable
   evidence point for why hospital-api's own KHIS/ADX automation (Sprint 10) is worth building
   properly rather than treating as a checkbox. DATIM (PEPFAR's reporting system) is described as
   effectively "the PEPFAR-specific version of KHIS" — the same MOH-731 source feeds both.
3. **Batch de-identified ETL (older, HIV-specific)**: **DWAPI** (Data Warehouse API), a client tool
   installed at each facility that extracts from the local EMR database, runs de-duplication/quality
   checks locally, then pushes over SSL to a national staging area — with an explicit "no PII
   transmitted" design constraint. This feeds Kenya's **National Data Warehouse (NDW)**, which by
   end-2020 held longitudinal ART records for 1.93 million individuals across 1,516 facilities.
4. **Real-time event streaming (current)**: `case-surveillance-realtime`, a Kafka-based pipeline for
   HIV cascade events (PrEP uptake, HEI/PMTCT milestones, linkage).

Read together, this is a visible architectural progression — batch/de-identified ETL, to aggregate
ADX push, to real-time event streaming — for the same underlying HIV cascade data. It suggests
Codevertex Afya's own KHIS/ADX design (currently planned as a scheduled batch export, see
`docs/integrations.md` §2B) should not assume batch aggregate export is a permanent ceiling; the
market it is entering has already moved past it for its highest-priority programme data.

**Separately, TB does not go through this pipeline at all**: Kenya runs a dedicated, TB-only national
case-based surveillance system called **TIBU** (launched 2012, first in East Africa, run by MOH's
National TB, Leprosy and Lung Disease Program), with a lighter community-facing "TIBU lite" variant
in pilot. TB coordinators enter case-based data that aggregates into KHIS2 only quarterly. This means
a facility EMR's TB module has two distinct potential integration targets, not one: routine ADX
aggregate reporting (like every other programme) and, eventually, TIBU-shaped case-based export.

---

## 6. National HIE / Shared Health Record integration — real, current, and concrete

- **`compass-shr`**: a standalone Shared Health Record service built on **HAPI FHIR R4**, with an
  nginx gateway and JWT auth. Confirmed endpoints: `GET /v1/hie-auth?key=` (token), `POST
  /v1/shr-submission?resource=ServiceRequest`, and `GET /v1/shr-referrals?facility_code=` — the last
  one keyed explicitly by Kenya's **Master Facility List (MFL) code**, the same identifier
  Codevertex Afya already plans to store as tenant metadata (`docs/compliance-kenya.md` §6). It seeds
  itself from a CSV of facilities as FHIR `Organization` resources, functioning as a lightweight
  facility-directory/mediator even though it is not badged as an official OpenHIE mediator.
- The `feat/client-registry` branch of the patient-registration frontend, and `esm-providers-app`'s
  own description ("seamless workflows integrated with the Health Information Exchange system"),
  confirm active, current Client Registry integration work exists in the KenyaEMR ecosystem, not
  just on the government's side.
- No OpenHIE-branded mediator (e.g. an `openhim`-named repo) was found in this footprint —
  `compass-shr`'s gateway is the closest functional equivalent found.

---

## 7. Offline connectivity and patient identification

- **No KenyaEMR-specific offline-sync architecture was found.** Whatever offline behavior exists is
  most likely inherited from OpenMRS 3's generic service-worker/PWA caching, not a KenyaEMR-built
  capability. Not confirmed either way with certainty.
- **Biometric deduplication has been piloted, not confirmed shipped.** A peer-reviewed study
  (PMC8734934) documents the **mUzima Fingerprint Module**, integrating a DigitalPersona U.are.U
  4500 scanner and the NIST Bozorth3 matching algorithm into OpenMRS at a Western Kenya HIV clinic:
  89.3% sensitivity, 0% false-acceptance, 3.2-second record retrieval versus 9.5 seconds for
  demographic search, 96.3% patient acceptance. This is a research pilot, not confirmed as part of
  the current shipped KenyaEMR 3.x distro — no reference to SIMPRINTS or any other biometric vendor
  was found anywhere in the current GitHub footprint. Worth tracking as a future differentiator
  (duplicate-patient prevention is a real, named national problem, see `docs/compliance-kenya.md`),
  not a near-term commitment.

---

## 8. Lab and pharmacy/commodity interoperability

**Lab: a batch courier-manifest workflow, not live device integration.** The real `esm-lab-manifest-app`
source models Kenya's actual national reference-lab pattern — samples (frozen plasma, whole blood,
consistent with viral-load/EID dried-blood-spot referral testing) are batched into a manifest with
`sampleCollectionDate`, `sampleSeparationDate`, `dispatchDate`, courier name, and a phone-validated
handoff contact, then couriered to a centralized lab. The manifest has its own lifecycle: `Draft →
Ready to send → Sending → Submitted → Incomplete/Complete`. This is a materially different pattern
from "HL7/ASTM device integration," and probably a more realistic model for Kenya's referred-out
national testing (viral load, EID, TB) than assuming every facility has an in-house analyzer talking
HL7. Worth mirroring this manifest/batch-referral shape for hospital-api's own referred-out lab
workflow (`sprint-3-laboratory.md`), as a pattern that sits alongside, not replaces, live in-house
result entry. **OpenELIS Global** is the standard open-source LIS in this broader ecosystem
generally (with documented GeneXpert/Cepheid analyzer integration support), but no confirmed,
current, named pairing of OpenELIS specifically with a production KenyaEMR deployment was found.

**Pharmacy/commodity: two separate, unlinked systems.** `openmrs-module-commodity` is confirmed to
be a fork of the official `openmrs-module-stockmanagement` — Kenya's facility-level drug/commodity
stock tracking rides on the generic OpenMRS stock module, not a bespoke build. Separately, KEMSA (the
national medical-supplies parastatal) runs its own **i-LMIS** (an integrated Logistics Management
Information System, covering roughly 8,500 of Kenya's ~9,500 facilities as of its rollout, with a
Commodity Early Warning and Alert System, an AI-driven allocation engine, and an electronic
proof-of-delivery app). No direct system-to-system integration between KenyaEMR's stock module and
KEMSA's i-LMIS was found — these are two separate, currently-unlinked systems. This is relevant
context for inventory-api (which already owns drug/lot data for the whole Codevertex platform) as a
future, explicitly out-of-scope-for-now integration point, not a near-term task.

---

## 9. Other real Kenyan HMIS vendors worth tracking

Beyond what `docs/market-and-competitive-landscape.md` already names:

- **FunSoft ("FunSoft I-HMIS")**, built by System Partners Limited, open-source-platform-based,
  live and institution-wide at Kisii Teaching and Referral Hospital and Jaramogi Oginga Odinga
  Teaching & Referral Hospital since 2010 (registration, OPD/IPD, billing, pharmacy, nursing,
  finance, supply chain). Notably, DHA's own Kenya Patient Summary FHIR Implementation Guide names
  FunSoft, alongside eCHIS and KenyaEMR, as one of the systems it expects to interoperate with — a
  signal FunSoft has real government-facing status, not just a hospital case study.
- **C-PAD** — identified alongside OpenMRS, IQCare, and FunSoft as one of four EMRs meeting a
  national 60%-functionality threshold in an assessment; no further technical detail confirmed.
- **IQCare** ("IQCare KeHMIS") technical detail: built by Palladium, a browser-based data-entry
  application with a Windows-based form-configuration tool, backed by Microsoft SQL Server. A 2017
  national assessment found KenyaEMR and IQCare performing similarly (97% reporting completeness,
  83% timeliness) — useful concrete detail where the existing competitive doc only had "being phased
  out."
- **KNH's "Afya Apex Taifa Care System"**, unveiled March 2026, a paperless HMIS covering radiology,
  lab, triage, A&E, pharmacy, and registration at Kenya's largest national referral hospital. The
  "Taifa Care" branding suggests it likely runs on the same underlying national platform as the
  public SHA HMIS rollout (see §10), though this specific link is not independently confirmed.
- **Not found, despite targeted searching**: "iCare," "Lwazi," and "Ushauri" as real Kenyan
  clinical/HIV systems. These may be non-existent under these names, defunct, or confused with
  similarly-named systems elsewhere (a CDC/PEPFAR "SmartCare" system exists in Zambia, not Kenya).
  Do not repeat these three names as confirmed Kenyan competitors.

---

## 10. "Taifa Care HMIS" is a specific product: Tiberbu, by Medtronic LABS

The government's Taifa Care HMIS app is not an abstract, unbranded system. The public Play Store
listings (`com.medtroniclabs.spice.tiberbu` and a companion "Training TaifaCare" app), the domains
`tiberbu.com`/`hmis.tiberbu.app`, and Medtronic LABS' own Kenya programme page together identify it
as **"Tiberbu," built on Medtronic LABS' "SPICE" digital-health platform**. Medtronic LABS has
partnered with Kenya's Ministry of Health since 2018, originally for community-level NCD (diabetes/
hypertension) screening under SPICE, later extended into the general-purpose HMIS now used for
real-time SHA claims. A `developer.tiberbu.app` alias was also found hosting the AfyaLink developer
portal, suggesting Tiberbu's infrastructure underlies at least part of DHA's own developer tooling.
This is well-triangulated across independent sources (app-store metadata, the vendor's own site, a
peer-reviewed programme description) but is not confirmed by an official DHA/SHA statement
explicitly naming Tiberbu — treat it as strongly corroborated internal knowledge, not yet a citable
fact for a client-facing document, consistent with `docs/market-and-competitive-landscape.md` §7's
existing sourcing discipline.

Separately, the broader national digital-health backend (the Enterprise Service Bus and its
component registries, see `docs/compliance-kenya.md` §10) is reportedly built under a ten-year,
KES 104.8 billion contract by a consortium of **Apeiro Kenya Technologies (59.55%), Safaricom PLC
(22.56%), and Konvergenz Network Solutions (17.89%)** — publicly reported, as of go-live, to have
only 5 of a planned 17 architectural components functioning at full capacity. This is useful internal
context for expecting continued instability in the national infrastructure Codevertex Afya
integrates against, not something to name or editorialize about in front of a client (same
convention as `docs/market-and-competitive-landscape.md` §6's treatment of the SHA outage and RUPHA
payment-survey findings).

---

## 11. Biggest takeaways for Codevertex Afya's own roadmap

- The distributed billing ledger design is a genuine, verifiable architectural advance over the
  market's dominant open-source system's real, current billing model — not just a talking point.
- The insurance-claims-from-selected-line-items pattern is proven prior art; our own
  `BillableCharge`-to-claim design already matches its shape.
- Add an `exempted` status value to `BillableCharge`/`PatientAccount` charge status, distinct from
  `waived`, when Sprint 5 is implemented — a real gap this audit surfaced.
- The KHIS/ADX export (Sprint 10) should be designed knowing the market has already progressed past
  pure batch aggregate reporting for its highest-value data (HIV cascade); an automated feed is
  measurably more accurate than manual entry, a genuinely strong sales argument once built.
- TB reporting has a second, separate national target (TIBU, case-based) beyond generic KHIS/ADX
  aggregate export.
- Referred-out lab work (viral load, EID, TB) is realistically a batch courier-manifest workflow in
  Kenya, not universal live analyzer connectivity — worth designing Sprint 3 around both patterns.
- The specialized-programmes list (`docs/plan.md`, `docs/erd.md`, `sprint-10`) now explicitly
  includes VMMC, OTZ, PMTCT/EID, and cervical + prostate cancer screening alongside ANC/PNC/ART/TB/
  Immunization/Morgue.

## Could not confirm

Whether any KenyaEMR module implements DHA's newer FHIR Claim Bundle spec; whether `compass-shr` is
an officially designated national mediator versus a Palladium-internal tool; any KenyaEMR-specific
offline-sync design; whether the mUzima fingerprint pilot ever shipped into production; any direct
KenyaEMR-to-KEMSA i-LMIS integration; dedicated modules for KVP, family planning, nutrition, or
COVID-19; and the full current repo contents of the `facilitywide` and `Palladium-hub` orgs beyond
their top-level listings.

---

## References

- [Plan](plan.md)
- [Architecture](architecture.md) — "Distributed Billing & Patient Accounts"
- [Entity Relationship Diagram](erd.md)
- [Integrations](integrations.md)
- [Compliance & Certification Reference](compliance-kenya.md)
- [Market & Competitive Landscape](market-and-competitive-landscape.md)
- [Sprint 3: Laboratory](sprints/sprint-3-laboratory.md)
- [Sprint 5: Billing & Insurance](sprints/sprint-5-billing-insurance.md)
- [Sprint 10: Specialized Programmes + KHIS](sprints/sprint-10-specialized-programmes-khis.md)
