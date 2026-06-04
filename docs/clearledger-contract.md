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

## ClearLedger Responsibilities

ClearLedger remains the transaction authority. It reads the Redis runtime manifest, enforces active lanes and approved buyers, signs requests, validates bid responses, selects winners, returns VAST/adm to supply, tracks delivery events, archives evidence, materializes billing/settlement rows, computes publisher net and ClearLedger fee, and produces final receipts.

## Bidder Responsibilities

The open-source bidder validates requests, applies local campaign rules, returns `204 No Content` for controlled no-bid, or returns an OpenRTB response with matching `id`, `seatbid.seat`, `bid.id`, `bid.impid`, CPM `price`, `crid`, `adomain`, PMP `dealid`, `adm`, notices, and `ext.clearledger` identifiers.

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
5. Validates response id, seat, impid, price/floor, currency, crid, adomain, dealid, VAST/adm, and notice/proof fields.
6. Selects the winning bid and builds the supply VAST/adm response.
7. Emits proof steps marking delivery tracking, billing, settlement, publisher net, ClearLedger fee, and final receipt authority as ClearLedger-owned and outside the bidder.

Example:

```bash
go run ./cmd/clearledger-harness \
  -manifest samples/clearledger-runtime-manifest.local.json \
  -private-market-id pm_cert \
  -buyer-id agency_bidder_1 \
  -token "$BIDDER_OPENRTB_AUTH_TOKEN" \
  -signing-secret "$BIDDER_OPENRTB_SIGNING_SECRET"
```
