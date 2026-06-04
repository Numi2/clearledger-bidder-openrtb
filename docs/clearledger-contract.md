# ClearLedger Compatibility Contract

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

## Bidder Responsibilities

The open-source bidder validates requests, applies local campaign rules, returns `204 No Content` for controlled no-bid, or returns an OpenRTB response with matching `id`, `seatbid.seat`, non-empty bid arrays, unique `bid.id`, one bid per impression, positive CPM `price`, `crid`, non-empty `adomain`, PMP `dealid`, media-appropriate `adm`, absolute `http` or `https` notice URLs when present, and `ext.clearledger` identifiers. Request validation rejects malformed timing, floor, dimension, and duration bounds before campaign rules run. For video and audio bids, VAST must include an impression, duration, media file, MIME type compatible with the request, and duration within the requested bounds. When ClearLedger sends proof correlation fields under `imp.ext.clearledger`, the bidder echoes safe lane/package/placement/proof identifiers in `bid.ext.clearledger` and notice URLs for reconciliation only. If `receipt_required` is true, the shared validator requires the echoed proof fields to match. ClearLedger still owns all delivery, billing, settlement, and final receipt authority.

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
4. Submit the registration payload shape from `samples/clearledger-approved-buyer-registration.json`.

ClearLedger operator:

1. Add the endpoint to the active private-auction runtime manifest as an approved buyer.
2. Publish the manifest to Redis.
3. Run buyer certification against the endpoint.
4. Observe live fanout evidence: bid request, bid response/no-bid, winner selection, VAST/adm return, impression tracking, evidence archive, rollups, settlement proof, and final receipt creation.

## Local ClearLedger Harness

`cmd/clearledger-harness` is a local compatibility harness, not a replacement for ClearLedger production certification. It verifies the same boundary with no Redis/Supabase dependency:

1. Reads a ClearLedger-style private-auction runtime manifest.
2. Finds an active lane and selected approved buyer.
3. Applies lane floor, placement, app bundle, Deal ID, and proof extensions to the OpenRTB request.
4. Signs the buyer request with the production `X-ClearLedger-Buyer-*` headers.
5. Enforces each approved buyer's route constraints, including OpenRTB protocol, allowed formats, and buyer timeout from the runtime manifest.
6. Classifies every approved buyer as skipped, bid, no-bid, invalid bid, HTTP error, timeout, or transport error.
7. Validates response id, seat, impid, price/floor, currency, crid, adomain, dealid, media-appropriate VAST/display/native `adm`, VAST MIME/duration/dimension constraints, and notice/proof fields.
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
