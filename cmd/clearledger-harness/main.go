package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Numi2/clearledger-bidder-openrtb/internal/clearledger"
)

func main() {
	var options clearledger.HarnessOptions
	var timeoutMS int
	flag.StringVar(&options.ManifestPath, "manifest", "samples/clearledger-runtime-manifest.local.json", "ClearLedger runtime manifest path")
	flag.StringVar(&options.PrivateMarketID, "private-market-id", "", "private market id to run")
	flag.StringVar(&options.BuyerID, "buyer-id", "", "buyer id to select")
	flag.StringVar(&options.SamplePath, "sample", "samples/openrtb-video-request.json", "OpenRTB sample request")
	flag.StringVar(&options.EndpointOverride, "endpoint", "", "override buyer endpoint")
	flag.StringVar(&options.AuthToken, "token", os.Getenv("BIDDER_OPENRTB_AUTH_TOKEN"), "buyer auth token")
	flag.StringVar(&options.SigningSecret, "signing-secret", os.Getenv("BIDDER_OPENRTB_SIGNING_SECRET"), "buyer signing secret")
	flag.StringVar(&options.OpenRTBVersion, "openrtb-version", getenv("BIDDER_OPENRTB_OUTBOUND_VERSION", ""), "override X-OpenRTB-Version header")
	flag.IntVar(&timeoutMS, "timeout-ms", 2000, "HTTP timeout")
	flag.Parse()
	options.Timeout = time.Duration(timeoutMS) * time.Millisecond

	report, err := clearledger.RunHarness(context.Background(), options)
	if err != nil {
		fmt.Fprintf(os.Stderr, "clearledger harness failed: %v\n", err)
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
