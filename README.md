# ClearLedger Bidder OpenRTB

Open-source reference bidder for ClearLedger approved-buyer endpoints. It runs as a standalone HTTP service, accepts OpenRTB 2.6-compatible JSON bid requests, evaluates local campaign/budget/creative rules, and returns either `204 No Content` or a valid OpenRTB bid response before `tmax`.

The design goal is simple: demand can run anywhere, while ClearLedger remains the transaction authority. Agencies, brands, and buying teams can operate this bidder on their own infrastructure, connect campaigns and creatives locally, and register the endpoint with ClearLedger as an approved buyer. ClearLedger continues to own signed fanout, lane enforcement, winner validation, delivery proof, billing proof, settlement proof, publisher net, ClearLedger fee, payout workflow, and final receipt production.

This repository is only the bidder runtime and its local operator tooling. It intentionally does not include the ClearLedger ad server, clearing system, receipt network, marketplace, operator workflow, payment workflow, or settlement infrastructure.

## What You Get

- Production-shaped OpenRTB HTTP endpoint: `POST /openrtb`.
- Optional multi-buyer route shape: `POST /buyers/{buyer}/openrtb`.
- Local campaign, budget, pacing, QPS, placement, Deal ID, privacy, and creative rule evaluation.
- VAST, display, and OpenRTB Native markup generation and validation helpers.
- ClearLedger-compatible auth, request signing, proof extensions, notice URLs, and registration payloads.
- Health, readiness, Prometheus metrics, sanitized state, and local notice callback endpoints.
- Certification and ClearLedger lane harnesses that prove the bidder can run standalone and connect to a ClearLedger approved-buyer lane.
- Docker, Compose, sample OpenRTB requests/responses, sample campaign config, and CI tests.

## What Stays In ClearLedger

ClearLedger remains responsible for everything that makes the transaction provable and payable:

- auction router, Redis runtime manifest, lane/package/placement enforcement, and approved buyer fanout
- bid-response validation, winner selection, VAST/adm return to supply, and delivery tracking
- raw evidence archive, receipt event processing, billing proof, settlement proof, publisher net, ClearLedger fee, payout workflow, and final receipt creation

The bidder decides whether to bid. ClearLedger proves what delivered and what is billable.

## Supported User Flow

This does not require a bidder website. The supported flow is CLI/API first because the RTB hot path should stay small, auditable, and low-latency:

1. Configure campaigns, budgets, targeting rules, and creatives in local JSON.
2. Run `make test`, `make configcheck`, and `make bench`.
3. Deploy the bidder on any HTTPS-capable server.
4. Run `cmd/certify` against the public endpoint.
5. Optionally run `cmd/clearledger-harness` with a ClearLedger-style runtime manifest for local lane proof.
6. Submit the approved-buyer registration payload to ClearLedger.
7. ClearLedger publishes the approved buyer in its runtime manifest and starts signed OpenRTB fanout.
8. Monitor `/readyz`, `/metrics`, `/statez`, and notice callback counts while ClearLedger owns billable delivery, settlement, payout, and final receipts.

Agencies can build their own UI on top of the JSON config and operator endpoints if they want one. The open-source runtime itself stays focused on the programmatic decision path.

If you want ClearLedger to configure this bidder for your campaigns, rules, creatives, and buying workflow, contact your success representative or Tony, CEO of ClearLedger, at `tony@clearledger.org` or `+1 (832) 696-9666`.

## Runtime Contract

ClearLedger calls:

```http
POST /openrtb
Authorization: Bearer <token>
X-ClearLedger-Buyer-Timestamp: <rfc3339nano>
X-ClearLedger-Auction-ID: <auction id>
X-ClearLedger-Request-ID: <request id>
X-ClearLedger-Buyer-Body-SHA256: <sha256 raw body>
X-ClearLedger-Buyer-Signature: hmac-sha256=<hmac_sha256 signature>
Content-Type: application/json
```

`/buyers/{buyer}/openrtb` is also supported for deployments that route multiple approved buyer names through one host.

The production signature base is documented in `docs/clearledger-contract.md`. The request must include:

- `id`, `tmax`, `cur`
- exactly one of `site` or `app`
- one or more `imp` objects with exactly one media object: `banner`, `video`, `audio`, or `native`
- non-negative `tmax`, floor, media dimensions, and duration bounds; when both duration bounds are set, minimum cannot exceed maximum
- for native inventory, `imp.native.request` must be parseable OpenRTB Native JSON with unique positive asset IDs
- floor and currency through `imp.bidfloor` / `imp.bidfloorcur`
- PMP Deal ID through `imp.pmp.deals[].id` when ClearLedger sends private-auction inventory
- optional `source.ext.schain`, `regs`, `device`, `user`, and `imp.ext.clearledger` proof fields

Bid responses include:

- response `id` matching the request
- `seatbid[].seat`
- `bid.id`, matching `bid.impid`, positive CPM `bid.price`, `bid.crid`, and non-empty `bid.adomain` entries
- non-empty `seatbid[].bid`, unique `bid.id`, and at most one bid per impression in a single response
- `bid.dealid` matching the bid's own impression Deal ID for PMP inventory
- `bid.adm` containing VAST for video/audio, display markup for banner, or OpenRTB native response JSON for native
- optional `nurl`, `burl`, `lurl` notice URLs, which must be absolute `http` or `https` URLs when present
- `bid.ext.clearledger` with buyer/campaign/creative identifiers plus echoed ClearLedger lane/package/placement/proof fields when present in `imp.ext.clearledger`

When `imp.ext.clearledger.receipt_required` is true, the validator requires `bid.ext.clearledger` to include buyer, campaign, and creative IDs and to echo any ClearLedger lane/package/placement/proof fields from the request.

For video and audio, `bid.adm` must be parseable VAST with an impression, duration, and media file. When the request declares media constraints, the validator checks VAST duration against `minduration`/`maxduration`, checks `MediaFile type` against requested `mimes`, and checks video media dimensions when both the request and VAST provide dimensions. For native, `bid.adm` must be OpenRTB Native response JSON with a landing link and must include every asset ID marked required in `imp.native.request`; the built-in native renderer uses those required asset IDs when it builds the response.

No-bid is `204 No Content`. Malformed OpenRTB is `400`.

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

With auth/signatures:

```bash
export BIDDER_OPENRTB_AUTH_TOKEN='replace-me'
export BIDDER_OPENRTB_SIGNING_SECRET='replace-me'
export BIDDER_OPENRTB_REQUIRE_AUTH=true
export BIDDER_OPENRTB_REQUIRE_SIGNATURE=true
make run
```

For local helper tools, the bidder also accepts the simpler signature payload:

```text
<X-ClearLedger-Timestamp>.<raw JSON request body>
```

Production ClearLedger fanout uses the `X-ClearLedger-Buyer-*` header set, not the local helper header set.

When ClearLedger request headers are present, the bidder checks that `X-ClearLedger-Request-ID` matches OpenRTB `id`, `X-ClearLedger-Auction-ID` matches `source.tid` or `id`, and buyer/seat headers match the configured `buyer_id` and `seat`. Mismatches are rejected before campaign evaluation.

## Docker

```bash
docker build -t clearledger-bidder-openrtb:local .
docker run --rm -p 8080:8080 clearledger-bidder-openrtb:local
```

## Campaign Config

Campaigns are local JSON in `config/campaigns.sample.json`. The hot path reads this manifest at startup and does not call ClearLedger, Redis, Supabase, payment systems, or settlement systems.

Each campaign can constrain:

- app ID, bundle, domain, placement, Deal ID, geo, media type
- CPM bid, daily budget, QPS
- creative approval, advertiser domain, landing URL, asset URL, VAST/display/native rendering
- request currency, PMP Deal currency, media MIME compatibility, creative duration, media dimensions, blocked advertiser domains, COPPA, and limited-ad-tracking constraints

`make configcheck` fails fast on invalid local setup, including unsupported media types, duplicate campaign or creative IDs, negative QPS/duration/dimensions, missing approved creatives, and creatives that cannot serve the declared media type.

## Operations Endpoints

The bidder exposes production-shaped service endpoints:

- `GET /healthz`: process health and bidder identity.
- `GET /readyz`: readiness, enabled campaign count, enabled media types, and whether auth/signing are configured.
- `GET /metrics`: Prometheus text metrics for bids, no-bids, malformed requests, notice callbacks, spend, budget, QPS, and campaign enabled state.
- `GET /statez`: sanitized runtime state for campaign spend, pacing, QPS, media types, deal count, placement count, and approved creative count. It does not return auth tokens, signing secrets, creative markup, or ClearLedger settlement data.
- `GET|POST /events/{win|bill|loss|imp}`: local notice callback sink for bidder-side observability. ClearLedger remains the billable impression and receipt authority.

Generated notice URLs include auction, bid, buyer, campaign, creative, and any echoed ClearLedger lane/package/placement/proof identifiers so operators can reconcile local bidder logs with ClearLedger-owned delivery proof.

`/readyz` returns `503` when no campaign is enabled or when required auth/signature secrets are missing. That lets load balancers and ClearLedger certification catch an unsafe deployment before live bid fanout.

## Runtime Tuning

The HTTP server has conservative defaults for RTB traffic:

- `BIDDER_HTTP_READ_HEADER_TIMEOUT_MS` default `500`
- `BIDDER_HTTP_READ_TIMEOUT_MS` default `2000`
- `BIDDER_HTTP_WRITE_TIMEOUT_MS` default `2000`
- `BIDDER_HTTP_IDLE_TIMEOUT_MS` default `60000`
- `BIDDER_HTTP_SHUTDOWN_TIMEOUT_MS` default `5000`
- `BIDDER_MAX_REQUEST_BODY_BYTES` default `262144`
- `BIDDER_MAX_HEADER_BYTES` default `16384`

Keep `BIDDER_HTTP_WRITE_TIMEOUT_MS` above the largest ClearLedger buyer timeout plus network margin, but below the deployment platform timeout.

## ClearLedger Registration Mode

After deployment, generate and submit the approved-buyer registration payload:

```bash
export CLEARLEDGER_REGISTER_URL='https://api.clearledger.org/v1/approved-buyers'
export CLEARLEDGER_API_KEY='...'
export BIDDER_PUBLIC_ENDPOINT='https://agency-bidder.example.com'
export BIDDER_OPENRTB_ENDPOINT='https://agency-bidder.example.com/openrtb'
go run ./cmd/bidder -config config/campaigns.sample.json -register
```

`BIDDER_PUBLIC_ENDPOINT` is the public base URL used for generated notice URLs such as `/events/imp`. `BIDDER_OPENRTB_ENDPOINT` is the exact endpoint ClearLedger should call for bid requests. If `BIDDER_OPENRTB_ENDPOINT` is omitted, registration derives it as `<BIDDER_PUBLIC_ENDPOINT>/openrtb`.

## Endpoint Certification

Run the certification harness against a deployed endpoint before asking ClearLedger to approve it. This is the agency/operator preflight check:

```bash
go run ./cmd/certify \
  -endpoint https://agency-bidder.example.com/openrtb \
  -buyer-id agency_bidder_1 \
  -seat-id agency_seat_1 \
  -token "$BIDDER_OPENRTB_AUTH_TOKEN" \
  -signing-secret "$BIDDER_OPENRTB_SIGNING_SECRET"
```

The harness checks readiness, production ClearLedger identity/signature headers, valid bid response shape for video, audio, display, and native samples, controlled no-bid, malformed request rejection, and OpenRTB bid-response validation including response currency, approved buyer identity proof, native required assets, VAST MIME, duration, and dimension constraints.

Run one sample only when debugging a specific format:

```bash
go run ./cmd/certify \
  -endpoint https://agency-bidder.example.com/openrtb \
  -sample samples/openrtb-video-request.json
```

## ClearLedger Lane Harness

For local end-to-end compatibility without private ClearLedger services, run the ClearLedger-side harness. It simulates the production boundary from the ClearLedger side: runtime manifest lookup, active lane enforcement, approved buyer routing, signed OpenRTB fanout, buyer timeout handling, bid/no-bid/error classification, bid validation, winner selection, supply response construction, and proof ownership reporting.

The harness reads `samples/clearledger-runtime-manifest.local.json`, applies lane floor, placement, app bundle, Deal ID, timeout, media format, and proof-extension rules, then emits proof steps showing that delivery tracking, billing, settlement, publisher net, ClearLedger fee, and final receipts stay outside the bidder.

```bash
export BIDDER_OPENRTB_AUTH_TOKEN='token'
export BIDDER_OPENRTB_SIGNING_SECRET='secret'
export BIDDER_OPENRTB_REQUIRE_AUTH=true
export BIDDER_OPENRTB_REQUIRE_SIGNATURE=true
make run

# In another shell:
make harness
```

ClearLedger will still certify the endpoint, enforce the approved buyer lane, validate bid responses, select winners, return VAST/adm to supply, track delivery, and handle all billable/settlement/final receipt proof outside this bidder.

## Performance Posture

The bidder keeps the OpenRTB hot path in-process: campaign config is loaded and compiled at startup, budget/QPS/pacing state is protected by a short mutex, and bid IDs are deterministic from auction ID, impression ID, campaign ID, and creative ID. Local spend and QPS reservations are only committed for bids the bidder actually returns; controlled no-bids do not consume local QPS. Use `make bench` to run the hot-path benchmark before changing auction logic.

## Compose

For local container smoke testing:

```bash
docker compose up --build
```

Docker is optional; the bidder is just a Go HTTP process.
