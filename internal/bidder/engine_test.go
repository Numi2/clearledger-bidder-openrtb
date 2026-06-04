package bidder

import (
	"testing"
	"time"

	"github.com/Numi2/clearledger-bidder-openrtb/internal/config"
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
