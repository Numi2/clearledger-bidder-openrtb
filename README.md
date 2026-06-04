# ClearLedger Bidder OpenRTB

Open-source reference bidder for ClearLedger approved-buyer endpoints. It runs as a standalone HTTP service, accepts OpenRTB 2.6-compatible JSON bid requests, evaluates local campaign/budget/creative rules, and returns either `204 No Content` or a valid OpenRTB bid response before `tmax`.

This repository is only the bidder runtime. ClearLedger clearing, receipts, billing proof, settlement proof, publisher net, ClearLedger fee, payout workflow, marketplace/operator workflow, and final receipt network remain proprietary ClearLedger infrastructure.

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
- floor and currency through `imp.bidfloor` / `imp.bidfloorcur`
- PMP Deal ID through `imp.pmp.deals[].id` when ClearLedger sends private-auction inventory
- optional `source.ext.schain`, `regs`, `device`, `user`, and `imp.ext.clearledger` proof fields

Bid responses include:

- response `id` matching the request
- `seatbid[].seat`
- `bid.id`, `bid.impid`, `bid.price`, `bid.crid`, `bid.adomain`
- `bid.dealid` for PMP inventory
- `bid.adm` containing VAST for video/audio, display markup for banner, or OpenRTB native response JSON for native
- `nurl`, `burl`, `lurl` notice URLs
- `bid.ext.clearledger` with buyer/campaign/creative identifiers plus echoed ClearLedger lane/package/placement/proof fields when present in `imp.ext.clearledger`

When `imp.ext.clearledger.receipt_required` is true, the validator requires `bid.ext.clearledger` to include buyer, campaign, and creative IDs and to echo any ClearLedger lane/package/placement/proof fields from the request.

For video and audio, `bid.adm` must be parseable VAST with an impression, duration, and media file. When the request declares media constraints, the validator checks VAST duration against `minduration`/`maxduration`, checks `MediaFile type` against requested `mimes`, and checks video media dimensions when both the request and VAST provide dimensions.

No-bid is `204 No Content`. Malformed OpenRTB is `400`.

## Quickstart

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
- request currency, PMP Deal currency, media MIME compatibility, blocked advertiser domains, COPPA, and limited-ad-tracking constraints

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

Register the endpoint after deployment:

```bash
export CLEARLEDGER_REGISTER_URL='https://api.clearledger.org/v1/approved-buyers'
export CLEARLEDGER_API_KEY='...'
export BIDDER_PUBLIC_ENDPOINT='https://agency-bidder.example.com/openrtb'
go run ./cmd/bidder -config config/campaigns.sample.json -register
```

## Endpoint Certification

Run the certification harness against a deployed endpoint before asking ClearLedger to approve it:

```bash
go run ./cmd/certify \
  -endpoint https://agency-bidder.example.com/openrtb \
  -token "$BIDDER_OPENRTB_AUTH_TOKEN" \
  -signing-secret "$BIDDER_OPENRTB_SIGNING_SECRET"
```

The harness checks readiness, production ClearLedger signature headers, valid bid response shape for video, audio, display, and native samples, controlled no-bid, malformed request rejection, and OpenRTB bid-response validation including VAST MIME, duration, and dimension constraints.

Run one sample only when debugging a specific format:

```bash
go run ./cmd/certify \
  -endpoint https://agency-bidder.example.com/openrtb \
  -sample samples/openrtb-video-request.json
```

## ClearLedger Lane Harness

For local end-to-end compatibility without private ClearLedger services, run the ClearLedger-side harness. It reads `samples/clearledger-runtime-manifest.local.json`, enforces the active lane and approved buyer route, skips buyers whose protocol or allowed formats do not match the request, applies each buyer's manifest timeout, signs OpenRTB fanout, classifies each buyer as skipped/bid/no-bid/invalid/error, validates bid responses, selects the highest valid bid, builds the VAST/adm supply response, and emits proof steps showing that delivery tracking, billing, settlement, publisher net, ClearLedger fee, and final receipts stay outside the bidder.

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

The bidder keeps the OpenRTB hot path in-process: campaign config is loaded and compiled at startup, budget/QPS/pacing state is protected by a short mutex, and bid IDs are deterministic from auction ID, impression ID, campaign ID, and creative ID. Use `make bench` to run the hot-path benchmark before changing auction logic.

## User Flow

This does not require a bidder website. The open-source bidder is an HTTP service plus CLI tools because the runtime path must stay small and low-latency. Agencies can build their own UI on top of the JSON config and endpoints if they want one, but the supported user flow is CLI/API first:

1. Configure local campaigns in JSON.
2. Deploy the bidder on any HTTPS-capable server.
3. Run `cmd/certify` against the public endpoint.
4. Optionally run `cmd/clearledger-harness` with a ClearLedger-style runtime manifest for local lane proof.
5. Submit the approved-buyer registration payload to ClearLedger.
6. ClearLedger publishes the approved buyer in the Redis runtime manifest and starts signed OpenRTB fanout.
7. Monitor `/readyz`, `/metrics`, `/statez`, and notice callback counts while ClearLedger owns delivery proof, billable events, settlement, publisher net, fee computation, payout workflow, and final receipts.

If you want ClearLedger to configure this bidder for your campaigns, rules, creatives, and buying workflow, contact your success representative or Tony, CEO of ClearLedger, at `tony@clearledger.org` or `+1 (832) 696-9666`.

## Compose

For local container smoke testing:

```bash
docker compose up --build
```

Docker is optional; the bidder is just a Go HTTP process.
