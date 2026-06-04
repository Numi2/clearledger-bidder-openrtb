package openrtb

import (
	"encoding/json"
	"os"
	"testing"
)

func TestValidateBidResponse(t *testing.T) {
	body, err := os.ReadFile("../../samples/openrtb-video-request.json")
	if err != nil {
		t.Fatal(err)
	}
	req, err := DecodeRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	resp := &BidResponse{
		ID:  req.ID,
		Cur: "USD",
		SeatBid: []SeatBid{{
			Seat: "seat_1",
			Bid: []Bid{{
				ID:      "bid_1",
				ImpID:   "1",
				Price:   9.25,
				CrID:    "creative_1",
				Adomain: []string{"advertiser.com"},
				DealID:  "deal_clearledger_123",
				AdM:     `<VAST version="4.3"><Ad><InLine><Impression>https://example.com/i</Impression><Creatives><Creative><Linear><Duration>00:00:30</Duration><MediaFiles><MediaFile>https://example.com/a.mp4</MediaFile></MediaFiles></Linear></Creative></Creatives></InLine></Ad></VAST>`,
				Ext: map[string]any{"clearledger": map[string]any{
					"buyer_id":         "buyer",
					"campaign_id":      "campaign",
					"creative_id":      "creative_1",
					"lane_id":          "lane_123",
					"package_id":       "package_123",
					"placement_id":     "placement_123",
					"proof_run_id":     "proof_123",
					"receipt_required": true,
				}},
			}},
		}},
	}
	if err := ValidateBidResponse(req, resp); err != nil {
		t.Fatal(err)
	}
	if !LooksLikeVAST(resp.SeatBid[0].Bid[0].AdM) {
		t.Fatal("expected VAST helper to accept markup")
	}
	resp.SeatBid[0].Bid[0].ImpID = "wrong"
	if err := ValidateBidResponse(req, resp); err == nil {
		t.Fatal("expected impid mismatch")
	}
}

func TestDecodeRequestAllowsExtensions(t *testing.T) {
	body := []byte(`{"id":"a","site":{"domain":"example.com","publisher":{"id":"pub"}},"imp":[{"id":"1","banner":{"w":1,"h":1},"ext":{"clearledger":{"lane_id":"lane"}}}],"ext":{"x":1}}`)
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeRequest(body); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeRequestRejectsTrailingJSON(t *testing.T) {
	body := []byte(`{"id":"a","site":{"domain":"example.com"},"imp":[{"id":"1","banner":{"w":1,"h":1}}]} {"id":"b"}`)
	if _, err := DecodeRequest(body); err == nil {
		t.Fatal("expected trailing JSON rejection")
	}
}

func TestValidateBidResponseRequiresNoticeOrProofExt(t *testing.T) {
	req := &BidRequest{
		ID:   "auction",
		Cur:  []string{"USD"},
		Site: &Site{Domain: "example.com"},
		Imp:  []Impression{{ID: "1", BidFloor: 1, Banner: &Banner{W: 300, H: 250}}},
	}
	resp := &BidResponse{
		ID:  "auction",
		Cur: "USD",
		SeatBid: []SeatBid{{
			Seat: "seat",
			Bid: []Bid{{
				ID:      "bid",
				ImpID:   "1",
				Price:   1,
				CrID:    "creative",
				Adomain: []string{"advertiser.com"},
				AdM:     `<a href="https://advertiser.com"><img src="https://cdn.example/ad.png" width="300" height="250" alt=""></a>`,
			}},
		}},
	}
	if err := ValidateBidResponse(req, resp); err == nil {
		t.Fatal("expected missing notices/proof ext rejection")
	}
	resp.SeatBid[0].Bid[0].NURL = "https://bidder.example/win"
	if err := ValidateBidResponse(req, resp); err != nil {
		t.Fatal(err)
	}
}

func TestValidateBidResponseRequiresClearLedgerProofFieldsWhenReceiptRequired(t *testing.T) {
	req := &BidRequest{
		ID:   "auction",
		Cur:  []string{"USD"},
		Site: &Site{Domain: "example.com"},
		Imp: []Impression{{
			ID:       "1",
			BidFloor: 1,
			Banner:   &Banner{W: 300, H: 250},
			Ext: map[string]any{"clearledger": map[string]any{
				"lane_id":          "lane",
				"placement_id":     "placement",
				"proof_run_id":     "proof",
				"receipt_required": true,
			}},
		}},
	}
	resp := &BidResponse{
		ID:  "auction",
		Cur: "USD",
		SeatBid: []SeatBid{{
			Seat: "seat",
			Bid: []Bid{{
				ID:      "bid",
				ImpID:   "1",
				Price:   1,
				CrID:    "creative",
				Adomain: []string{"advertiser.com"},
				AdM:     `<img src="https://cdn.example/ad.png">`,
				NURL:    "https://bidder.example/win",
				Ext: map[string]any{"clearledger": map[string]any{
					"buyer_id":         "buyer",
					"campaign_id":      "campaign",
					"creative_id":      "creative",
					"lane_id":          "wrong",
					"placement_id":     "placement",
					"proof_run_id":     "proof",
					"receipt_required": true,
				}},
			}},
		}},
	}
	if err := ValidateBidResponse(req, resp); err == nil {
		t.Fatal("expected proof field mismatch")
	}
	resp.SeatBid[0].Bid[0].Ext["clearledger"].(map[string]any)["lane_id"] = "lane"
	if err := ValidateBidResponse(req, resp); err != nil {
		t.Fatal(err)
	}
}

func TestValidateBidResponseRejectsWrongMediaMarkup(t *testing.T) {
	req := &BidRequest{
		ID:   "auction",
		Cur:  []string{"USD"},
		Site: &Site{Domain: "example.com"},
		Imp:  []Impression{{ID: "1", BidFloor: 1, Video: &Video{Mimes: []string{"video/mp4"}}}},
	}
	resp := &BidResponse{
		ID:  "auction",
		Cur: "USD",
		SeatBid: []SeatBid{{
			Seat: "seat",
			Bid: []Bid{{
				ID:      "bid",
				ImpID:   "1",
				Price:   1,
				CrID:    "creative",
				Adomain: []string{"advertiser.com"},
				AdM:     `<img src="https://example.com/ad.png">`,
				NURL:    "https://bidder.example/win",
			}},
		}},
	}
	if err := ValidateBidResponse(req, resp); err == nil {
		t.Fatal("expected video response with display adm to fail")
	}
}

func TestLooksLikeNativeAdM(t *testing.T) {
	valid := `{"native":{"assets":[{"id":1,"title":{"text":"creative"}}],"link":{"url":"https://advertiser.com"},"imptrackers":["https://tracker.example/imp"]}}`
	if !LooksLikeNativeAdM(valid) {
		t.Fatal("expected native adm to validate")
	}
	if LooksLikeNativeAdM(`{"native":{"assets":[],"link":{"url":""}}}`) {
		t.Fatal("expected incomplete native adm to fail")
	}
}
