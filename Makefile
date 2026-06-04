.PHONY: test configcheck bench run certify harness docker

test:
	go test ./...

configcheck:
	go run ./cmd/configcheck -config config/campaigns.sample.json

bench:
	go test ./internal/bidder -bench BenchmarkEngineBidVideoPMP -benchmem

run:
	go run ./cmd/bidder -config config/campaigns.sample.json

certify:
	go run ./cmd/certify -endpoint http://localhost:8080/openrtb -token "$$BIDDER_OPENRTB_AUTH_TOKEN" -signing-secret "$$BIDDER_OPENRTB_SIGNING_SECRET"

harness:
	go run ./cmd/clearledger-harness -manifest samples/clearledger-runtime-manifest.local.json -private-market-id pm_cert -buyer-id agency_bidder_1 -token "$$BIDDER_OPENRTB_AUTH_TOKEN" -signing-secret "$$BIDDER_OPENRTB_SIGNING_SECRET"

docker:
	docker build -t clearledger-bidder-openrtb:local .
