.PHONY: test configcheck bench bench-guard run certify harness docker

test:
	go test ./...

configcheck:
	go run ./cmd/configcheck -config config/campaigns.sample.json

bench:
	go test ./internal/bidder -bench BenchmarkEngineBidVideoPMP -benchmem

bench-guard:
	go test ./internal/bidder -run '^$$' -bench BenchmarkEngineBidVideoPMP -benchmem -benchtime=1000x | tee /tmp/clearledger-bidder-bench.txt
	awk 'BEGIN { limit = 250000 } /BenchmarkEngineBidVideoPMP/ { for (i = 1; i <= NF; i++) if ($$i == "ns/op") ns = $$(i-1) } END { if (ns == 0) { print "missing benchmark ns/op"; exit 2 } if (ns > limit) { printf "benchmark regression: %.0f ns/op > %.0f ns/op\n", ns, limit; exit 1 } printf "benchmark ok: %.0f ns/op <= %.0f ns/op\n", ns, limit }' /tmp/clearledger-bidder-bench.txt

run:
	go run ./cmd/bidder -config config/campaigns.sample.json

certify:
	go run ./cmd/certify -endpoint http://localhost:8080/openrtb -token "$$BIDDER_OPENRTB_AUTH_TOKEN" -signing-secret "$$BIDDER_OPENRTB_SIGNING_SECRET"

harness:
	go run ./cmd/clearledger-harness -manifest samples/clearledger-runtime-manifest.local.json -private-market-id pm_cert -buyer-id agency_bidder_1 -token "$$BIDDER_OPENRTB_AUTH_TOKEN" -signing-secret "$$BIDDER_OPENRTB_SIGNING_SECRET"

docker:
	docker build -t clearledger-bidder-openrtb:local .
