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
				AdM:     `<VAST version="4.3"><Ad><InLine><Impression>https://example.com/i</Impression><Creatives><Creative><Linear><MediaFiles><MediaFile>https://example.com/a.mp4</MediaFile></MediaFiles></Linear></Creative></Creatives></InLine></Ad></VAST>`,
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
