package clearledger

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type Manifest struct {
	Version       string `json:"version"`
	SchemaVersion string `json:"schema_version,omitempty"`
	GeneratedAt   string `json:"generated_at,omitempty"`
	OrgID         string `json:"org_id,omitempty"`
	Lanes         []Lane `json:"lanes"`
}

type Lane struct {
	OrgID           string          `json:"org_id,omitempty"`
	LaneID          string          `json:"lane_id"`
	PrivateMarketID string          `json:"private_market_id"`
	SellerID        string          `json:"seller_id"`
	Status          string          `json:"status"`
	AuctionType     string          `json:"auction_type,omitempty"`
	FloorCPM        float64         `json:"floor_cpm"`
	Currency        string          `json:"currency"`
	Formats         []string        `json:"formats"`
	AppBundles      []string        `json:"app_bundles"`
	PlacementIDs    []string        `json:"placement_ids"`
	ApprovedBuyers  []ApprovedBuyer `json:"approved_buyers"`
	RequireAdomain  bool            `json:"require_adomain"`
	Metadata        map[string]any  `json:"metadata,omitempty"`
}

type ApprovedBuyer struct {
	BuyerID          string        `json:"buyer_id"`
	SeatID           string        `json:"seat_id"`
	Name             string        `json:"name,omitempty"`
	Status           string        `json:"status"`
	Endpoint         string        `json:"openrtb_endpoint"`
	BidProtocol      string        `json:"bid_protocol,omitempty"`
	TimeoutMS        int           `json:"timeout_ms,omitempty"`
	QPSLimit         float64       `json:"qps_limit,omitempty"`
	AllowedFormats   []string      `json:"allowed_formats,omitempty"`
	AuthHeaderName   string        `json:"auth_header_name,omitempty"`
	AuthTokenEnv     string        `json:"auth_token_env,omitempty"`
	AuthToken        string        `json:"auth_token,omitempty"`
	SigningSecretEnv string        `json:"signing_secret_env,omitempty"`
	SigningSecret    string        `json:"signing_secret,omitempty"`
	OpenRTBCompat    OpenRTBCompat `json:"openrtb_compat,omitempty"`
}

type OpenRTBCompat struct {
	AcceptedRequestVersions []string `json:"accepted_request_versions,omitempty"`
	OutboundVersion         string   `json:"outbound_version,omitempty"`
	PartnerProfile          string   `json:"partner_profile,omitempty"`
	PreservePartnerExt      bool     `json:"preserve_partner_ext,omitempty"`
}

func LoadManifest(path string) (Manifest, error) {
	var manifest Manifest
	body, err := os.ReadFile(path)
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		return manifest, err
	}
	if len(manifest.Lanes) == 0 {
		return manifest, fmt.Errorf("manifest must include at least one lane")
	}
	return manifest, nil
}

func (m Manifest) Lane(privateMarketID string) (Lane, bool) {
	for _, lane := range m.Lanes {
		if privateMarketID == "" || strings.EqualFold(lane.PrivateMarketID, privateMarketID) {
			return lane, true
		}
	}
	return Lane{}, false
}

func (l Lane) ActiveApprovedBuyers() []ApprovedBuyer {
	out := make([]ApprovedBuyer, 0, len(l.ApprovedBuyers))
	for _, buyer := range l.ApprovedBuyers {
		if strings.EqualFold(strings.TrimSpace(buyer.Status), "approved") && strings.TrimSpace(buyer.Endpoint) != "" {
			out = append(out, buyer)
		}
	}
	return out
}
