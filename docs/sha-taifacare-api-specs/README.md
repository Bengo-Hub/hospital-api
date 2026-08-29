# SHA / Taifa Care / DHA API Specifications (official UAT docs)

**Saved:** 2026-08-29, pulled directly from a shared Apidog project link
(`share.apidog.com/e4b53c35-99da-4941-bf04-dd25647defa0/`). These are raw OpenAPI 3.0.1 exports from
what the specs themselves call the **"Kenya Digital Superhighway"** project, served against
`https://uat.dha.go.ke`, the Digital Health Agency's own UAT host. This is the strongest source this
project has for the actual technical contract, stronger than any of the vendor blogs or news
articles cited in `../compliance-kenya.md`. Treat the endpoint shapes, field names, and example
payloads below as confirmed. Treat anything not shown in these specs (rate limits, production base
URL, the exact certification workflow) as still unconfirmed.

Both "Testing Env" and "Prod Env" are listed against the same `uat.dha.go.ke` host in every file
here, that looks like an artifact of how these docs were exported rather than a confirmed production
URL. Do not assume `uat.dha.go.ke` is also the production host without checking with DHA directly
when the time comes.

## Files

| File | Endpoint | Purpose |
|---|---|---|
| `api-13847375.md` | `GET /v1/hie-auth?key={consumer_key}` | Generate a JWT token via Basic Auth. **Token expires after 20 seconds.** |
| `api-13847589.md` | `GET /v3/client-registry/fetch-client` | Fetch a patient/client record from the national Client Registry by ID type + number. Response payload is encrypted (PII field), decrypt with your registered private key. |
| `api-13847595.md` | (labelled `PUT /v1/hie-auth` in the source doc, almost certainly a doc-export error, the real path is unconfirmed) | Update a Client Registry record's demographic and clinical-summary fields (allergies, chronic conditions, current medications). |
| `api-13847605.md` | `GET /v1/facility-search?facility_code=` | Look up a facility's registration/license status by its MFL facility code. |
| `api-13847619.md` | `GET /v1/practitioner-search?identification_type=&identification_number=` | Verify a practitioner's registration status by national ID or passport. |
| `api-13959747.md` | `GET /v1/shr-med/claim-status?claim_id=&bundle_id=` | Poll the status of a previously submitted claim. |
| `api-13976610.md` | `POST /v1/shr-med/bundle` (the source doc's own cURL example instead shows `/v1/shr-med/post-bundle`, another apparent doc-export inconsistency, confirm the real path before implementing) | Submit a claim as a FHIR-style `Bundle` (Organization + Coverage + Patient + Claim resources). |

## What this confirms, worth carrying into `integrations.md` and `compliance-kenya.md`

- **Auth is two-layer and short-lived.** Basic Auth (username/password issued via the "AfyaLink"
  dashboard) gets you a JWT that is valid for only 20 seconds. A client integration cannot cache
  this token for any meaningful period, it has to fetch a fresh one immediately before each call, or
  batch calls tightly around a single fetch.
- **"AfyaLink" is the actual name of the developer/credentials portal**, referenced directly inside
  the spec text ("obtained from the credentials section on your AfyaLink dashboard"). The earlier
  research pass could not confirm a specific developer-portal name, this settles it.
- **Client Registry responses carry encrypted PII**, decrypted with a private key whose matching
  public key is registered on the AfyaLink dashboard. Any service that calls this endpoint needs
  real key custody, not just a bearer token, that is a secrets-management requirement, not just an
  HTTP client requirement.
- **Diagnosis coding is ICD-11, not ICD-10.** The sample `Claim` resource in the Submit Claim spec
  uses `"code": "1A00", "display": "Cholera"`, which is an ICD-11 code (ICD-10's code for cholera is
  `A00`). `docs/erd.md`'s `diagnosis_catalog_default` table currently describes its `code` column as
  "ICD-10-ish", that should be corrected to ICD-11.
- **Claims are FHIR-message Bundles**, not a bespoke JSON shape: `resourceType: "Bundle"`,
  `type: "message"`, with `entry` items for `Organization` (the facility, identified by an
  `FID-xx-xxxxxx-x` style ID), `Coverage` (referencing a `Patient` and carrying
  `schemeCategory: "SOCIAL HEALTH AUTHORITY"`), `Patient` (identified by a Client Registry `CR...`
  ID), and `Claim` itself (`type`/`subType` like `institutional`/`op`, a `diagnosis` code+display, one
  or more `item`s with a `productOrService` code such as `SHA-02-005`, `quantity`, `unitPrice`, and a
  `total`).
- **Claim submission is asynchronous and polled**, not synchronous request/response. The submit
  endpoint returns a `mediator_id` with an explicit instruction to poll the claim-status endpoint
  with it, not a final adjudication result inline.
- **Facility and practitioner verification are separate, queryable endpoints.** A facility's
  operational/license status and a practitioner's registration/active status can both be checked
  against the national registry before treating either as valid, this is a real opportunity for
  hospital-api to validate a tenant's facility code or a doctor's license at onboarding time, not
  just at claim-submission time.

## What this does NOT confirm

- The production base URL (both environments point at the same UAT host in these docs).
- The exact DHA software-certification workflow (Form HMIS 4, step counts, turnaround time), that
  remains sourced only from vendor blogs per `../compliance-kenya.md` §4. These specs cover the
  runtime data-exchange contract, not the certification/onboarding process that grants access to it
  in the first place.
- Rate limits, retry/backoff guidance, or error-response shapes beyond the single 200 example shown
  per endpoint.
- Which specific consortium or aggregator relationship the `agent` field implies (`"agent":
  "SAFARICOM-CONSORTIUM-SANDBOX"` appears in the examples, suggesting integration may route through
  a sanctioned aggregator/consortium in some cases, not necessarily direct-to-DHA for every
  integrator). Confirm with DHA which access model applies to Codevertex before building against
  this assuming direct access.

## References

- [Integrations](../integrations.md) §2.4 to §2.5
- [Compliance & Certification Reference](../compliance-kenya.md)
- [Entity Relationship Diagram](../erd.md)
