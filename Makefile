.PHONY: test run docker

test:
	go test ./...

run:
	go run ./cmd/bidder -config config/campaigns.sample.json

docker:
	docker build -t clearledger-bidder-openrtb:local .
