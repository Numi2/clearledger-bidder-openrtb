# ClearLedger Compatibility Contract

This document defines the runtime boundary between ClearLedger and an agency-operated open-source bidder. It is written for implementers, operators, and ClearLedger certification reviewers.

The bidder is intentionally narrow: it decides whether to bid on a signed OpenRTB opportunity. ClearLedger is intentionally authoritative: it controls the lane, validates the response, chooses the winner, returns the ad to supply, records delivery, archives evidence, computes billing and settlement, and produces final receipts.

## Integration Model

1. Supply enters ClearLedger from a publisher, app, SDK, ad server, or SSP path.
2. ClearLedger reads the active private-auction runtime manifest from Redis.
3. ClearLedger resolves the lane, package, placement, floor, Deal ID, media type, approved buyers, QPS/timeout controls, privacy rules, and creative requirements.
4. ClearLedger builds an OpenRTB 2.6-compatible request and signs the fanout request to the approved buyer endpoint.
5. The bidder validates the request, applies local campaign and creative rules, and returns either `204 No Content` or a valid OpenRTB bid response before `tmax`.
6. ClearLedger validates all bid responses, enforces the approved buyer route, selects the winner, and returns VAST/adm to the supply path.
7. Delivery tracking, evidence archive, billing proof, settlement proof, publisher net, ClearLedger fee, payout workflow, and final receipt generation remain outside the bidder.

## Buyer Endpoint

ClearLedger registers the bidder as an approved buyer endpoint and calls it with OpenRTB 2.6 JSON:

```http
POST https://agency-bidder.example.com/openrtb
Authorization: Bearer <buyer token>
X-OpenRTB-Version: 2.6
X-ClearLedger-Buyer-Timestamp: 2026-06-04T12:00:00.000000000Z
X-ClearLedger-Auction-ID: auction_123
X-ClearLedger-Request-ID: auction_123
X-ClearLedger-Buyer-Body-SHA256: <lowercase hex sha256 raw body>
X-ClearLedger-Buyer-Signature: hmac-sha256=<lowercase hex hmac>
Content-Type: application/json
```

Signature base:

```text
<timestamp>
<auction_id>
<request_id>
<body_sha256>
```

The bidder also accepts the simpler local helper headers `X-ClearLedger-Timestamp` and `X-ClearLedger-Signature`, but production ClearLedger fanout uses the buyer header set above.

After signature verification, the bidder enforces header/body consistency. `X-ClearLedger-Request-ID` must match OpenRTB `id`, `X-ClearLedger-Auction-ID` must match `source.tid` when present or otherwise `id`, and `X-ClearLedger-Buyer-ID` / `X-ClearLedger-Seat-ID` must match the configured bidder identity when those headers are present. A mismatch is treated as a bad ClearLedger request and rejected before local campaign rules run.

## ClearLedger Responsibilities

ClearLedger remains the transaction authority. It reads the Redis runtime manifest, enforces active lanes and approved buyers, signs requests, validates bid responses, selects winners, returns VAST/adm to supply, tracks delivery events, archives evidence, materializes billing/settlement rows, computes publisher net and ClearLedger fee, and produces final receipts.

ClearLedger certification should prove that the endpoint can be safely called under the active lane contract, that invalid or late responses cannot win, and that no bidder-side state is treated as settlement truth.

## Bidder Responsibilities

The open-source bidder validates requests, applies local campaign rules, returns `204 No Content` for controlled no-bid, or returns an OpenRTB response with matching `id`, explicit response `cur`, `seatbid.seat`, non-empty bid arrays, unique `bid.id`, one bid per impression, positive CPM `price`, `crid`, non-empty `adomain`, PMP `dealid`, media-appropriate `adm`, absolute `http` or `https` notice URLs when present, and `ext.clearledger` identifiers. Request validation rejects malformed timing, floor, dimension, duration, and native request bounds before campaign rules run. For video and audio bids, VAST must include an impression, duration, media file, MIME type compatible with the request, and duration within the requested bounds. For native bids, `imp.native.request` must be parseable OpenRTB Native JSON with unique positive asset IDs, and `bid.adm` must include every asset marked required by the request. When ClearLedger sends proof correlation fields under `imp.ext.clearledger`, the bidder echoes safe lane/package/placement/proof identifiers in `bid.ext.clearledger` and notice URLs for reconciliation only. If `receipt_required` is true, the shared validator requires the echoed proof fields to match. ClearLedger also checks `bid.ext.clearledger.buyer_id` against the approved buyer route before winner selection. ClearLedger still owns all delivery, billing, settlement, and final receipt authority.

The bidder must not depend on ClearLedger settlement APIs in the hot path. Campaign matching, pacing, QPS checks, budget reservation, and creative selection are local runtime decisions. Generated notice URLs are useful for bidder-side observability, but ClearLedger impression tracking is the billable source of truth.

It also exposes operator endpoints for certification and debugging:

- `/healthz` and `/readyz` for liveness and readiness.
- `/metrics` for Prometheus-compatible counters and campaign gauges.
- `/statez` for sanitized campaign runtime state.
- `/events/{win|bill|loss|imp}` as a local notice sink.

Readiness is intentionally strict: if bearer auth or request signatures are required but not configured, `/readyz` returns unavailable and certification should fail before ClearLedger sends live OpenRTB fanout.

These endpoints help an agency operate the bidder, but they do not replace ClearLedger impression tracking, evidence archive, billing, settlement, publisher net, ClearLedger fee, payout, or final receipt systems.

## Certification Flow

Agency/server operator:

1. Deploy this bidder with a public HTTPS endpoint.
2. Configure `BIDDER_OPENRTB_AUTH_TOKEN` and `BIDDER_OPENRTB_SIGNING_SECRET`.
3. Run `go run ./cmd/certify -endpoint https://agency-bidder.example.com/openrtb`.
4. Confirm `/readyz`, `/metrics`, and `/statez` expose safe operator state.
5. Submit the registration payload shape from `samples/clearledger-approved-buyer-registration.json`.

ClearLedger operator:

1. Add the endpoint to the active private-auction runtime manifest as an approved buyer.
2. Publish the manifest to Redis.
3. Run buyer certification against the endpoint.
4. Observe live fanout evidence: bid request, bid response/no-bid, invalid/timeout classification where applicable, winner selection, VAST/adm return, impression tracking, evidence archive, rollups, settlement proof, and final receipt creation.

## Local ClearLedger Harness

`cmd/clearledger-harness` is a local compatibility harness, not a replacement for ClearLedger production certification. It verifies the same boundary with no Redis/Supabase dependency:

1. Reads a ClearLedger-style private-auction runtime manifest.
2. Finds an active lane and selected approved buyer.
3. Applies lane floor, placement, app bundle, Deal ID, and proof extensions to the OpenRTB request.
4. Signs the buyer request with the production `X-ClearLedger-Buyer-*` headers.
5. Enforces each approved buyer's route constraints, including OpenRTB protocol, allowed formats, and buyer timeout from the runtime manifest.
6. Classifies every approved buyer as skipped, bid, no-bid, invalid bid, HTTP error, timeout, or transport error.
7. Validates response id, seat, impid, price/floor, explicit response currency, approved buyer proof identity, crid, adomain, dealid, media-appropriate VAST/display/native `adm`, native required assets, VAST MIME/duration/dimension constraints, and notice/proof fields.
8. Selects the highest valid bid and builds the supply VAST/adm response.
9. Emits delivery proof and proof steps marking delivery tracking, evidence archive, billing, settlement, publisher net, ClearLedger fee, and final receipt authority as ClearLedger-owned and outside the bidder.

Example:

```bash
go run ./cmd/clearledger-harness \
  -manifest samples/clearledger-runtime-manifest.local.json \
  -private-market-id pm_cert \
  -buyer-id agency_bidder_1 \
  -token "$BIDDER_OPENRTB_AUTH_TOKEN" \
  -signing-secret "$BIDDER_OPENRTB_SIGNING_SECRET"
```
