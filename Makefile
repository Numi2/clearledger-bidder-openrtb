.PHONY: test run certify harness docker

test:
	go test ./...

run:
	go run ./cmd/bidder -config config/campaigns.sample.json

certify:
	go run ./cmd/certify -endpoint http://localhost:8080/openrtb

harness:
	go run ./cmd/clearledger-harness -manifest samples/clearledger-runtime-manifest.local.json -private-market-id pm_cert -buyer-id agency_bidder_1

docker:
	docker build -t clearledger-bidder-openrtb:local .
