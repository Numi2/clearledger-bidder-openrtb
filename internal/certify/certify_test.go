package certify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPostOpenRTBSendsProductionIdentityHeaders(t *testing.T) {
	var gotBuyerID, gotSeatID, gotAuctionID, gotRequestID, gotSignature string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBuyerID = r.Header.Get("X-ClearLedger-Buyer-ID")
		gotSeatID = r.Header.Get("X-ClearLedger-Seat-ID")
		gotAuctionID = r.Header.Get("X-ClearLedger-Auction-ID")
		gotRequestID = r.Header.Get("X-ClearLedger-Request-ID")
		gotSignature = r.Header.Get("X-ClearLedger-Buyer-Signature")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	body := mustJSON(t, map[string]any{
		"id":     "request_1",
		"source": map[string]any{"tid": "auction_1"},
		"site":   map[string]any{"domain": "example.com"},
		"imp":    []map[string]any{{"id": "1", "banner": map[string]any{"w": 300, "h": 250}}},
	})
	status, _, err := postOpenRTB(context.Background(), srv.Client(), Options{
		Endpoint:      srv.URL,
		SigningSecret: "secret",
		BuyerID:       "agency_bidder_1",
		SeatID:        "agency_seat_1",
	}, body)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusNoContent {
		t.Fatalf("status=%d", status)
	}
	if gotBuyerID != "agency_bidder_1" || gotSeatID != "agency_seat_1" {
		t.Fatalf("identity headers buyer=%q seat=%q", gotBuyerID, gotSeatID)
	}
	if gotAuctionID != "auction_1" || gotRequestID != "request_1" {
		t.Fatalf("request headers auction=%q request=%q", gotAuctionID, gotRequestID)
	}
	if gotSignature == "" {
		t.Fatal("missing production signature header")
	}
}

func TestRunReportsIdentityMismatchFromEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readyz":
			w.WriteHeader(http.StatusOK)
		case "/openrtb":
			if r.Header.Get("X-ClearLedger-Buyer-ID") != "expected_buyer" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"buyer_id_mismatch"}`))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	report, err := Run(context.Background(), Options{
		Endpoint:    srv.URL + "/openrtb",
		BuyerID:     "wrong_buyer",
		SamplePath:  "../../samples/openrtb-display-request.json",
		Timeout:     time.Second,
		SamplePaths: []string{"../../samples/openrtb-display-request.json"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK {
		t.Fatalf("expected certification failure: %#v", report.Checks)
	}
	if !hasCheckDetail(report, "display_valid_bid_http", "status=400") {
		t.Fatalf("expected status detail in report: %#v", report.Checks)
	}
}

func hasCheckDetail(report Report, name, detail string) bool {
	for _, check := range report.Checks {
		if check.Name == name && check.Detail == detail {
			return true
		}
	}
	return false
}

func mustJSON(t *testing.T, payload any) []byte {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
