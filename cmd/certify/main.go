package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Numi2/clearledger-bidder-openrtb/internal/certify"
)

func main() {
	var options certify.Options
	var timeoutMS int
	flag.StringVar(&options.Endpoint, "endpoint", os.Getenv("BIDDER_PUBLIC_ENDPOINT"), "OpenRTB endpoint to certify")
	flag.StringVar(&options.Token, "token", os.Getenv("BIDDER_OPENRTB_AUTH_TOKEN"), "Bearer token")
	flag.StringVar(&options.SigningSecret, "signing-secret", os.Getenv("BIDDER_OPENRTB_SIGNING_SECRET"), "HMAC signing secret")
	flag.StringVar(&options.SamplePath, "sample", "samples/openrtb-video-request.json", "sample OpenRTB request path")
	flag.IntVar(&timeoutMS, "timeout-ms", 2000, "HTTP timeout per certification request")
	flag.Parse()
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
