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
	OpenRTBEndpoint        string     `json:"openrtb_endpoint,omitempty"`
	BuyerID                string     `json:"buyer_id"`
	Seat                   string     `json:"seat"`
	Currency               string     `json:"currency"`
	AuthToken              string     `json:"-"`
	SigningSecret          string     `json:"-"`
	RequireAuth            bool       `json:"-"`
	RequireSignature       bool       `json:"-"`
	SignatureSkew          int        `json:"-"`
	ReadHeaderTimeoutMS    int        `json:"-"`
	ReadTimeoutMS          int        `json:"-"`
	WriteTimeoutMS         int        `json:"-"`
	IdleTimeoutMS          int        `json:"-"`
	ShutdownTimeoutMS      int        `json:"-"`
	MaxRequestBodyBytes    int        `json:"-"`
	MaxHeaderBytes         int        `json:"-"`
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
	PacingMode        string     `json:"pacing_mode,omitempty"`
	PacingTolerance   float64    `json:"pacing_tolerance,omitempty"`
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
	cfg.OpenRTBEndpoint = getenv("BIDDER_OPENRTB_ENDPOINT", cfg.OpenRTBEndpoint)
	cfg.BuyerID = getenv("BIDDER_BUYER_ID", first(cfg.BuyerID, "clearledger_bidder_openrtb"))
	cfg.Seat = getenv("BIDDER_SEAT", first(cfg.Seat, "agency_seat_1"))
	cfg.Currency = strings.ToUpper(getenv("BIDDER_CURRENCY", first(cfg.Currency, "USD")))
	cfg.AuthToken = os.Getenv("BIDDER_OPENRTB_AUTH_TOKEN")
	cfg.SigningSecret = os.Getenv("BIDDER_OPENRTB_SIGNING_SECRET")
	cfg.RequireAuth = boolEnv("BIDDER_OPENRTB_REQUIRE_AUTH", cfg.AuthToken != "")
	cfg.RequireSignature = boolEnv("BIDDER_OPENRTB_REQUIRE_SIGNATURE", cfg.SigningSecret != "")
	cfg.SignatureSkew = intEnv("BIDDER_OPENRTB_SIGNATURE_SKEW_SECONDS", 300)
	cfg.ReadHeaderTimeoutMS = positiveIntEnv("BIDDER_HTTP_READ_HEADER_TIMEOUT_MS", 500)
	cfg.ReadTimeoutMS = positiveIntEnv("BIDDER_HTTP_READ_TIMEOUT_MS", 2000)
	cfg.WriteTimeoutMS = positiveIntEnv("BIDDER_HTTP_WRITE_TIMEOUT_MS", 2000)
	cfg.IdleTimeoutMS = positiveIntEnv("BIDDER_HTTP_IDLE_TIMEOUT_MS", 60000)
	cfg.ShutdownTimeoutMS = positiveIntEnv("BIDDER_HTTP_SHUTDOWN_TIMEOUT_MS", 5000)
	cfg.MaxRequestBodyBytes = positiveIntEnv("BIDDER_MAX_REQUEST_BODY_BYTES", 256<<10)
	cfg.MaxHeaderBytes = positiveIntEnv("BIDDER_MAX_HEADER_BYTES", 16<<10)
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
	if cfg.SignatureSkew <= 0 {
		return cfg, fmt.Errorf("BIDDER_OPENRTB_SIGNATURE_SKEW_SECONDS must be positive")
	}
	campaignIDs := map[string]struct{}{}
	for i := range cfg.Campaigns {
		if cfg.Campaigns[i].Seat == "" {
			cfg.Campaigns[i].Seat = cfg.Seat
		}
		if cfg.Campaigns[i].ID != "" {
			if _, ok := campaignIDs[cfg.Campaigns[i].ID]; ok {
				return cfg, fmt.Errorf("duplicate campaign id %s", cfg.Campaigns[i].ID)
			}
			campaignIDs[cfg.Campaigns[i].ID] = struct{}{}
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
	switch normalizeToken(c.PacingMode) {
	case "", "asap", "even":
	default:
		return fmt.Errorf("campaign %s pacing_mode must be asap or even", c.ID)
	}
	if c.PacingTolerance < 0 {
		return fmt.Errorf("campaign %s pacing_tolerance must be non-negative", c.ID)
	}
	if c.QPS < 0 {
		return fmt.Errorf("campaign %s qps must be non-negative", c.ID)
	}
	if len(c.MediaTypes) == 0 {
		return fmt.Errorf("campaign %s needs at least one media type", c.ID)
	}
	if len(c.Creatives) == 0 {
		return fmt.Errorf("campaign %s needs at least one creative", c.ID)
	}
	mediaTypes := map[string]struct{}{}
	for _, mediaType := range c.MediaTypes {
		mediaType = normalizeMediaType(mediaType)
		if mediaType == "" {
			return fmt.Errorf("campaign %s contains an empty media type", c.ID)
		}
		if !supportedMediaType(mediaType) {
			return fmt.Errorf("campaign %s unsupported media_type %s", c.ID, mediaType)
		}
		mediaTypes[mediaType] = struct{}{}
	}
	approved := 0
	creativeIDs := map[string]struct{}{}
	for _, creative := range c.Creatives {
		if creative.ID == "" {
			return fmt.Errorf("campaign %s creative id is required", c.ID)
		}
		if _, ok := creativeIDs[creative.ID]; ok {
			return fmt.Errorf("campaign %s duplicate creative id %s", c.ID, creative.ID)
		}
		creativeIDs[creative.ID] = struct{}{}
		mediaType := normalizeMediaType(creative.MediaType)
		if !supportedMediaType(mediaType) {
			return fmt.Errorf("campaign %s creative %s unsupported media_type %s", c.ID, creative.ID, creative.MediaType)
		}
		if _, ok := mediaTypes[mediaType]; !ok {
			return fmt.Errorf("campaign %s creative %s media_type is not in campaign media_types", c.ID, creative.ID)
		}
		if len(creative.Adomain) == 0 {
			return fmt.Errorf("campaign %s creative %s adomain is required", c.ID, creative.ID)
		}
		if creative.Duration < 0 {
			return fmt.Errorf("campaign %s creative %s duration must be non-negative", c.ID, creative.ID)
		}
		if creative.W < 0 || creative.H < 0 {
			return fmt.Errorf("campaign %s creative %s dimensions must be non-negative", c.ID, creative.ID)
		}
		if creative.Approved {
			approved++
		}
		if creative.Markup == "" {
			switch mediaType {
			case "video", "audio":
				if creative.AssetURL == "" {
					return fmt.Errorf("campaign %s creative %s asset_url is required for %s", c.ID, creative.ID, mediaType)
				}
			case "display":
				if creative.AssetURL == "" || creative.LandingURL == "" {
					return fmt.Errorf("campaign %s creative %s asset_url and landing_url are required for display", c.ID, creative.ID)
				}
			case "native":
				if creative.LandingURL == "" {
					return fmt.Errorf("campaign %s creative %s landing_url is required for native", c.ID, creative.ID)
				}
			}
		}
	}
	if approved == 0 {
		return fmt.Errorf("campaign %s needs at least one approved creative", c.ID)
	}
	return nil
}

func supportedMediaType(value string) bool {
	switch normalizeMediaType(value) {
	case "video", "audio", "display", "native":
		return true
	default:
		return false
	}
}

func normalizeMediaType(value string) string {
	value = normalizeToken(value)
	if value == "banner" {
		return "display"
	}
	return value
}

func normalizeToken(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", "_"))
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

func positiveIntEnv(key string, fallback int) int {
	value := intEnv(key, fallback)
	if value <= 0 {
		return fallback
	}
	return value
}

func (c Config) SignatureSkewDuration() time.Duration {
	return time.Duration(c.SignatureSkew) * time.Second
}

func (c Config) RegistrationEndpoint() string {
	if strings.TrimSpace(c.OpenRTBEndpoint) != "" {
		return strings.TrimRight(strings.TrimSpace(c.OpenRTBEndpoint), "/")
	}
	base := strings.TrimRight(strings.TrimSpace(c.PublicEndpoint), "/")
	if base == "" {
		return ""
	}
	if strings.HasSuffix(base, "/openrtb") {
		return base
	}
	return base + "/openrtb"
}

func (c Config) ReadHeaderTimeout() time.Duration {
	return time.Duration(c.ReadHeaderTimeoutMS) * time.Millisecond
}

func (c Config) ReadTimeout() time.Duration {
	return time.Duration(c.ReadTimeoutMS) * time.Millisecond
}

func (c Config) WriteTimeout() time.Duration {
	return time.Duration(c.WriteTimeoutMS) * time.Millisecond
}

func (c Config) IdleTimeout() time.Duration {
	return time.Duration(c.IdleTimeoutMS) * time.Millisecond
}

func (c Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.ShutdownTimeoutMS) * time.Millisecond
}
