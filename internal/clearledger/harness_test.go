package clearledger

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Numi2/clearledger-bidder-openrtb/internal/bidder"
	"github.com/Numi2/clearledger-bidder-openrtb/internal/config"
	"github.com/Numi2/clearledger-bidder-openrtb/internal/server"
)

func TestRunHarnessProvesClearLedgerBoundary(t *testing.T) {
	cfg, err := config.Load("../../config/campaigns.sample.json")
	if err != nil {
		t.Fatal(err)
	}
	cfg.AuthToken = "token"
	cfg.SigningSecret = "secret"
	cfg.RequireAuth = true
	cfg.RequireSignature = true
	srv := httptest.NewServer(server.New(cfg, bidder.NewEngine(cfg)))
	defer srv.Close()

	manifestPath := writeManifest(t, srv.URL+"/openrtb")
	report, err := RunHarness(context.Background(), HarnessOptions{
		ManifestPath:     manifestPath,
		PrivateMarketID:  "pm_cert",
		BuyerID:          "agency_bidder_1",
		SamplePath:       "../../samples/openrtb-video-request.json",
		AuthToken:        "token",
		SigningSecret:    "secret",
		EndpointOverride: srv.URL + "/openrtb",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK {
		body, _ := json.MarshalIndent(report, "", "  ")
		t.Fatalf("expected harness ok: %s", body)
	}
	if report.Winner == nil || report.Winner.BuyerID != "agency_bidder_1" || report.Winner.Price < 9 {
		t.Fatalf("unexpected winner: %#v", report.Winner)
	}
	if report.SupplyResponse == nil || report.SupplyResponse.Type != "vast" || report.SupplyResponse.VAST == "" {
		t.Fatalf("expected VAST supply response: %#v", report.SupplyResponse)
	}
	if !hasProofStep(report, "billing_settlement_final_receipt", "clearledger") {
		t.Fatalf("expected ClearLedger-owned settlement proof step: %#v", report.ProofSteps)
	}
}

func writeManifest(t *testing.T, endpoint string) string {
	t.Helper()
	manifest := Manifest{
		Version: "test",
		OrgID:   "org_cert",
		Lanes: []Lane{{
			OrgID:           "org_cert",
			LaneID:          "lane_123",
			PrivateMarketID: "pm_cert",
			SellerID:        "publisher_123",
			Status:          "active",
			AuctionType:     "first_price",
			FloorCPM:        9,
			Currency:        "USD",
			Formats:         []string{"video"},
			AppBundles:      []string{"com.example.app"},
			PlacementIDs:    []string{"placement_123"},
			RequireAdomain:  true,
			Metadata:        map[string]any{"package_id": "package_123", "deal_id": "deal_clearledger_123"},
			ApprovedBuyers: []ApprovedBuyer{{
				BuyerID:          "agency_bidder_1",
				SeatID:           "agency_seat_1",
				Status:           "approved",
				Endpoint:         endpoint,
				BidProtocol:      "openrtb_json",
				AllowedFormats:   []string{"video"},
				AuthTokenEnv:     "BIDDER_OPENRTB_AUTH_TOKEN",
				SigningSecretEnv: "BIDDER_OPENRTB_SIGNING_SECRET",
			}},
		}},
	}
	body, _ := json.MarshalIndent(manifest, "", "  ")
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func hasProofStep(report Report, name, owner string) bool {
	for _, step := range report.ProofSteps {
		if step.Name == name && step.Owner == owner && step.Complete {
			return true
		}
	}
	return false
}
