# ClearLedger Bidder OpenRTB

ClearLedger Bidder OpenRTB is an open-source reference bidder for agencies, brands, and buying teams that want to operate their own OpenRTB demand endpoint for ClearLedger approved-buyer lanes.

The service runs as a standalone Go HTTP process. It accepts OpenRTB 2.x-compatible JSON bid requests, evaluates local campaign, budget, pacing, placement, Deal ID, privacy, and creative rules, and returns either a controlled no-bid or a valid OpenRTB bid response within the request timeout.

ClearLedger uses this endpoint as the buyer-side decision surface. The ClearLedger network remains responsible for exchange and publisher supply ingestion, signed fanout, lane enforcement, bid validation, delivery evidence, billing evidence, settlement evidence, publisher net calculations, ClearLedger fee accounting, payout workflows, and final receipt production.

## Project Scope

This repository contains the bidder runtime and local operator tooling:

- OpenRTB buyer endpoint: `POST /openrtb`
- Optional multi-buyer route: `POST /buyers/{buyer}/openrtb`
- Local campaign and creative configuration
- Budget, pacing, QPS, floor, Deal ID, placement, media, and privacy checks
- VAST, display, audio, and OpenRTB Native response helpers
- ClearLedger-compatible auth, request signatures, proof extensions, notice URLs, and registration payloads
- Health, readiness, Prometheus metrics, sanitized state, and local notice callback endpoints
- Certification and ClearLedger lane harnesses for standalone and approved-buyer validation
- Docker, Compose, sample requests, sample responses, sample campaign config, benchmarks, and CI tests

The repository does not include the ClearLedger ad server, auction router, exchange gateway, clearing system, marketplace, receipt network, payment workflow, payout workflow, or settlement infrastructure.

## Architecture

```text
publisher / exchange supply
        -> ClearLedger auction router
        -> signed OpenRTB fanout
        -> agency-hosted bidder
        -> ClearLedger winner validation
        -> delivery proof, billing proof, settlement proof, final receipt
```

The bidder decides whether to bid. ClearLedger validates the response and records the delivery and financial proof required for billing and settlement.

## Supported Operating Model

1. Configure campaigns, budgets, targeting rules, and creatives in local JSON.
2. Run `make test`, `make configcheck`, and `make bench`.
3. Deploy the bidder behind a stable HTTPS endpoint.
4. Run `cmd/certify` against the public endpoint.
5. Optionally run `cmd/clearledger-harness` with a ClearLedger-style runtime manifest for local lane proof.
6. Submit the approved-buyer registration payload to ClearLedger.
7. ClearLedger runs independent certification and, after approval, publishes the buyer endpoint in the active runtime manifest.
8. Monitor `/readyz`, `/metrics`, `/statez`, and local notice callbacks while ClearLedger owns billable delivery, settlement, payout, and receipt evidence.

The bidder is intentionally CLI/API first. Agencies may build internal workflow tools on top of the JSON config and operator endpoints, but the runtime itself is kept focused on the low-latency programmatic decision path.

## Runtime Contract

ClearLedger sends bid requests to the bidder over HTTPS:

```http
POST /openrtb
Authorization: Bearer <token>
X-ClearLedger-Buyer-Timestamp: <rfc3339nano>
X-ClearLedger-Auction-ID: <auction id>
X-ClearLedger-Request-ID: <request id>
X-ClearLedger-Buyer-Body-SHA256: <sha256 raw body>
X-ClearLedger-Buyer-Signature: hmac-sha256=<hmac_sha256 signature>
X-OpenRTB-Version: 2.6
Content-Type: application/json
```

`/buyers/{buyer}/openrtb` is also supported for deployments that route multiple approved buyer names through one host.

The production signature base is documented in `docs/clearledger-contract.md`.

Requests must include:

- `id`, `tmax`, and `cur`
- exactly one of `site` or `app`
- one or more `imp` objects
- exactly one media object per impression: `banner`, `video`, `audio`, or `native`
- non-negative `tmax`, floor, media dimensions, and duration bounds
- valid duration bounds when both minimum and maximum are present
- parseable OpenRTB Native request JSON for native inventory
- floor and currency through `imp.bidfloor` and `imp.bidfloorcur`
- PMP Deal ID through `imp.pmp.deals[].id` for private-auction inventory
- optional `source.ext.schain`, `regs`, `device`, `user`, and `imp.ext.clearledger` proof fields

The bidder verifies that ClearLedger request headers match the OpenRTB request where applicable. `X-ClearLedger-Request-ID` must match OpenRTB `id`; `X-ClearLedger-Auction-ID` must match `source.tid` or `id`; buyer and seat headers must match the configured buyer identity.

## OpenRTB Compatibility

OpenRTB compatibility is configured locally:

- `accepted_openrtb_versions` or `BIDDER_OPENRTB_ACCEPTED_VERSIONS`, default `2.6,2.5`
- `openrtb_outbound_version` or `BIDDER_OPENRTB_OUTBOUND_VERSION`, default `2.6`
- `openrtb_compat_profile` or `BIDDER_OPENRTB_COMPAT_PROFILE`, default `openrtb_json`
- `preserve_partner_ext` or `BIDDER_OPENRTB_PRESERVE_PARTNER_EXT`, default `true`

The bidder detects the request version from `X-OpenRTB-Version` first, then body and extension hints. OpenRTB 2.6 and 2.5 are accepted by default. Older 2.x variants require explicit opt-in.

The decoder normalizes common compatibility fields into the local request model, including privacy values from `regs.ext`, `source.ext.schain`, `user.ext.consent`, `user.ext.eids`, floor aliases, native `version`/`ver`, and video/audio `placement`/`plcmt`. Response `ext.openrtb_compat` records detected version, outbound version, profile, normalized fields, and preserved extension keys.

## Bid Responses

A valid bid response includes:

- response `id` matching the request
- `seatbid[].seat`
- `bid.id`
- matching `bid.impid`
- positive CPM `bid.price`
- `bid.crid`
- non-empty `bid.adomain`
- non-empty `seatbid[].bid`
- unique `bid.id`
- at most one bid per impression in a single response
- `bid.dealid` matching the impression Deal ID for PMP inventory
- `bid.adm` containing VAST for video/audio, display markup for banner, or OpenRTB Native response JSON for native
- optional absolute `http` or `https` `nurl`, `burl`, and `lurl`
- `bid.ext.clearledger` with buyer, campaign, creative, and echoed ClearLedger lane/package/placement/proof identifiers when present in the request

When `imp.ext.clearledger.receipt_required` is true, the validator requires `bid.ext.clearledger` to include buyer, campaign, and creative IDs and to echo any ClearLedger lane, package, placement, and proof fields from the request.

For video and audio, `bid.adm` must be parseable VAST with an impression, duration, and media file. When the request declares media constraints, the validator checks duration, MIME type, and dimensions where available.

For native, `bid.adm` must be OpenRTB Native response JSON with a landing link and every required asset ID declared by `imp.native.request`.

Controlled no-bid is `204 No Content`. Malformed OpenRTB is `400 Bad Request`.

## Quickstart

Run the local bidder and post a sample OpenRTB request:

```bash
make test
make configcheck
make bench
make run
curl -i http://localhost:8080/readyz
curl -i -X POST http://localhost:8080/openrtb \
  -H 'Content-Type: application/json' \
  --data @samples/openrtb-video-request.json
```

Run with auth and request signatures:

```bash
export BIDDER_OPENRTB_AUTH_TOKEN='replace-me'
export BIDDER_OPENRTB_SIGNING_SECRET='replace-me'
export BIDDER_OPENRTB_REQUIRE_AUTH=true
export BIDDER_OPENRTB_REQUIRE_SIGNATURE=true
make run
```

For local helper tools, the bidder also accepts this simpler signature payload:

```text
<X-ClearLedger-Timestamp>.<raw JSON request body>
```

Production ClearLedger fanout uses the `X-ClearLedger-Buyer-*` header set.

## Docker

```bash
docker build -t clearledger-bidder-openrtb:local .
docker run --rm -p 8080:8080 clearledger-bidder-openrtb:local
```

For local container smoke testing:

```bash
docker compose up --build
```

Docker is optional; the bidder is a Go HTTP service and can run on any platform that supports the required network and secret-management controls.

## Campaign Configuration

Campaigns are local JSON in `config/campaigns.sample.json`. The hot path reads this manifest at startup and does not call ClearLedger, Redis, Supabase, payment systems, or settlement systems.

Each campaign can constrain:

- app ID, bundle, domain, placement, Deal ID, and geo
- media type and creative compatibility
- CPM bid, daily budget, pacing, and QPS
- creative approval, advertiser domain, landing URL, asset URL, and markup rendering
- request currency and PMP Deal currency
- media MIME type, duration, dimensions, and blocked advertiser domains
- COPPA, limited-ad-tracking, and privacy policy requirements

`make configcheck` fails fast on invalid local setup, including unsupported media types, duplicate campaign or creative IDs, negative QPS/duration/dimensions, missing approved creatives, and creatives that cannot serve the declared media type.

## Operations Endpoints

The bidder exposes production-shaped service endpoints:

- `GET /healthz`: process health and bidder identity
- `GET /readyz`: readiness, enabled campaign count, enabled media types, and auth/signing state
- `GET /metrics`: Prometheus metrics for bids, no-bids, malformed requests, notice callbacks, spend, budgets, QPS, and campaign state
- `GET /statez`: sanitized runtime state for campaign spend, pacing, QPS, media types, deal count, placement count, and approved creative count
- `GET|POST /events/{win|bill|loss|imp}`: local notice callback sink for bidder-side observability

`/statez` does not expose auth tokens, signing secrets, creative markup, or ClearLedger settlement data.

Generated notice URLs include auction, bid, buyer, campaign, creative, and echoed ClearLedger lane/package/placement/proof identifiers so operators can reconcile local bidder logs with ClearLedger-owned delivery proof.

`/readyz` returns `503` when no campaign is enabled or when required auth/signature secrets are missing. This allows load balancers and ClearLedger certification to reject unsafe deployments before live bid fanout.

## Runtime Tuning

The HTTP server has conservative defaults for RTB traffic:

- `BIDDER_HTTP_READ_HEADER_TIMEOUT_MS`, default `500`
- `BIDDER_HTTP_READ_TIMEOUT_MS`, default `2000`
- `BIDDER_HTTP_WRITE_TIMEOUT_MS`, default `2000`
- `BIDDER_HTTP_IDLE_TIMEOUT_MS`, default `60000`
- `BIDDER_HTTP_SHUTDOWN_TIMEOUT_MS`, default `5000`
- `BIDDER_MAX_REQUEST_BODY_BYTES`, default `262144`
- `BIDDER_MAX_HEADER_BYTES`, default `16384`

Keep `BIDDER_HTTP_WRITE_TIMEOUT_MS` above the largest ClearLedger buyer timeout plus network margin, and below the deployment platform timeout.

## ClearLedger Registration

After deployment, generate and submit the approved-buyer registration payload:

```bash
export CLEARLEDGER_REGISTER_URL='https://api.clearledger.org/v1/approved-buyers'
export CLEARLEDGER_API_KEY='...'
export BIDDER_PUBLIC_ENDPOINT='https://agency-bidder.example.com'
export BIDDER_OPENRTB_ENDPOINT='https://agency-bidder.example.com/openrtb'
go run ./cmd/bidder -config config/campaigns.sample.json -registration-payload
go run ./cmd/bidder -config config/campaigns.sample.json -register
```

`BIDDER_PUBLIC_ENDPOINT` is the public base URL used for generated notice URLs such as `/events/imp`. `BIDDER_OPENRTB_ENDPOINT` is the exact endpoint ClearLedger should call for bid requests. If `BIDDER_OPENRTB_ENDPOINT` is omitted, registration derives it as `<BIDDER_PUBLIC_ENDPOINT>/openrtb`.

Use `-registration-payload` to inspect or attach the approval payload without making a network call. The payload includes buyer identity, endpoint, OpenRTB contract, supported media, auth/signature requirements, certification checks, and safe operator endpoints. Use `-register` only when `CLEARLEDGER_REGISTER_URL` and `CLEARLEDGER_API_KEY` are configured for a real ClearLedger registration API.

## Endpoint Certification

Run the certification harness against a deployed endpoint before requesting ClearLedger approval:

```bash
go run ./cmd/certify \
  -endpoint https://agency-bidder.example.com/openrtb \
  -buyer-id agency_bidder_1 \
  -seat-id agency_seat_1 \
  -token "$BIDDER_OPENRTB_AUTH_TOKEN" \
  -signing-secret "$BIDDER_OPENRTB_SIGNING_SECRET"
```

The harness checks readiness, ClearLedger identity and signature headers, valid bid response shape for video, audio, display, and native samples, controlled no-bid, malformed request rejection, and OpenRTB bid-response validation.

Certification output is machine-readable JSON. It includes endpoint, contract name, buyer and seat identity, timeout, per-media HTTP status and latency, supported media, auth/signature coverage, readiness, controlled no-bid, malformed rejection, and maximum observed certification latency.

ClearLedger runs its own certification before adding an endpoint to the runtime manifest.

Run one sample when debugging a specific format:

```bash
go run ./cmd/certify \
  -endpoint https://agency-bidder.example.com/openrtb \
  -sample samples/openrtb-video-request.json
```

## ClearLedger Lane Harness

For local end-to-end compatibility without private ClearLedger services, run the ClearLedger-side harness. It simulates runtime manifest lookup, active lane enforcement, approved buyer routing, signed OpenRTB fanout, buyer timeout handling, bid/no-bid/error classification, bid validation, winner selection, supply response construction, and proof ownership reporting.

The harness reads `samples/clearledger-runtime-manifest.local.json`, applies lane floor, placement, app bundle, Deal ID, timeout, media format, and proof-extension rules, then emits proof steps showing the boundary between bidder-side decisioning and ClearLedger-owned delivery, billing, settlement, fee, payout, and receipt workflows.

```bash
export BIDDER_OPENRTB_AUTH_TOKEN='token'
export BIDDER_OPENRTB_SIGNING_SECRET='secret'
export BIDDER_OPENRTB_REQUIRE_AUTH=true
export BIDDER_OPENRTB_REQUIRE_SIGNATURE=true
make run

# In another shell:
make harness
```

## Performance

The bidder keeps the OpenRTB hot path in-process. Campaign config is loaded and compiled at startup, budget/QPS/pacing state is protected by a short mutex, and bid IDs are deterministic from auction ID, impression ID, campaign ID, and creative ID.

Local spend and QPS reservations are only committed for bids the bidder returns. Controlled no-bids do not consume local QPS.

Use `make bench` before changing auction logic. CI also runs `make bench-guard`, which fails if the reference video/PMP bid path crosses the configured regression threshold. The threshold is a regression tripwire, not a substitute for production load testing.

## Support and Commercial Services

This bidder is open source and can be self-hosted. ClearLedger offers commercial services for teams that want managed operation or faster production onboarding, including endpoint certification support, managed hosting, exchange connectivity, campaign migration, optimization, reporting, SLA support, creative QA, privacy/compliance review, and enterprise observability.

For onboarding or managed-service discussions, contact ClearLedger at `exchange@clearledger.org` or Tony, CEO of ClearLedger, at `tony@clearledger.org`.
