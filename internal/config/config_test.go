package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadHTTPRuntimeDefaults(t *testing.T) {
	cfg, err := Load("../../config/campaigns.sample.json")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReadHeaderTimeoutMS != 500 || cfg.ReadTimeoutMS != 2000 || cfg.WriteTimeoutMS != 2000 {
		t.Fatalf("unexpected HTTP timeouts: %#v", cfg)
	}
	if cfg.IdleTimeoutMS != 60000 || cfg.ShutdownTimeoutMS != 5000 {
		t.Fatalf("unexpected lifecycle timeouts: %#v", cfg)
	}
	if cfg.MaxRequestBodyBytes != 256<<10 || cfg.MaxHeaderBytes != 16<<10 {
		t.Fatalf("unexpected limits: %#v", cfg)
	}
}

func TestLoadHTTPRuntimeEnvOverrides(t *testing.T) {
	t.Setenv("BIDDER_HTTP_READ_HEADER_TIMEOUT_MS", "750")
	t.Setenv("BIDDER_HTTP_READ_TIMEOUT_MS", "3000")
	t.Setenv("BIDDER_HTTP_WRITE_TIMEOUT_MS", "3500")
	t.Setenv("BIDDER_HTTP_IDLE_TIMEOUT_MS", "45000")
	t.Setenv("BIDDER_HTTP_SHUTDOWN_TIMEOUT_MS", "7000")
	t.Setenv("BIDDER_MAX_REQUEST_BODY_BYTES", "65536")
	t.Setenv("BIDDER_MAX_HEADER_BYTES", "32768")
	cfg, err := Load("../../config/campaigns.sample.json")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReadHeaderTimeoutMS != 750 || cfg.ReadTimeoutMS != 3000 || cfg.WriteTimeoutMS != 3500 {
		t.Fatalf("unexpected HTTP timeout overrides: %#v", cfg)
	}
	if cfg.IdleTimeoutMS != 45000 || cfg.ShutdownTimeoutMS != 7000 {
		t.Fatalf("unexpected lifecycle overrides: %#v", cfg)
	}
	if cfg.MaxRequestBodyBytes != 65536 || cfg.MaxHeaderBytes != 32768 {
		t.Fatalf("unexpected limit overrides: %#v", cfg)
	}
}

func TestValidateCampaignRejectsCreativeOutsideMediaTypes(t *testing.T) {
	err := validateCampaign(Campaign{
		ID:          "campaign",
		BidCPM:      1,
		MediaTypes:  []string{"video"},
		DailyBudget: 10,
		Creatives: []Creative{{
			ID:         "creative",
			Adomain:    []string{"advertiser.com"},
			MediaType:  "display",
			AssetURL:   "https://cdn.example/banner.png",
			LandingURL: "https://advertiser.com",
			Approved:   true,
		}},
	})
	if err == nil {
		t.Fatal("expected media mismatch error")
	}
}

func TestValidateCampaignAcceptsBannerAlias(t *testing.T) {
	err := validateCampaign(Campaign{
		ID:          "campaign",
		BidCPM:      1,
		MediaTypes:  []string{"banner"},
		DailyBudget: 10,
		Creatives: []Creative{{
			ID:         "creative",
			Adomain:    []string{"advertiser.com"},
			MediaType:  "display",
			AssetURL:   "https://cdn.example/banner.png",
			LandingURL: "https://advertiser.com",
			Approved:   true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsDuplicateCampaignIDs(t *testing.T) {
	path := writeConfig(t, `{
	  "buyer_id": "buyer",
	  "seat": "seat",
	  "currency": "USD",
	  "campaigns": [
	    {"id":"campaign","enabled":true,"bid_cpm":1,"daily_budget":1,"media_types":["display"],"creatives":[{"id":"creative_1","adomain":["advertiser.com"],"media_type":"display","asset_url":"https://cdn.example/ad.png","landing_url":"https://advertiser.com","approved":true}]},
	    {"id":"campaign","enabled":true,"bid_cpm":1,"daily_budget":1,"media_types":["display"],"creatives":[{"id":"creative_2","adomain":["advertiser.com"],"media_type":"display","asset_url":"https://cdn.example/ad.png","landing_url":"https://advertiser.com","approved":true}]}
	  ]
	}`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "duplicate campaign id") {
		t.Fatalf("expected duplicate campaign id error, got %v", err)
	}
}

func TestValidateCampaignRejectsUnsupportedMediaEvenWithMarkup(t *testing.T) {
	err := validateCampaign(Campaign{
		ID:          "campaign",
		BidCPM:      1,
		MediaTypes:  []string{"ctv"},
		DailyBudget: 10,
		Creatives: []Creative{{
			ID:        "creative",
			Adomain:   []string{"advertiser.com"},
			MediaType: "ctv",
			Markup:    "<xml></xml>",
			Approved:  true,
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported media_type") {
		t.Fatalf("expected unsupported media error, got %v", err)
	}
}

func TestValidateCampaignRejectsDuplicateCreativeIDs(t *testing.T) {
	err := validateCampaign(Campaign{
		ID:          "campaign",
		BidCPM:      1,
		MediaTypes:  []string{"display"},
		DailyBudget: 10,
		Creatives: []Creative{
			{ID: "creative", Adomain: []string{"advertiser.com"}, MediaType: "display", AssetURL: "https://cdn.example/ad.png", LandingURL: "https://advertiser.com", Approved: true},
			{ID: "creative", Adomain: []string{"advertiser.com"}, MediaType: "display", AssetURL: "https://cdn.example/ad2.png", LandingURL: "https://advertiser.com", Approved: true},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate creative id") {
		t.Fatalf("expected duplicate creative id error, got %v", err)
	}
}

func TestValidateCampaignRejectsNegativeOperationalValues(t *testing.T) {
	for _, tc := range []struct {
		name     string
		campaign Campaign
		want     string
	}{
		{
			name: "negative qps",
			campaign: Campaign{
				ID:          "campaign",
				BidCPM:      1,
				DailyBudget: 1,
				QPS:         -1,
				MediaTypes:  []string{"display"},
				Creatives:   []Creative{{ID: "creative", Adomain: []string{"advertiser.com"}, MediaType: "display", AssetURL: "https://cdn.example/ad.png", LandingURL: "https://advertiser.com", Approved: true}},
			},
			want: "qps must be non-negative",
		},
		{
			name: "negative duration",
			campaign: Campaign{
				ID:          "campaign",
				BidCPM:      1,
				DailyBudget: 1,
				MediaTypes:  []string{"video"},
				Creatives:   []Creative{{ID: "creative", Adomain: []string{"advertiser.com"}, MediaType: "video", AssetURL: "https://cdn.example/ad.mp4", Duration: -1, Approved: true}},
			},
			want: "duration must be non-negative",
		},
		{
			name: "negative dimensions",
			campaign: Campaign{
				ID:          "campaign",
				BidCPM:      1,
				DailyBudget: 1,
				MediaTypes:  []string{"display"},
				Creatives:   []Creative{{ID: "creative", Adomain: []string{"advertiser.com"}, MediaType: "display", AssetURL: "https://cdn.example/ad.png", LandingURL: "https://advertiser.com", W: -1, Approved: true}},
			},
			want: "dimensions must be non-negative",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCampaign(tc.campaign)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
