# Hospital API — Market & Competitive Landscape (Kenya)

**Last updated:** 2026-08-29, based on a dedicated research pass covering existing EMR/HMIS vendors
in Kenya, pricing benchmarks, and where Codevertex Afya's real advantages sit. This is a product and
sales reference, not a legal or financial document. Vendor-published figures are marked as such and
should not be repeated as independently verified facts.

---

## 1. The market splits into three tiers, and Codevertex Afya is deliberately built for the middle one

**Donor-funded and open-source clinical EMRs**, grown out of HIV/PEPFAR case management:
KenyaEMR (OpenMRS-based, rebranded in 2025 as "TaifaCare, powered by KenyaEMR", not to be confused
with the SHA claims system of a similar name, see §5), IQCare (being phased out in favor of
KenyaEMR), and AMRS (the AMPATH Medical Record System, academic-medical-center pedigree from
western Kenya). These are free, government-endorsed, and strong on HIV/TB/immunization reporting,
but they were never built as commercial billing systems: none of them carry native M-Pesa collection
or KRA eTIMS invoicing, because that was never their job.

**Enterprise systems built for large hospital groups and claims processing.** Slade 360 (Savannah
Informatics) is the recognized market leader in electronic claims validation between payers and
providers, reportedly present in 2,500+ hospitals for that specific function, not necessarily as
each facility's primary clinical EMR. Kenya's largest referral hospitals (KUTRRH, KNH) run custom,
undisclosed-vendor systems built for their own scale. This tier is priced and built for Level 5 and
6 facilities, not a 20-patient-a-day clinic.

**A crowded field of Kenya-built commercial vendors serving small-to-mid facilities**, roughly
Codevertex Afya's actual competitive set: Hanmak/MedicentreV3, AfyaConnect, CarHill Medical,
DolphineHMS, EasyClinic, iDeveloper, Techsphere's E-Afya, and others. Most now advertise M-Pesa and
SHA/SHIF as built-in. AphiaOne explicitly markets itself for "the complexity of Level 4, 5, and 6"
facilities, by its own positioning it is not competing for the Level 2/3 dispensary segment at all.

**Two more real, previously untracked vendors, both above the small-clinic tier.** FunSoft
("FunSoft I-HMIS," built by System Partners Limited) has run institution-wide at Kisii Teaching and
Referral Hospital and at Jaramogi Oginga Odinga Teaching & Referral Hospital since 2010, covering
registration, OPD/IPD, billing, pharmacy, nursing, finance, and supply chain, notable enough that a
DHA-published national FHIR Implementation Guide names it, alongside KenyaEMR and the community-health
system eCHIS, as a system it expects to interoperate with. C-PAD was separately identified, alongside
OpenMRS/KenyaEMR, IQCare, and FunSoft, as one of four EMRs meeting a national 60%-functionality
assessment threshold, no further technical detail was confirmed for it. IQCare's own technical build
(useful where the earlier "being phased out" framing had no detail) is a Palladium-built, browser-based
data-entry application backed by Microsoft SQL Server, with a 2017 national assessment finding it
performed similarly to KenyaEMR (97% reporting completeness, 83% timeliness). KNH, Kenya's largest
national referral hospital, unveiled its own "Afya Apex Taifa Care System" in March 2026, a paperless
HMIS spanning radiology, lab, triage, A&E, pharmacy, and registration, the "Taifa Care" branding
suggests a link to the national platform described in §5, though that link is not independently
confirmed. Full technical detail on all of the above: `docs/kenyaemr-technical-reference.md` §9.

**Where Codevertex Afya sits:** priced and scoped for Level 2 to 4 facilities (the Afya Clinic and
Facility tiers), with the Afya Hospital tier available for Level 5/6 when a client grows into it,
without re-entering data. That is a genuine, defensible position: several of the more visible
competitors either skip the small end of the market by design (AphiaOne) or were never built for
commercial billing at all (the donor-funded EMRs).

---

## 2. Evidence of a real gap at the small end of the market

- Business Daily Africa reporting cites Ministry of Health facility-readiness data: only 7% of
  Kenyan facilities are ready to provide basic outpatient services, and only 2% can offer all
  essential health services. That is not a vendor-neglect claim on its own, but it establishes that
  the capacity gap is real and that smaller facilities sit at the bottom of it.
- No single news article or MOH report was found making the specific claim "large HMIS vendors
  ignore small clinics." That framing is an inference from the evidence (AphiaOne's own
  Level-4-and-up positioning, the enterprise players' focus on referral hospitals, the general
  absence of small-clinic case studies from the bigger vendors), not a directly sourced quote, and
  should be presented that way rather than attributed to a specific publication.
- **Pricing benchmark:** KES 5,000 to 10,000 per month is the recurring figure for a small
  clinic/dispensary tier across multiple sources, corroborated independently by AfyaConnect's own
  published pricing page. Mid-tier (Level 3B/4 with maternity) runs roughly KES 27,000 to 40,000 per
  month. Enterprise/multi-branch tiers start around KES 60,000 per month.
- Codevertex Afya Clinic (KES 7,500/month) sits inside that small-clinic benchmark range. The
  Inpatient add-on (KES 4,000/month, total KES 11,500/month for this client's actual quotation) is a
  genuine differentiator at that price point: most small-clinic-tier competitors do not offer any
  inpatient/ward tracking below their mid or enterprise tier.

---

## 3. M-Pesa: table stakes among direct competitors, not a standalone differentiator

M-Pesa (typically STK push for bills, deposits, and co-pays) is now advertised as standard by
essentially every Kenya-built commercial vendor in Codevertex Afya's actual competitive set: Hanmak,
AfyaConnect, CarHill, DolphineHMS, EasyClinic all list it as built-in. Claiming "we support M-Pesa"
alone will not move a buyer who is also evaluating those vendors.

It remains a real differentiator only against the donor-funded/open-source EMRs (which have none)
and against any legacy or international system without local payment rails. Where Codevertex Afya
can still win on this point is depth, not presence: one connected record where a cash, M-Pesa, or
insurance payment against the same invoice reconciles automatically, rather than a payment
collected in one tool and manually reconciled against records in another.

**A direct technical audit of KenyaEMR's real billing code (2026-08-29) confirms this depth argument
is genuine, not just positioning.** KenyaEMR's production billing backend is cash-point/cashier-
centric, one bill is tied to one physical collection point and one cashier at posting time, the
opposite of Codevertex Afya's distributed ledger, where any department can charge and the Billing
desk is the fallback. This is a real architectural gap in the market's most-deployed open-source
system, not a manufactured claim. Full detail: `docs/kenyaemr-technical-reference.md` §3.

---

## 4. KRA eTIMS: a genuine, currently under-exploited differentiator

eTIMS (electronic Tax Invoice Management System) enforcement tightened sharply through 2025 and
2026. KRA had issued more than 140,000 compliance notices by early 2026, with penalties up to KES 1
million or 200% of the tax due, and from 1 January 2026 KRA validates income and expense
declarations against eTIMS data as a primary source, meaning unsupported expenses lose their
tax-deductibility. This affects hospitals, clinics, and pharmacies on both the revenue side (patient
and insurer receipts) and the expense side (drug and supplier purchases).

The donor-funded clinical EMRs have no fiscal-invoicing function at all. Several vendors listed in a
general Kenya HMIS directory (SoftwareSuggest) show no eTIMS mention whatsoever, which suggests a
meaningful share of the installed base still bolts a separate accounting or POS tool onto their EMR
purely for tax compliance. Codevertex Afya's eTIMS integration is opt-in per tenant/service (see
`docs/integrations.md` §2.3), switched on only where a facility actually needs to issue a fiscal
invoice, which is a more accurate model of how Kenyan clinical billing actually works than an
all-or-nothing approach.

---

## 5. A naming collision worth handling proactively with clients

"Taifa Care" is used as the brand name for **two different things**: the government's umbrella
digital-health programme brand generally, and specifically the SHA claims-processing HMIS that
Level 4 public hospitals were moved onto from 29 June 2026 (see `docs/compliance-kenya.md` §3).
Separately, KenyaEMR (the OpenMRS-based clinical system) rebranded in 2025 as "TaifaCare, powered by
KenyaEMR." A client who has heard the phrase "Taifa Care" in the news may mean any of these three
things. Worth clarifying directly in a sales conversation rather than assuming which one they mean.
A fourth usage, DHA's own tender names a "Taifa Care" WhatsApp beneficiary-engagement channel
distinct from all three (`docs/compliance-kenya.md` §8). The SHA claims HMIS itself is very likely a
specific commercial product ("Tiberbu," by Medtronic LABS, see `docs/compliance-kenya.md` §3 and
`docs/kenyaemr-technical-reference.md` §10), not an unbranded government build, worth knowing
internally even though it is not yet confirmed solidly enough to state to a client as fact.

---

## 6. Recent context worth having in mind for a client conversation, handled carefully

Kenya's digital-health rollout has had visible, publicly reported execution problems: a nationwide
SHA systems outage disrupted hospital pre-authorizations in early 2025, and a large centralized
digital-health rollout at a national referral hospital suffered a public go-live failure in March
2026. RUPHA's own 2025 facility payment survey found only 20% of primary-healthcare-accredited
facilities were paid every month in the first quarter of that year, with Level 2/3 facilities hit
hardest.

These are real, well-sourced facts, but they concern a national government system and its funding
mechanics, not Codevertex Afya. Referencing them in a client pitch should be limited to a general,
respectful framing (a large, rushed, centralized rollout carries real execution risk, which is
exactly why Codevertex phases a facility's go-live deliberately) rather than naming the specific
incidents or organizations involved. It is not Codevertex's place to editorialize about SHA's
finances or a public hospital's outage in a sales document, however accurately sourced.

The same treatment applies to a further finding (2026-08-29): the national digital-health backend
behind SHA/DHA is reportedly a named, ten-year, KES 104.8 billion contract split across three named
companies, publicly reported as running only a fraction of its planned architecture at full capacity
as of go-live. Useful internal context for expecting continued rough edges in the systems Codevertex
Afya integrates against, not something to name in front of a client. Full detail:
`docs/compliance-kenya.md` §3.

On the positive side, African healthtech funding is real and growing: $215 million in equity
funding across the continent in 2025, up 232% year over year, with Kenya alone drawing roughly $87
million across 38 deals. That is useful context for "this is a real, growing market", not a claim
about Codevertex Afya's own funding or scale.

---

## 7. What not to claim

- Do not name Zeltas, Access:mds, Ayasdi, MEDDIC/MEDNET, Elsewedy Electric, or InterSystems TrakCare
  as Kenyan HMIS competitors. No credible evidence of a Kenya health-sector deployment was found for
  any of them during this research.
- Do not name "iCare," "Lwazi," or "Ushauri" as Kenyan clinical/HIV systems either (checked again
  2026-08-29, targeted searches found no trace of any of the three under those names in Kenya's
  health-IT landscape, a CDC/PEPFAR "SmartCare" system by a similar description exists in Zambia,
  not Kenya).
- Do not repeat vendor-published statistics (AphiaOne's "60% wait-time reduction" case study,
  specific "X hospitals served" counts from Hanmak or others) as independently verified. They are
  fine to reference internally as "vendor X claims Y", not as confirmed fact in a client document.
- Do not describe medsoftwares.com-sourced pricing (HospitalOS/PharmaPOS one-time-license figures)
  as independently verified: that site appears to be vendor-operated content marketing for its own
  products.

---

## 8. The regulator itself is now buying AI-driven dashboards, not just certifying EMRs

DHA's own open tender for a "Foundational AI Driven Digital Health Platform for RMNCAH" (notice 26
August 2026, see `docs/compliance-kenya.md` §8 for the full read) is useful context beyond
compliance: it shows DHA is actively procuring national-level AI dashboards, drill-down reporting, and
cause-of-death analytics for itself, not only writing certification rules for vendors. That is a
signal the market is moving toward AI-assisted, dashboard-first health reporting at the national
level, reinforcing that Codevertex Afya's own roadmap (Sprint 11's reporting/analytics dashboards,
Sprint 10's KHIS/DHIS2 export) is aimed in the right direction. It is not evidence of anything specific
to Codevertex Afya's own capabilities or scale, and should not be cited to a client as if it were.

---

## 9. References

- [Plan](plan.md)
- [Compliance & Certification Reference](compliance-kenya.md)
- [KenyaEMR Technical Architecture Reference](kenyaemr-technical-reference.md)
- `d:\Projects\Codevertex\CODEVERTEX AFRICA HOSPITAL MANAGEMENT SYSTEMS PRICING MODEL.md`
- DHA Tender No. DHA/ONT/01/2026-2027, "Procurement of a Foundational AI Driven Digital Health
  Platform for RMNCAH in Kenya" (notice 26 August 2026); full extraction at
  `shared/tenders-and-cvs/dha-tender-analysis.md`
