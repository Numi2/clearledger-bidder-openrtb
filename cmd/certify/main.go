package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Numi2/clearledger-bidder-openrtb/internal/certify"
)

func main() {
	var options certify.Options
	var timeoutMS int
	var samples string
	flag.StringVar(&options.Endpoint, "endpoint", defaultEndpoint(), "OpenRTB endpoint to certify")
	flag.StringVar(&options.Token, "token", os.Getenv("BIDDER_OPENRTB_AUTH_TOKEN"), "Bearer token")
	flag.StringVar(&options.SigningSecret, "signing-secret", os.Getenv("BIDDER_OPENRTB_SIGNING_SECRET"), "HMAC signing secret")
	flag.StringVar(&options.BuyerID, "buyer-id", os.Getenv("BIDDER_BUYER_ID"), "ClearLedger buyer id header to certify")
	flag.StringVar(&options.SeatID, "seat-id", os.Getenv("BIDDER_SEAT"), "ClearLedger seat id header to certify")
	flag.StringVar(&options.OpenRTBVersion, "openrtb-version", getenv("BIDDER_OPENRTB_OUTBOUND_VERSION", "2.6"), "X-OpenRTB-Version header for certification requests")
	flag.StringVar(&samples, "samples", "", "comma-separated OpenRTB sample request paths")
	flag.StringVar(&options.SamplePath, "sample", "", "single OpenRTB request path")
	flag.IntVar(&timeoutMS, "timeout-ms", 2000, "HTTP timeout per certification request")
	flag.Parse()
	if samples != "" {
		for _, sample := range strings.Split(samples, ",") {
			if strings.TrimSpace(sample) != "" {
				options.SamplePaths = append(options.SamplePaths, strings.TrimSpace(sample))
			}
		}
	}
	options.Timeout = time.Duration(timeoutMS) * time.Millisecond

	report, err := certify.Run(context.Background(), options)
	if err != nil {
		fmt.Fprintf(os.Stderr, "certification failed: %v\n", err)
		os.Exit(2)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
	if !report.OK {
		os.Exit(1)
	}
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func defaultEndpoint() string {
	if value := strings.TrimSpace(os.Getenv("BIDDER_OPENRTB_ENDPOINT")); value != "" {
		return value
	}
	value := strings.TrimRight(strings.TrimSpace(os.Getenv("BIDDER_PUBLIC_ENDPOINT")), "/")
	if value == "" {
		return ""
	}
	if strings.HasSuffix(value, "/openrtb") {
		return value
	}
	return value + "/openrtb"
}
