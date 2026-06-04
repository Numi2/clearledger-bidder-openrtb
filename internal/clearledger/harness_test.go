package clearledger

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if !strings.Contains(report.SupplyResponse.VAST, "package_id=package_123") || !strings.Contains(report.SupplyResponse.VAST, "proof_run_id=proof_") {
		t.Fatalf("expected package/proof correlation in VAST notice URL: %s", report.SupplyResponse.VAST)
	}
	if report.DeliveryProof == nil || report.DeliveryProof.Owner != "clearledger" || report.DeliveryProof.BidderReceivesSettlementState {
		t.Fatalf("expected ClearLedger-owned delivery proof: %#v", report.DeliveryProof)
	}
	if !hasProofStep(report, "billing_settlement_final_receipt", "clearledger") {
		t.Fatalf("expected ClearLedger-owned settlement proof step: %#v", report.ProofSteps)
	}
}

func TestRunHarnessClassifiesBuyerOutcomesAndSelectsHighestValidBid(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/no-bid", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/invalid", func(w http.ResponseWriter, r *http.Request) {
		writeTestBidResponse(t, w, r, "buyer_invalid", "seat_invalid", 1.00, "deal_clearledger_123")
	})
	mux.HandleFunc("/low", func(w http.ResponseWriter, r *http.Request) {
		writeTestBidResponse(t, w, r, "buyer_low", "seat_low", 9.25, "deal_clearledger_123")
	})
	mux.HandleFunc("/high", func(w http.ResponseWriter, r *http.Request) {
		writeTestBidResponse(t, w, r, "buyer_high", "seat_high", 12.50, "deal_clearledger_123")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	manifestPath := writeManifestWithBuyers(t, []ApprovedBuyer{
		testBuyer("buyer_no_bid", "seat_no_bid", srv.URL+"/no-bid"),
		testBuyer("buyer_invalid", "seat_invalid", srv.URL+"/invalid"),
		testBuyer("buyer_low", "seat_low", srv.URL+"/low"),
		testBuyer("buyer_high", "seat_high", srv.URL+"/high"),
	})
	report, err := RunHarness(context.Background(), HarnessOptions{
		ManifestPath:    manifestPath,
		PrivateMarketID: "pm_cert",
		SamplePath:      "../../samples/openrtb-video-request.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK {
		t.Fatalf("expected strict report to flag invalid selected buyer: %#v", report.Checks)
	}
	if report.Winner == nil || report.Winner.BuyerID != "buyer_high" || report.Winner.Price != 12.50 {
		t.Fatalf("expected highest valid bidder to win: %#v", report.Winner)
	}
	assertOutcome(t, report, "buyer_no_bid", "no_bid")
	assertOutcome(t, report, "buyer_invalid", "invalid_bid")
	assertOutcome(t, report, "buyer_low", "bid")
	assertOutcome(t, report, "buyer_high", "bid")
	if report.SupplyResponse == nil || report.SupplyResponse.Type != "vast" || !strings.Contains(report.SupplyResponse.VAST, "MediaFile") {
		t.Fatalf("expected VAST supply response: %#v", report.SupplyResponse)
	}
	if report.DeliveryProof == nil || report.DeliveryProof.Owner != "clearledger" || report.DeliveryProof.BillableEvent != "impression" {
		t.Fatalf("expected ClearLedger delivery proof: %#v", report.DeliveryProof)
	}
}

func TestRunHarnessEnforcesBuyerRouteAndTimeout(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(80 * time.Millisecond)
		writeTestBidResponse(t, w, r, "buyer_slow", "seat_slow", 13.00, "deal_clearledger_123")
	})
	mux.HandleFunc("/valid", func(w http.ResponseWriter, r *http.Request) {
		writeTestBidResponse(t, w, r, "buyer_valid", "seat_valid", 10.00, "deal_clearledger_123")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	wrongFormat := testBuyer("buyer_wrong_format", "seat_wrong_format", srv.URL+"/valid")
	wrongFormat.AllowedFormats = []string{"display"}
	wrongProtocol := testBuyer("buyer_wrong_protocol", "seat_wrong_protocol", srv.URL+"/valid")
	wrongProtocol.BidProtocol = "vast_tag"
	slow := testBuyer("buyer_slow", "seat_slow", srv.URL+"/slow")
	slow.TimeoutMS = 20
	valid := testBuyer("buyer_valid", "seat_valid", srv.URL+"/valid")
	valid.TimeoutMS = 200
	manifestPath := writeManifestWithBuyers(t, []ApprovedBuyer{wrongFormat, wrongProtocol, slow, valid})

	report, err := RunHarness(context.Background(), HarnessOptions{
		ManifestPath:    manifestPath,
		PrivateMarketID: "pm_cert",
		SamplePath:      "../../samples/openrtb-video-request.json",
		Timeout:         time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK {
		t.Fatalf("expected strict report to flag timed out buyer: %#v", report.Checks)
	}
	assertOutcome(t, report, "buyer_wrong_format", "skipped")
	assertReason(t, report, "buyer_wrong_format", "buyer_format_not_allowed")
	assertOutcome(t, report, "buyer_wrong_protocol", "skipped")
	assertReason(t, report, "buyer_wrong_protocol", "unsupported_bid_protocol")
	assertOutcome(t, report, "buyer_slow", "error")
	assertOutcome(t, report, "buyer_valid", "bid")
	if report.Winner == nil || report.Winner.BuyerID != "buyer_valid" {
		t.Fatalf("expected valid buyer to win despite skipped/timeout buyers: %#v", report.Winner)
	}
}

func writeManifest(t *testing.T, endpoint string) string {
	t.Helper()
	return writeManifestWithBuyers(t, []ApprovedBuyer{{
		BuyerID:          "agency_bidder_1",
		SeatID:           "agency_seat_1",
		Status:           "approved",
		Endpoint:         endpoint,
		BidProtocol:      "openrtb_json",
		AllowedFormats:   []string{"video"},
		AuthTokenEnv:     "BIDDER_OPENRTB_AUTH_TOKEN",
		SigningSecretEnv: "BIDDER_OPENRTB_SIGNING_SECRET",
	}})
}

func writeManifestWithBuyers(t *testing.T, buyers []ApprovedBuyer) string {
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
			ApprovedBuyers:  buyers,
		}},
	}
	body, _ := json.MarshalIndent(manifest, "", "  ")
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testBuyer(id, seat, endpoint string) ApprovedBuyer {
	return ApprovedBuyer{
		BuyerID:        id,
		SeatID:         seat,
		Status:         "approved",
		Endpoint:       endpoint,
		BidProtocol:    "openrtb_json",
		AllowedFormats: []string{"video"},
	}
}

func writeTestBidResponse(t *testing.T, w http.ResponseWriter, r *http.Request, buyerID, seat string, price float64, dealID string) {
	t.Helper()
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Fatal(err)
	}
	reqID, _ := req["id"].(string)
	impID := "1"
	clearLedgerExt := map[string]any{
		"buyer_id":         buyerID,
		"campaign_id":      "campaign_" + buyerID,
		"creative_id":      "creative_" + buyerID,
		"receipt_required": true,
	}
	if imps, ok := req["imp"].([]any); ok && len(imps) > 0 {
		if imp, ok := imps[0].(map[string]any); ok {
			if raw, ok := imp["id"].(string); ok && raw != "" {
				impID = raw
			}
			if ext, ok := imp["ext"].(map[string]any); ok {
				if cl, ok := ext["clearledger"].(map[string]any); ok {
					for _, key := range []string{"lane_id", "private_market_id", "package_id", "placement_id", "proof_run_id", "receipt_required"} {
						if value, ok := cl[key]; ok {
							clearLedgerExt[key] = value
						}
					}
				}
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"id":  reqID,
		"cur": "USD",
		"seatbid": []map[string]any{{
			"seat": seat,
			"bid": []map[string]any{{
				"id":      "bid_" + buyerID,
				"impid":   impID,
				"price":   price,
				"crid":    "creative_" + buyerID,
				"adomain": []string{"advertiser.com"},
				"dealid":  dealID,
				"adm":     `<VAST version="4.3"><Ad><InLine><Impression><![CDATA[https://clearledger.example/imp]]></Impression><Creatives><Creative><Linear><Duration>00:00:30</Duration><MediaFiles><MediaFile delivery="progressive" type="video/mp4"><![CDATA[https://cdn.example.com/ad.mp4]]></MediaFile></MediaFiles></Linear></Creative></Creatives></InLine></Ad></VAST>`,
				"nurl":    "https://clearledger.example/win",
				"ext":     map[string]any{"clearledger": clearLedgerExt},
			}},
		}},
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		t.Fatal(err)
	}
}

func assertOutcome(t *testing.T, report Report, buyerID, outcome string) {
	t.Helper()
	for _, result := range report.BuyerResults {
		if result.BuyerID == buyerID {
			if result.Outcome != outcome {
				t.Fatalf("buyer %s outcome=%s want=%s result=%#v", buyerID, result.Outcome, outcome, result)
			}
			return
		}
	}
	t.Fatalf("missing buyer result for %s: %#v", buyerID, report.BuyerResults)
}

func assertReason(t *testing.T, report Report, buyerID, reason string) {
	t.Helper()
	for _, result := range report.BuyerResults {
		if result.BuyerID == buyerID {
			if result.Reason != reason {
				t.Fatalf("buyer %s reason=%s want=%s result=%#v", buyerID, result.Reason, reason, result)
			}
			return
		}
	}
	t.Fatalf("missing buyer result for %s: %#v", buyerID, report.BuyerResults)
}

func hasProofStep(report Report, name, owner string) bool {
	for _, step := range report.ProofSteps {
		if step.Name == name && step.Owner == owner && step.Complete {
			return true
		}
	}
	return false
}
