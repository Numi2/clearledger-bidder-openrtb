package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Numi2/clearledger-bidder-openrtb/internal/bidder"
	"github.com/Numi2/clearledger-bidder-openrtb/internal/config"
	"github.com/Numi2/clearledger-bidder-openrtb/internal/openrtb"
)

func TestBidVideoPMP(t *testing.T) {
	h := testHandler(t)
	rr := post(t, h, sampleVideoRequest(t))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp openrtb.BidResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != "auction_123" || len(resp.SeatBid) != 1 || len(resp.SeatBid[0].Bid) != 1 {
		t.Fatalf("invalid response: %+v", resp)
	}
	bid := resp.SeatBid[0].Bid[0]
	if bid.ImpID != "1" || bid.Price < 9 || bid.CrID == "" || bid.DealID != "deal_clearledger_123" {
		t.Fatalf("invalid bid: %+v", bid)
	}
	if !strings.Contains(bid.AdM, `<VAST version="4.3">`) {
		t.Fatalf("expected VAST adm, got %s", bid.AdM)
	}
}

func TestNoBidFloorAndDeal(t *testing.T) {
	h := testHandler(t)
	req := sampleVideoRequestMap(t)
	req["imp"].([]any)[0].(map[string]any)["bidfloor"] = 99.0
	if rr := post(t, h, mustJSON(t, req)); rr.Code != http.StatusNoContent {
		t.Fatalf("floor status=%d body=%s", rr.Code, rr.Body.String())
	}
	req = sampleVideoRequestMap(t)
	req["imp"].([]any)[0].(map[string]any)["pmp"].(map[string]any)["deals"] = []any{map[string]any{"id": "wrong_deal", "bidfloor": 1.0}}
	if rr := post(t, h, mustJSON(t, req)); rr.Code != http.StatusNoContent {
		t.Fatalf("deal status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestMalformedOpenRTB(t *testing.T) {
	h := testHandler(t)
	rr := post(t, h, []byte(`{"id":"x","site":{"domain":"example.com"},"imp":[]}`))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDisplayAndNativeMarkup(t *testing.T) {
	h := testHandler(t)
	display := []byte(`{"id":"display_auction","tmax":100,"site":{"domain":"example.com"},"imp":[{"id":"1","tagid":"display_1","bidfloor":1,"bidfloorcur":"USD","banner":{"w":300,"h":250}}]}`)
	rr := post(t, h, display)
	if rr.Code != http.StatusOK {
		t.Fatalf("display status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "<img") {
		t.Fatalf("expected display adm: %s", rr.Body.String())
	}
	native := []byte(`{"id":"native_auction","tmax":100,"site":{"domain":"example.com"},"imp":[{"id":"1","tagid":"native_1","bidfloor":1,"bidfloorcur":"USD","native":{"request":"{}"}}]}`)
	rr = post(t, h, native)
	if rr.Code != http.StatusOK {
		t.Fatalf("native status=%d body=%s", rr.Code, rr.Body.String())
	}
	var nativeResp openrtb.BidResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &nativeResp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(nativeResp.SeatBid[0].Bid[0].AdM, `"native"`) {
		t.Fatalf("expected native adm: %s", nativeResp.SeatBid[0].Bid[0].AdM)
	}
}

func TestTMaxTooLowNoBid(t *testing.T) {
	h := testHandler(t)
	req := sampleVideoRequestMap(t)
	req["tmax"] = 1
	rr := post(t, h, mustJSON(t, req))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAuthAndSignature(t *testing.T) {
	cfg := sampleConfig(t)
	cfg.AuthToken = "token"
	cfg.SigningSecret = "secret"
	cfg.RequireAuth = true
	cfg.RequireSignature = true
	h := New(cfg, bidder.NewEngine(cfg))
	body := sampleVideoRequest(t)
	req := httptest.NewRequest(http.MethodPost, "/openrtb", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer token")
	ts := time.Now().Unix()
	req.Header.Set("X-ClearLedger-Timestamp", strconvFormat(ts))
	req.Header.Set("X-ClearLedger-Signature", sign("secret", ts, body))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestProductionBuyerSignatureHeaders(t *testing.T) {
	cfg := sampleConfig(t)
	cfg.AuthToken = "token"
	cfg.SigningSecret = "secret"
	cfg.RequireAuth = true
	cfg.RequireSignature = true
	h := New(cfg, bidder.NewEngine(cfg))
	body := sampleVideoRequest(t)
	req := httptest.NewRequest(http.MethodPost, "/buyers/agency_bidder_1/openrtb", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer token")
	applyProductionSignature(req, "secret", "auction_123", "auction_123", body)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestProductionBuyerSignatureRejectsBodyHashMismatch(t *testing.T) {
	cfg := sampleConfig(t)
	cfg.AuthToken = "token"
	cfg.SigningSecret = "secret"
	cfg.RequireAuth = true
	cfg.RequireSignature = true
	h := New(cfg, bidder.NewEngine(cfg))
	body := sampleVideoRequest(t)
	req := httptest.NewRequest(http.MethodPost, "/openrtb", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer token")
	applyProductionSignature(req, "secret", "auction_123", "auction_123", []byte(`{"different":true}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "body_hash_mismatch") {
		t.Fatalf("expected body hash mismatch, got %s", rr.Body.String())
	}
}

func TestStateAndMetricsExposeSanitizedRuntimeState(t *testing.T) {
	cfg := sampleConfig(t)
	cfg.AuthToken = "secret-token"
	cfg.SigningSecret = "secret-signing-key"
	h := New(cfg, bidder.NewEngine(cfg))
	if rr := post(t, h, sampleVideoRequest(t)); rr.Code != http.StatusOK {
		t.Fatalf("bid status=%d body=%s", rr.Code, rr.Body.String())
	}
	stateReq := httptest.NewRequest(http.MethodGet, "/statez", nil)
	stateRR := httptest.NewRecorder()
	h.ServeHTTP(stateRR, stateReq)
	if stateRR.Code != http.StatusOK {
		t.Fatalf("state status=%d body=%s", stateRR.Code, stateRR.Body.String())
	}
	body := stateRR.Body.String()
	for _, want := range []string{`"buyer_id":"agency_bidder_1"`, `"spend_today":0.00925`, `"approved_creative_count":1`} {
		if !strings.Contains(body, want) {
			t.Fatalf("state missing %s in %s", want, body)
		}
	}
	if strings.Contains(body, cfg.AuthToken) || strings.Contains(body, cfg.SigningSecret) {
		t.Fatalf("state leaked secrets: %s", body)
	}

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRR := httptest.NewRecorder()
	h.ServeHTTP(metricsRR, metricsReq)
	metrics := metricsRR.Body.String()
	for _, want := range []string{
		`clearledger_bidder_openrtb_requests_total{result="bid"} 1`,
		`clearledger_bidder_campaign_spend_usd{campaign_id="campaign_video_1"} 0.009250`,
		`clearledger_bidder_campaign_enabled{campaign_id="campaign_video_1"} 1`,
	} {
		if !strings.Contains(metrics, want) {
			t.Fatalf("metrics missing %s in %s", want, metrics)
		}
	}
}

func TestEventEndpointCountsNoticeCallbacks(t *testing.T) {
	h := testHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/events/win?auction_id=a&bid_id=b", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("event status=%d body=%s", rr.Code, rr.Body.String())
	}
	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRR := httptest.NewRecorder()
	h.ServeHTTP(metricsRR, metricsReq)
	if !strings.Contains(metricsRR.Body.String(), `clearledger_bidder_openrtb_requests_total{result="event_win"} 1`) {
		t.Fatalf("missing event metric: %s", metricsRR.Body.String())
	}
}

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	cfg := sampleConfig(t)
	return New(cfg, bidder.NewEngine(cfg))
}

func sampleConfig(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Load("../../config/campaigns.sample.json")
	if err != nil {
		t.Fatal(err)
	}
	cfg.PublicEndpoint = "http://bidder.test"
	return cfg
}

func sampleVideoRequest(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile("../../samples/openrtb-video-request.json")
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func sampleVideoRequestMap(t *testing.T) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(sampleVideoRequest(t), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func post(t *testing.T, h http.Handler, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/openrtb", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func sign(secret string, ts int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconvFormat(ts)))
	mac.Write([]byte("."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func applyProductionSignature(req *http.Request, secret, auctionID, requestID string, body []byte) {
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	bodyHash := sha256Body(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "\n" + auctionID + "\n" + requestID + "\n" + bodyHash))
	req.Header.Set("X-ClearLedger-Buyer-Timestamp", timestamp)
	req.Header.Set("X-ClearLedger-Auction-ID", auctionID)
	req.Header.Set("X-ClearLedger-Request-ID", requestID)
	req.Header.Set("X-ClearLedger-Buyer-Body-SHA256", bodyHash)
	req.Header.Set("X-ClearLedger-Buyer-Signature", "hmac-sha256="+hex.EncodeToString(mac.Sum(nil)))
}

func sha256Body(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func strconvFormat(v int64) string {
	return fmt.Sprintf("%d", v)
}
