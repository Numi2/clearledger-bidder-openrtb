package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                   string     `json:"-"`
	PublicEndpoint         string     `json:"public_endpoint"`
	BuyerID                string     `json:"buyer_id"`
	Seat                   string     `json:"seat"`
	Currency               string     `json:"currency"`
	AuthToken              string     `json:"-"`
	SigningSecret          string     `json:"-"`
	RequireAuth            bool       `json:"-"`
	RequireSignature       bool       `json:"-"`
	SignatureSkew          int        `json:"-"`
	ClearLedgerRegisterURL string     `json:"-"`
	ClearLedgerAPIKey      string     `json:"-"`
	Campaigns              []Campaign `json:"campaigns"`
}

type Campaign struct {
	ID                string     `json:"id"`
	Enabled           bool       `json:"enabled"`
	Seat              string     `json:"seat,omitempty"`
	BidCPM            float64    `json:"bid_cpm"`
	DailyBudget       float64    `json:"daily_budget"`
	QPS               int        `json:"qps,omitempty"`
	AllowedApps       []string   `json:"allowed_apps,omitempty"`
	AllowedBundles    []string   `json:"allowed_bundles,omitempty"`
	AllowedDomains    []string   `json:"allowed_domains,omitempty"`
	AllowedPlacements []string   `json:"allowed_placements,omitempty"`
	DealIDs           []string   `json:"deal_ids,omitempty"`
	GeoCountries      []string   `json:"geo_countries,omitempty"`
	MediaTypes        []string   `json:"media_types"`
	Creatives         []Creative `json:"creatives"`
}

type Creative struct {
	ID            string   `json:"id"`
	Adomain       []string `json:"adomain"`
	MediaType     string   `json:"media_type"`
	Markup        string   `json:"markup,omitempty"`
	AssetURL      string   `json:"asset_url,omitempty"`
	LandingURL    string   `json:"landing_url,omitempty"`
	Duration      int      `json:"duration,omitempty"`
	W             int      `json:"w,omitempty"`
	H             int      `json:"h,omitempty"`
	Approved      bool     `json:"approved"`
	BlockedCOPPA  bool     `json:"blocked_coppa,omitempty"`
	RequiresIFA   bool     `json:"requires_ifa,omitempty"`
	NoticeBaseURL string   `json:"notice_base_url,omitempty"`
}

func Load(path string) (Config, error) {
	var cfg Config
	if path != "" {
		body, err := os.ReadFile(path)
		if err != nil {
			return cfg, err
		}
		if err := json.Unmarshal(body, &cfg); err != nil {
			return cfg, err
		}
	}
	cfg.Port = getenv("PORT", "8080")
	cfg.PublicEndpoint = getenv("BIDDER_PUBLIC_ENDPOINT", cfg.PublicEndpoint)
	cfg.BuyerID = getenv("BIDDER_BUYER_ID", first(cfg.BuyerID, "clearledger_bidder_openrtb"))
	cfg.Seat = getenv("BIDDER_SEAT", first(cfg.Seat, "agency_seat_1"))
	cfg.Currency = strings.ToUpper(getenv("BIDDER_CURRENCY", first(cfg.Currency, "USD")))
	cfg.AuthToken = os.Getenv("BIDDER_OPENRTB_AUTH_TOKEN")
	cfg.SigningSecret = os.Getenv("BIDDER_OPENRTB_SIGNING_SECRET")
	cfg.RequireAuth = boolEnv("BIDDER_OPENRTB_REQUIRE_AUTH", cfg.AuthToken != "")
	cfg.RequireSignature = boolEnv("BIDDER_OPENRTB_REQUIRE_SIGNATURE", cfg.SigningSecret != "")
	cfg.SignatureSkew = intEnv("BIDDER_OPENRTB_SIGNATURE_SKEW_SECONDS", 300)
	cfg.ClearLedgerRegisterURL = os.Getenv("CLEARLEDGER_REGISTER_URL")
	cfg.ClearLedgerAPIKey = os.Getenv("CLEARLEDGER_API_KEY")
	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	if _, err := strconv.Atoi(cfg.Port); err != nil {
		return cfg, fmt.Errorf("PORT must be numeric")
	}
	if cfg.BuyerID == "" || cfg.Seat == "" || cfg.Currency == "" {
		return cfg, fmt.Errorf("buyer_id, seat, and currency are required")
	}
	for i := range cfg.Campaigns {
		if cfg.Campaigns[i].Seat == "" {
			cfg.Campaigns[i].Seat = cfg.Seat
		}
		if err := validateCampaign(cfg.Campaigns[i]); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

func validateCampaign(c Campaign) error {
	if c.ID == "" {
		return fmt.Errorf("campaign id is required")
	}
	if c.BidCPM <= 0 {
		return fmt.Errorf("campaign %s bid_cpm must be positive", c.ID)
	}
	if c.DailyBudget < 0 {
		return fmt.Errorf("campaign %s daily_budget must be non-negative", c.ID)
	}
	if len(c.MediaTypes) == 0 {
		return fmt.Errorf("campaign %s needs at least one media type", c.ID)
	}
	if len(c.Creatives) == 0 {
		return fmt.Errorf("campaign %s needs at least one creative", c.ID)
	}
	return nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func first(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func boolEnv(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return strings.EqualFold(value, "true") || value == "1"
}

func intEnv(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil {
		return fallback
	}
	return value
}

func (c Config) SignatureSkewDuration() time.Duration {
	return time.Duration(c.SignatureSkew) * time.Second
}
