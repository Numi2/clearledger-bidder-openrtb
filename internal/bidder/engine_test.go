package bidder

import (
	"context"
	"os"
	"strings"
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

func TestBidRejectsUnsupportedRequestCurrency(t *testing.T) {
	cfg, req := sampleConfigAndRequest(t)
	req.Cur = []string{"EUR"}
	decision := NewEngine(cfg).Bid(context.Background(), req, time.Now().UTC())
	if !decision.NoBid || decision.Reason != "no_eligible_campaign" {
		t.Fatalf("expected currency no-bid, got %#v", decision)
	}
}

func TestBidRejectsBlockedAdvertiserDomain(t *testing.T) {
	cfg, req := sampleConfigAndRequest(t)
	req.BAdv = []string{"advertiser.com"}
	decision := NewEngine(cfg).Bid(context.Background(), req, time.Now().UTC())
	if !decision.NoBid || decision.Reason != "no_eligible_campaign" {
		t.Fatalf("expected badv no-bid, got %#v", decision)
	}
}

func TestBidRejectsDealCurrencyMismatch(t *testing.T) {
	cfg, req := sampleConfigAndRequest(t)
	req.Imp[0].PMP.Deals[0].BidFloorCur = "EUR"
	decision := NewEngine(cfg).Bid(context.Background(), req, time.Now().UTC())
	if !decision.NoBid || decision.Reason != "no_eligible_campaign" {
		t.Fatalf("expected deal currency no-bid, got %#v", decision)
	}
}

func TestBidRejectsUnsupportedVideoMime(t *testing.T) {
	cfg, req := sampleConfigAndRequest(t)
	req.Imp[0].Video.Mimes = []string{"video/webm"}
	decision := NewEngine(cfg).Bid(context.Background(), req, time.Now().UTC())
	if !decision.NoBid || decision.Reason != "no_eligible_campaign" {
		t.Fatalf("expected mime no-bid, got %#v", decision)
	}
}

func TestVASTDurationFormatsOverOneMinute(t *testing.T) {
	cfg, req := sampleConfigAndRequest(t)
	cfg.Campaigns[0].Creatives[0].Duration = 125
	decision := NewEngine(cfg).Bid(context.Background(), req, time.Now().UTC())
	if decision.NoBid || decision.Response == nil {
		t.Fatalf("expected bid, got %#v", decision)
	}
	adm := decision.Response.SeatBid[0].Bid[0].AdM
	if !strings.Contains(adm, "<Duration>00:02:05</Duration>") {
		t.Fatalf("expected HH:MM:SS VAST duration, got %s", adm)
	}
}

func BenchmarkEngineBidVideoPMP(b *testing.B) {
	cfg, req := sampleConfigAndRequest(b)
	for i := range cfg.Campaigns {
		cfg.Campaigns[i].DailyBudget = 1_000_000
		cfg.Campaigns[i].QPS = 0
		cfg.Campaigns[i].PacingMode = "asap"
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

type testingFatalHelper interface {
	Helper()
	Fatal(args ...any)
}

func sampleConfigAndRequest(t testingFatalHelper) (config.Config, *openrtb.BidRequest) {
	t.Helper()
	cfg, err := config.Load("../../config/campaigns.sample.json")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile("../../samples/openrtb-video-request.json")
	if err != nil {
		t.Fatal(err)
	}
	req, err := openrtb.DecodeRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, req
}
