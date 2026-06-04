package bidder

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Numi2/clearledger-bidder-openrtb/internal/config"
	"github.com/Numi2/clearledger-bidder-openrtb/internal/openrtb"
)

func TestEvenPacingAllowsAtLeastOneBidThenCapsEarlySpend(t *testing.T) {
	campaign := config.Campaign{
		ID:              "campaign_1",
		BidCPM:          10,
		DailyBudget:     1,
		PacingMode:      "even",
		PacingTolerance: 1,
		QPS:             100,
	}
	engine := NewEngine(config.Config{})
	now := time.Date(2026, 6, 4, 0, 1, 0, 0, time.UTC)
	if !engine.reserve(campaign, 10, now) {
		t.Fatal("first bid should be allowed so campaigns can start delivery")
	}
	if engine.reserve(campaign, 10, now) {
		t.Fatal("second immediate bid should be paced out early in the day")
	}
}

func TestASAPPacingOnlyUsesDailyBudget(t *testing.T) {
	campaign := config.Campaign{
		ID:          "campaign_1",
		BidCPM:      10,
		DailyBudget: 0.02,
		QPS:         100,
	}
	engine := NewEngine(config.Config{})
	now := time.Date(2026, 6, 4, 0, 1, 0, 0, time.UTC)
	if !engine.reserve(campaign, 10, now) {
		t.Fatal("first bid should reserve")
	}
	if !engine.reserve(campaign, 10, now) {
		t.Fatal("second bid should reserve up to budget")
	}
	if engine.reserve(campaign, 10, now) {
		t.Fatal("third bid should exceed daily budget")
	}
}

func BenchmarkEngineBidVideoPMP(b *testing.B) {
	cfg, err := config.Load("../../config/campaigns.sample.json")
	if err != nil {
		b.Fatal(err)
	}
	for i := range cfg.Campaigns {
		cfg.Campaigns[i].DailyBudget = 1_000_000
		cfg.Campaigns[i].QPS = 0
		cfg.Campaigns[i].PacingMode = "asap"
	}
	body, err := os.ReadFile("../../samples/openrtb-video-request.json")
	if err != nil {
		b.Fatal(err)
	}
	req, err := openrtb.DecodeRequest(body)
	if err != nil {
		b.Fatal(err)
	}
	req.TMax = 0
	engine := NewEngine(cfg)
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decision := engine.Bid(context.Background(), req, now)
		if decision.NoBid || decision.Response == nil {
			b.Fatalf("unexpected no-bid: %s", decision.Reason)
		}
	}
}
