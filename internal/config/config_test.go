package config

import "testing"

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
