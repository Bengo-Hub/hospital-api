# Hospital API — Kenya Regulatory & Certification Reference

**Last updated:** 2026-08-29, based on a dedicated research pass (web search, direct fetches of
Kenya Law, ODPC, and government sources). This is a compliance reference for engineering and sales,
not legal advice. Where a source could not be independently confirmed, that is stated plainly next
to the claim rather than smoothed over. Verify anything date-sensitive against a live source before
repeating it to a client or regulator, since this is a fast-moving area of Kenyan law.

This doc exists because `docs/plan.md` and `docs/integrations.md` had been describing SHA/Taifa
Care and Kenya DPA compliance in general terms since the 2026-07-31 scaffold. The research below
found a second, distinct, and legally binding certification requirement, the Digital Health Agency
(DHA) software certification under the Digital Health Act, that neither doc had captured. That
requirement changes real engineering scope (Sprint 5 and Sprint 12), so it gets its own reference
doc rather than a paragraph buried in an integrations file.

---

## 1. Three separate regulators, three separate obligations

It is easy to collapse these into one "Kenya health compliance" bucket. They are legally distinct,
have different clocks, and a facility (or Codevertex, on its behalf) can be in violation of one
while compliant with the other two:

| Regulator | What it governs | Legal basis |
|---|---|---|
| **SHA (Social Health Authority)** | Whether a facility can bill the national insurance scheme (SHIF) at all | Social Health Insurance Act, 2023 |
| **DHA (Digital Health Agency)** | Whether the *software* a facility uses is legally allowed to exchange health data with national systems | Digital Health Act, 2023 + its 2025 implementing regulations |
| **ODPC (Office of the Data Protection Commissioner)** | Whether the facility (as a data controller/processor) is registered and handling sensitive personal data lawfully | Data Protection Act, 2019 |

hospital-api touches all three. Getting a client live means clearing all three, not just wiring up
an SHA claims API.

---

## 2. SHA / SHIF: what it is and its current legal footing

- The **Social Health Insurance Act, 2023** (enacted 22 November 2023) repealed the NHIF Act 1989
  and created three funds: the Primary Healthcare Fund, the Social Health Insurance Fund (SHIF),
  and the Emergency, Chronic and Critical Illness Fund. **SHA** is the administering authority;
  **SHIF** is the scheme itself.
- NHIF registration closed 30 September 2024; SHIF benefits began 1 October 2024. Registration is
  mandatory for all Kenyans (unlike NHIF's voluntary informal-sector enrolment).
- **Its constitutional footing has been contested more than once, and litigation is still live.**
  A High Court ruling on 12 July 2024 found the Social Health Insurance Act, the Primary Health Care
  Act, and the Digital Health Act all unconstitutional for inadequate public participation, and gave
  Parliament 120 days to fix it. The Court of Appeal suspended that ruling on 20 September 2024,
  letting the 1 October 2024 rollout proceed. A further ruling in March 2026 found the 2024 rollout
  had violated constitutional rights and gave government a 90-day structural interdict to fix
  patient-safeguard gaps. That 90-day clock is separate from the HMIS-integration 90-day clock in
  §3 below, do not conflate the two. At least one more constitutional petition was still pending as
  of mid-2026.
- **What this means for the deck and for client conversations:** SHA/SHIF operates today and
  facilities must deal with it, but describing its legal foundation as fully settled would be
  inaccurate. Frame it as "operating under an active legal framework that has been challenged and
  revised more than once", not "settled law".

---

## 3. Taifa Care HMIS: the SHA-side integration deadline

- "Taifa Care" is the government's broader digital-health branding launched around the SHIF
  transition in October 2024. It runs alongside SHA/SHIF, it is not a replacement name for them.
- On 29 to 30 June 2026, at a joint MOH/SHA/DHA forum, SHA's CEO gave all healthcare providers 90
  days (landing at roughly late September 2026) to fully integrate with the Taifa Care HMIS or be
  de-contracted from SHA. The old SHA Provider Portal was retired for claims from midnight 29 June
  2026, with Level 4 public facilities required to submit claims through Taifa Care HMIS from that
  point. This was reported consistently across multiple Kenyan outlets (The Star, Citizen Digital,
  allAfrica, and others) and corroborated by an official government source: a 10 July 2026 update
  on the government's own advertising-agency site (gaa.go.ke) quoting Health CS Aden Duale on a
  phased migration, with concrete rollout numbers (see below).
- **Rollout progress as of 10 July 2026** (the one official .go.ke figure found): 11,034
  SHA-accredited facilities, 6,228 public facilities (93%) digitally transformed, 5,078 facilities
  live on SHA HMIS, 2,978 facilities submitting e-claims. Read plainly: most public facilities were
  reported as "transformed", but a meaningfully smaller number were actually live and submitting
  claims, and this figure does not clearly break out private facilities at all. This is the honest
  basis for "most small and private facilities are not there yet", not a specific quoted percentage
  for that subgroup.
- **A separately-sourced 1 September 2026 date appears in vendor-blog commentary** (not an official
  DHA or SHA notice we could load directly): private facilities without a DHA-certified HMIS
  integrated into the national Health Information Exchange lose the ability to process SHA claims or
  win new SHA contracts from that date. This is close to, but not confirmed identical to, the
  "90 days from 29 June 2026" date above. Treat both as pointing at the same underlying pressure
  (Q3 2026), not as two independently verified hard deadlines. Confirm the live date against an
  official DHA/SHA notice before quoting a specific day to a client.
- No public registry of currently DHA-certified HMIS vendors was found or independently verified
  during this research, despite references to one existing. Do not tell a client "we are on the
  list" or "we checked the list", state plainly that no such registry was accessible to verify
  against.

---

## 4. DHA certification: the part the earlier drafts of these docs missed

This is the most consequential finding. It is not a policy aspiration, it is binding regulation with
specific numbered provisions, confirmed directly from Kenya Law's official text.

- **Digital Health Act, 2023** (enacted 19 October 2023) established the **Digital Health Agency
  (DHA)**, live at `dha.go.ke`, as the statutory regulator for digital health systems in Kenya.
- **The Digital Health (Health Information Management Procedures) Regulations, 2025** came into
  force 11 April 2025 (Kenya Law: `new.kenyalaw.org/akn/ke/act/ln/2025/76/eng@2025-04-11`). Key
  provisions, cited by regulation number:
  - **Reg 37(1):** a healthcare provider may not use a digital health solution unless it has been
    certified by the DHA. This is the actual statutory basis for mandatory HMIS/EMR certification,
    not a guideline.
  - **Reg 38(3):** certification fees range from KES 10,000 to KES 500,000 depending on the solution
    type.
  - **Reg 5(1)(f) / 15(2):** the DHA must retain health data for a minimum of 20 years, with the
    clock starting from the last update to the record.
  - **Reg 16(1)/(3):** legacy data must move to County Health Data Banks within 24 months of their
    operationalization, and to compliant systems or the National Health Data Bank within 1 year of
    the regulations coming into force.
  - **Reg 11(2)(a):** a data breach must be notified to the DHA CEO within 48 hours.
  - **Reg 12(c)(ii):** audit logs must be kept for a minimum of 20 years.
- **The Digital Health (Data Exchange Component) Regulations, 2025** governs the technical
  data-sharing layer, the national Health Information Exchange (HIE), also in force from 11 April
  2025 (`new.kenyalaw.org/akn/ke/act/ln/2025/77/eng@2025-04-11`).
- **Technical standards referenced across DHA's own materials and corroborating sources:** HL7 FHIR
  (R4) for real-time exchange, SNOMED CT / ICD-10 / ICD-11 / LOINC for clinical terminology (DHA's
  "Kenya National Terminology Services" maps these), integration through a national CIHIS
  (Comprehensive Integrated Health Information System) Enterprise Service Bus. DHA's national
  infrastructure also includes the Health Information Exchange itself, a Kenya Client Registry, a
  Health Worker Registry, a Facility Registry, a National Shared Health Record, the "Afya Yangu"
  patient portal, and an Electronic Health Insurance Claims Platform.
- **Certification procedure specifics found only in vendor/consultancy blogs, not an official DHA
  page we could load directly:** a self-attestation form referred to as "Form HMIS 4", and a
  described 5-to-6-step process (self-attestation, ODPC registration proof, a Data Protection
  Impact Assessment, documented security and backup policy, interoperability testing, then DHA
  technical review with a roughly 30-day turnaround). The substance is corroborated by the actual
  regulation text above, but these specific labels and step counts are not independently confirmed
  and should not be quoted to a client as official DHA terminology without verification.

**What this means for hospital-api's roadmap:** the "Future: Taifa Care HMIS" line in
`docs/integrations.md` §2.4 previously framed this as "treasury-api builds an adapter when it's
ready". That is still true for the SHA claims side, but DHA software certification is a separate,
additional gate that applies to hospital-api and hospital-ui directly, not just to treasury-api's
insurance connector. A tenant cannot legally run Codevertex Afya against national health systems
until the software itself is DHA-certified, regardless of whether treasury-api's Taifa Care adapter
is done. See `docs/sprints/sprint-12-compliance-hardening.md` for the resulting task list.

**Update, 2026-08-29:** a set of official DHA UAT API specifications was obtained directly (saved in
full at `docs/sha-taifacare-api-specs/`), covering the actual data-exchange contract: JWT
authentication, Client Registry lookup, facility and practitioner verification, and FHIR-Bundle claim
submission. This confirms several technical details, HL7 FHIR usage, ICD-11 diagnosis coding, a
20-second JWT lifetime, encrypted PII exchange requiring real key custody, that were previously only
inferred from secondary sources. It does **not** confirm the certification workflow itself (the "Form
HMIS 4" self-attestation process described in §4 above remains sourced only from vendor blogs), these
specs document the runtime API a certified system would call, not the process for getting certified
to call it.

---

## 5. ODPC / Data Protection Act: the parallel, older obligation

Read directly from the ODPC's own "Guidance Note on the Processing of Health Data" (December 2023).

- Health data is explicitly classified as **sensitive personal data** under the Data Protection Act,
  2019, requiring additional safeguards.
- **Registration with the ODPC as a data controller and/or processor is mandatory** for entities in
  the healthcare sector. Fee tiers (from the DPA's own registration regulations): micro/small
  KES 4,000, medium KES 16,000, large KES 40,000. A certificate is valid for 24 months and typically
  takes about 14 days to process.
- **KMPDC (Kenya Medical Practitioners and Dentists Council) enforcement:** effective 1 January
  2025, all new health facility registrations require a valid ODPC "Certificate of Data
  Handler/Processor". Existing facilities had until 31 March 2025 to obtain one. Non-compliance
  penalty cited by KMPDC: fines up to KES 5 million or 1% of annual turnover.
- **A Data Protection Impact Assessment (DPIA) is required** where processing is likely to pose a
  high risk to data subjects' rights. Note this is a separate DPIA obligation from the one DHA's
  certification process also asks for, a facility (or Codevertex, running the system) may need to
  produce two DPIAs for two different regulators covering overlapping ground.
- **Breach notification has two different clocks that can both apply to the same incident:** 72
  hours to the ODPC under the DPA, versus 48 hours to the DHA CEO under the 2025 Digital Health
  Regulations (§4 above). Build for the tighter one.
- **Retention period, do not conflate the two regimes:** the DPA itself does not set a fixed
  retention period generally, the ODPC's own guidance uses "seven years" only as an illustrative
  example for non-returning or deceased patients, not a legal minimum. The newer, DHA-specific 2025
  regulations do set a hard 20-year minimum, but only for data held in DHA-connected systems (§4
  above). Use the 20-year figure only when talking about DHA-connected/HMIS-certified systems.
- **Data localization:** the DPA requires personal data to be stored and processed in Kenya except
  with explicit consent or when the receiving country offers adequate protection. A March 2026 legal
  update flags that the Digital Health Act's own cross-border data rules (Section 47) remain
  ambiguous, particularly for offshore-hosted systems. Codevertex Afya is cloud-hosted in Kenya (see
  `docs/plan.md`), which avoids this question rather than needing to answer it, but note it as an
  open point in Kenyan law generally, not a settled rule.

---

## 6. Facility identity: MFL codes and licensing

- Every Kenyan health facility gets a unique, immutable **Master Facility List (MFL) code** from the
  Kenya Master Health Facility Registry (KMHFR, at `kmhfr.health.go.ke`).
- A facility is first licensed by its sector regulator (KMPDC for private hospitals and clinics),
  which pushes the new facility into KMHFR for County/Sub-County Health Records Officers to validate
  before the MFL code is assigned back to the regulator.
- **Facility levels (1 to 6, under Kenya's Essential Package for Health):** Levels 1 to 3 cover
  community units through health centres and dispensaries. Level 4 is a sub-county hospital. Level 5
  is a county referral hospital (100+ beds, medically-led). Level 6 is a national referral or large
  private teaching hospital. Patients move between levels by referral letter.
- The June/July 2026 Taifa Care rollout explicitly targeted Level 4 public hospitals first. No
  official document laying out the full level-by-level sequencing beyond that was found, treat
  "when does a Level 2/3 private facility get folded in" as an open question, not something to
  promise a specific date for.
- Codevertex Afya already models `hospital_tenant` metadata for this (see `docs/erd.md`). Storing
  the facility's MFL code, KMPDC registration number, and facility level as tenant metadata (not new
  schema tables) is the natural place for this, consistent with how the ODPC Certificate of Data
  Handler/Processor number is already planned as tenant metadata in Sprint 12.

---

## 7. What is confirmed versus what needs live verification before quoting a client

**Confirmed from a primary source (Kenya Law or an official .go.ke page):**
- Digital Health Act 2023 and its two 2025 implementing regulations, including the specific
  regulation numbers and figures in §4.
- SHA replacing NHIF on 1 October 2024, and the Social Health Insurance Act's legal basis.
- The July 2024 unconstitutionality ruling and its September 2024 Court of Appeal suspension.
- The 29 to 30 June 2026 SHA announcement and the rollout progress numbers, via the gaa.go.ke source.
- ODPC's sensitive-personal-data classification and registration requirement, from its own guidance
  note.
- KMPDC's Certificate of Data Handler/Processor mandate and its January/March 2025 deadlines.

**Not independently confirmed, verify before repeating as fact:**
- The exact wording "Form HMIS 4" and any specific step count for DHA certification.
- Whether "1 September 2026" and "90 days from 29 June 2026" are the same deadline or two related
  but distinct ones.
- The existence or contents of a public, browsable list of DHA-certified HMIS vendors.
- The full level-by-level rollout schedule beyond "Level 4 public hospitals first".
- The exact citation for the March 2026 court ruling referenced in §2.

See §8 for a 2026-08 update from a primary DHA source (an open tender) that confirms the
conformance-suite gap above is a known unknown, not just an unverified vendor claim, and gives a
rough timeline for when it might close.

---

## 8. DHA's own procurement of the RMNCAH platform (2026-08): a primary-source update

This section is based on a direct read of DHA's own open tender, not a vendor blog or secondary
report, so it carries more weight than most of the "not independently confirmed" items in §7. Source:
Tender No. DHA/ONT/01/2026-2027, "Procurement of a Foundational AI Driven Digital Health Platform for
RMNCAH in Kenya", notice dated 26 August 2026, submission deadline 4 September 2026. Full extraction:
`shared/tenders-and-cvs/dha-tender-analysis.md`.

- **This confirms the conformance suite in §4 does not exist yet, and DHA is procuring the team that
  will write it.** The tender's Key Personnel table asks for a "Health Information Systems
  Integration Lead" with experience "leading the integration of multiple facility level health
  information systems or electronic medical records with a national exchange, including publishing
  specifications and conformance suites for third party system owners and onboarding them." Read
  plainly: the specifications a facility EMR like hospital-api would eventually need to pass are not
  published anywhere today, they are a deliverable of this contract, to be produced by whoever wins
  it. Nothing in §4's "Form HMIS 4" / vendor-blog description changes as a result, this just confirms
  why no primary DHA source for it could be found: there may not be a stable one yet.
- **Timeline signal.** The contract period is fixed at 6 months plus a further 6-month post-warranty
  period, with production go-live targeted at month 5 of the contract. If DHA awards shortly after
  the 4 September 2026 deadline, a realistic go-live for this specific platform is not before
  mid-to-late 2027. That is a separate, slower clock than the SHA/Taifa Care Q3 2026 pressure in §3,
  do not conflate the two: the SHA HMIS integration deadline is near-term and already live, this
  RMNCAH platform (and whatever conformance suite eventually comes out of it) is not.
- **DHIS2 named again, as the specific target.** The tender names DHIS2 exactly once, as a national
  system its "data connectors and APIs" must ingest facility RMNCAH data from "without requiring
  facilities to duplicate data entry." This directly validates the KHIS/DHIS2 ADX export work already
  planned in `integrations.md` §2B and `plan.md` Sprint 10/capability 14, that work is aimed at the
  system DHA itself is building toward, not a guess.
- **A concrete RMNCAH indicator set, confirmed from a DHA source.** The only indicators named anywhere
  in the tender are antenatal care (ANC) coverage, skilled birth attendance, immunisation coverage,
  maternal mortality, and newborn mortality, plus a cause-of-death/risk-pattern analysis fed by
  facility-recorded deaths and community deaths captured through a "verbal autopsy" workflow. These
  map directly onto hospital-api's own planned ANC/PNC/Immunization programme tracking and Morgue
  module (`plan.md` Sprint 10/capability 13). Structuring cause-of-death capture in the Morgue module
  in a way that could later export ANC/immunisation/mortality indicators cleanly is now backed by a
  named national priority, not just an internal roadmap guess. This is a design cue for later sprint
  work, not a new near-term compliance obligation.
- **A new architecture signal: DHA's platform may write back into facility EMRs, not just read from
  them.** The tender describes "ambient listening" at the point of care (transcribing and structuring
  clinician-patient consultations) with "clinician review and approval before any data is written back
  to the facility EMR", plus clinical decision-support alerts surfaced to the clinician. If DHA's
  platform is eventually built this way, a facility EMR integrating with it would need to expose an
  inbound API to accept structured consultation data and clinical flags, not only an outbound
  reporting/export API. Worth keeping in mind when `integrations.md` §2B's export-only design is
  revisited, but this is speculative until DHA's own conformance suite exists (see the first bullet
  above), not a requirement to build against today.
- **"Taifa Care" gets a fourth confirmed usage.** The tender's Component 3 is a WhatsApp-based
  "Taifa Care conversational AI channel" for beneficiary engagement (rendered once in the source
  document as "aTaifacare"). This is a DHA source, not a vendor blog, confirming that "Taifa Care" is
  used as DHA's broader digital-health brand beyond the SHA claims HMIS already covered in §3, on top
  of KenyaEMR's own "TaifaCare, powered by KenyaEMR" rebrand already flagged in
  `market-and-competitive-landscape.md` §5. The naming-collision warning there now covers four
  distinct things sharing the phrase "Taifa Care", not three.
- **What this tender does *not* confirm, kept explicit on purpose:** no HL7 FHIR version, no named
  uptime/SLA figures, no certification fee or process specific to this platform, and the phrase
  "digital superhighway" itself is never defined anywhere in the 200-page document, it appears exactly
  once, in the qualification text for a personnel role. Treat all of this section as market and
  roadmap intelligence about where DHA is heading, not as a new compliance deadline layered on top of
  §4's certification requirement or §3's Taifa Care HMIS deadline.

---

## 9. References

- [Plan](plan.md)
- [Integrations](integrations.md) §2.2 to §2.5
- [Sprint 5: Billing & Insurance](sprints/sprint-5-billing-insurance.md)
- [Sprint 12: Compliance Hardening](sprints/sprint-12-compliance-hardening.md)
- [Market & Competitive Landscape](market-and-competitive-landscape.md)
- Kenya Law: Digital Health (Health Information Management Procedures) Regulations, 2025
- Kenya Law: Digital Health (Data Exchange Component) Regulations, 2025
- ODPC: Guidance Note on the Processing of Health Data (December 2023)
- DHA Tender No. DHA/ONT/01/2026-2027, "Procurement of a Foundational AI Driven Digital Health
  Platform for RMNCAH in Kenya" (notice 26 August 2026); full extraction at
  `shared/tenders-and-cvs/dha-tender-analysis.md`
