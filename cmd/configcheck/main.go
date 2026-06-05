package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/Numi2/clearledger-bidder-openrtb/internal/config"
)

func main() {
	var path string
	flag.StringVar(&path, "config", "config/campaigns.sample.json", "campaign config path")
	flag.Parse()
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config invalid: %v\n", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(cfg.Summary())
}
