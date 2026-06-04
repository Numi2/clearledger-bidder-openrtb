package config

import "testing"

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
