# ClearLedger Bidder OpenRTB

Open-source reference bidder for ClearLedger approved-buyer endpoints. It runs as a standalone HTTP service, accepts OpenRTB 2.6-compatible JSON bid requests, evaluates local campaign/budget/creative rules, and returns either `204 No Content` or a valid OpenRTB bid response before `tmax`.

This repository is only the bidder runtime. ClearLedger clearing, receipts, billing proof, settlement proof, publisher net, ClearLedger fee, payout workflow, marketplace/operator workflow, and final receipt network remain proprietary ClearLedger infrastructure.

## Runtime Contract

ClearLedger calls:

```http
POST /openrtb
Authorization: Bearer <token>
X-ClearLedger-Timestamp: <unix_seconds>
X-ClearLedger-Signature: sha256=<hmac_sha256(timestamp + "." + raw_body)>
Content-Type: application/json
```

`/buyers/{buyer}/openrtb` is also supported for deployments that route multiple approved buyer names through one host.

The request must include:

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
- `bid.adm` containing VAST, display markup, or native markup
- `nurl`, `burl`, `lurl` notice URLs
- `bid.ext.clearledger` with buyer/campaign/creative identifiers

No-bid is `204 No Content`. Malformed OpenRTB is `400`.

## Quickstart

```bash
make test
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

The signature payload is:

```text
<X-ClearLedger-Timestamp>.<raw JSON request body>
```

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
- basic privacy handling such as COPPA and limited-ad-tracking constraints

## ClearLedger Registration Mode

Register the endpoint after deployment:

```bash
export CLEARLEDGER_REGISTER_URL='https://api.clearledger.org/v1/approved-buyers'
export CLEARLEDGER_API_KEY='...'
export BIDDER_PUBLIC_ENDPOINT='https://agency-bidder.example.com/openrtb'
go run ./cmd/bidder -config config/campaigns.sample.json -register
```

ClearLedger will still certify the endpoint, enforce the approved buyer lane, validate bid responses, select winners, return VAST/adm to supply, track delivery, and handle all billable/settlement/final receipt proof outside this bidder.
