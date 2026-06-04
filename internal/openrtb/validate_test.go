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
				AdM:     `<VAST version="4.3"><Ad><InLine><Impression>https://example.com/i</Impression><Creatives><Creative><Linear><Duration>00:00:30</Duration><MediaFiles><MediaFile delivery="progressive" type="video/mp4" width="1280" height="720">https://example.com/a.mp4</MediaFile></MediaFiles></Linear></Creative></Creatives></InLine></Ad></VAST>`,
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

func TestDecodeRequestRejectsInvalidTimingAndMediaBounds(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "negative tmax",
			body: `{"id":"a","tmax":-1,"site":{"domain":"example.com"},"imp":[{"id":"1","banner":{"w":300,"h":250}}]}`,
		},
		{
			name: "negative banner width",
			body: `{"id":"a","site":{"domain":"example.com"},"imp":[{"id":"1","banner":{"w":-300,"h":250}}]}`,
		},
		{
			name: "negative video duration",
			body: `{"id":"a","app":{"bundle":"com.example"},"imp":[{"id":"1","video":{"mimes":["video/mp4"],"minduration":-1,"maxduration":30}}]}`,
		},
		{
			name: "video min exceeds max",
			body: `{"id":"a","app":{"bundle":"com.example"},"imp":[{"id":"1","video":{"mimes":["video/mp4"],"minduration":45,"maxduration":30}}]}`,
		},
		{
			name: "negative video dimensions",
			body: `{"id":"a","app":{"bundle":"com.example"},"imp":[{"id":"1","video":{"mimes":["video/mp4"],"w":-1,"h":720}}]}`,
		},
		{
			name: "audio min exceeds max",
			body: `{"id":"a","app":{"bundle":"com.example"},"imp":[{"id":"1","audio":{"mimes":["audio/mpeg"],"minduration":45,"maxduration":30}}]}`,
		},
		{
			name: "malformed native request",
			body: `{"id":"a","site":{"domain":"example.com"},"imp":[{"id":"1","native":{"request":"not-json"}}]}`,
		},
		{
			name: "native request without assets",
			body: `{"id":"a","site":{"domain":"example.com"},"imp":[{"id":"1","native":{"request":"{\"native\":{\"ver\":\"1.2\",\"assets\":[]}}"}}]}`,
		},
		{
			name: "native request duplicate asset ids",
			body: `{"id":"a","site":{"domain":"example.com"},"imp":[{"id":"1","native":{"request":"{\"native\":{\"ver\":\"1.2\",\"assets\":[{\"id\":1},{\"id\":1}]}}"}}]}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeRequest([]byte(tc.body)); err == nil {
				t.Fatal("expected malformed request")
			}
		})
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

func TestValidateBidResponseRejectsNonPositivePrice(t *testing.T) {
	req := &BidRequest{
		ID:   "auction",
		Cur:  []string{"USD"},
		Site: &Site{Domain: "example.com"},
		Imp:  []Impression{{ID: "1", BidFloor: 0, Banner: &Banner{W: 300, H: 250}}},
	}
	resp := &BidResponse{
		ID:  "auction",
		Cur: "USD",
		SeatBid: []SeatBid{{
			Seat: "seat",
			Bid: []Bid{{
				ID:      "bid",
				ImpID:   "1",
				Price:   0,
				CrID:    "creative",
				Adomain: []string{"advertiser.com"},
				AdM:     `<img src="https://cdn.example/ad.png">`,
				NURL:    "https://bidder.example/win",
			}},
		}},
	}
	if err := ValidateBidResponse(req, resp); err == nil {
		t.Fatal("expected non-positive price rejection")
	}
	resp.SeatBid[0].Bid[0].Price = 0.01
	if err := ValidateBidResponse(req, resp); err != nil {
		t.Fatal(err)
	}
}

func TestValidateBidResponseRejectsBlankAdomain(t *testing.T) {
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
				Adomain: []string{" "},
				AdM:     `<img src="https://cdn.example/ad.png">`,
				NURL:    "https://bidder.example/win",
			}},
		}},
	}
	if err := ValidateBidResponse(req, resp); err == nil {
		t.Fatal("expected blank adomain rejection")
	}
	resp.SeatBid[0].Bid[0].Adomain = []string{"advertiser.com"}
	if err := ValidateBidResponse(req, resp); err != nil {
		t.Fatal(err)
	}
}

func TestValidateBidResponseRejectsInvalidNoticeURLs(t *testing.T) {
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
				AdM:     `<img src="https://cdn.example/ad.png">`,
				NURL:    "javascript:alert(1)",
			}},
		}},
	}
	if err := ValidateBidResponse(req, resp); err == nil {
		t.Fatal("expected invalid notice URL rejection")
	}
	resp.SeatBid[0].Bid[0].NURL = "https://bidder.example/win"
	if err := ValidateBidResponse(req, resp); err != nil {
		t.Fatal(err)
	}
}

func TestValidateBidResponseRejectsEmptyAndDuplicateBids(t *testing.T) {
	req := &BidRequest{
		ID:   "auction",
		Cur:  []string{"USD"},
		Site: &Site{Domain: "example.com"},
		Imp:  []Impression{{ID: "1", BidFloor: 1, Banner: &Banner{W: 300, H: 250}}},
	}
	resp := &BidResponse{
		ID:      "auction",
		Cur:     "USD",
		SeatBid: []SeatBid{{Seat: "seat"}},
	}
	if err := ValidateBidResponse(req, resp); err == nil {
		t.Fatal("expected empty bid array to fail")
	}

	validBid := Bid{
		ID:      "bid",
		ImpID:   "1",
		Price:   1,
		CrID:    "creative",
		Adomain: []string{"advertiser.com"},
		AdM:     `<img src="https://cdn.example/ad.png">`,
		NURL:    "https://bidder.example/win",
	}
	resp.SeatBid = []SeatBid{{Seat: "seat", Bid: []Bid{validBid, validBid}}}
	if err := ValidateBidResponse(req, resp); err == nil {
		t.Fatal("expected duplicate bid id to fail")
	}

	duplicateImpBid := validBid
	duplicateImpBid.ID = "bid_2"
	resp.SeatBid = []SeatBid{{Seat: "seat", Bid: []Bid{validBid, duplicateImpBid}}}
	if err := ValidateBidResponse(req, resp); err == nil {
		t.Fatal("expected multiple bids for the same impid to fail")
	}
}

func TestValidateBidResponseValidatesPMPDealPerImpression(t *testing.T) {
	req := &BidRequest{
		ID:   "auction",
		Cur:  []string{"USD"},
		Site: &Site{Domain: "example.com"},
		Imp: []Impression{
			{
				ID:          "1",
				BidFloor:    1,
				BidFloorCur: "USD",
				Banner:      &Banner{W: 300, H: 250},
				PMP:         &PMP{PrivateAuction: 1, Deals: []Deal{{ID: "deal_1", BidFloor: 1, BidFloorCur: "USD"}}},
			},
			{
				ID:          "2",
				BidFloor:    1,
				BidFloorCur: "USD",
				Banner:      &Banner{W: 300, H: 250},
				PMP:         &PMP{PrivateAuction: 1, Deals: []Deal{{ID: "deal_2", BidFloor: 1, BidFloorCur: "USD"}}},
			},
		},
	}
	resp := &BidResponse{
		ID:  "auction",
		Cur: "USD",
		SeatBid: []SeatBid{{
			Seat: "seat",
			Bid: []Bid{{
				ID:      "bid_1",
				ImpID:   "1",
				Price:   1,
				CrID:    "creative",
				Adomain: []string{"advertiser.com"},
				DealID:  "deal_2",
				AdM:     `<img src="https://cdn.example/ad.png">`,
				NURL:    "https://bidder.example/win",
			}},
		}},
	}
	if err := ValidateBidResponse(req, resp); err == nil {
		t.Fatal("expected wrong impression deal id to fail")
	}
	resp.SeatBid[0].Bid[0].DealID = "deal_1"
	if err := ValidateBidResponse(req, resp); err != nil {
		t.Fatal(err)
	}
}

func TestValidateBidResponseDoesNotRequireDealForNonPMPImpression(t *testing.T) {
	req := &BidRequest{
		ID:   "auction",
		Cur:  []string{"USD"},
		Site: &Site{Domain: "example.com"},
		Imp: []Impression{
			{
				ID:          "1",
				BidFloor:    1,
				BidFloorCur: "USD",
				Banner:      &Banner{W: 300, H: 250},
				PMP:         &PMP{PrivateAuction: 1, Deals: []Deal{{ID: "deal_1", BidFloor: 1, BidFloorCur: "USD"}}},
			},
			{
				ID:          "2",
				BidFloor:    1,
				BidFloorCur: "USD",
				Banner:      &Banner{W: 300, H: 250},
			},
		},
	}
	resp := &BidResponse{
		ID:  "auction",
		Cur: "USD",
		SeatBid: []SeatBid{{
			Seat: "seat",
			Bid: []Bid{{
				ID:      "bid_2",
				ImpID:   "2",
				Price:   1,
				CrID:    "creative",
				Adomain: []string{"advertiser.com"},
				AdM:     `<img src="https://cdn.example/ad.png">`,
				NURL:    "https://bidder.example/win",
			}},
		}},
	}
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

func TestValidateBidResponseRejectsVASTOutsideMediaConstraints(t *testing.T) {
	req := &BidRequest{
		ID:  "auction",
		Cur: []string{"USD"},
		App: &App{Bundle: "com.example.app"},
		Imp: []Impression{{
			ID:       "1",
			Secure:   1,
			BidFloor: 1,
			Video:    &Video{Mimes: []string{"video/mp4"}, MinDuration: 5, MaxDuration: 30, W: 1280, H: 720},
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
				AdM:     `<VAST version="4.3"><Ad><InLine><Impression>https://example.com/i</Impression><Creatives><Creative><Linear><Duration>00:00:45</Duration><MediaFiles><MediaFile delivery="progressive" type="video/webm" width="640" height="360">https://example.com/a.webm</MediaFile></MediaFiles></Linear></Creative></Creatives></InLine></Ad></VAST>`,
				NURL:    "https://bidder.example/win",
			}},
		}},
	}
	if err := ValidateBidResponse(req, resp); err == nil {
		t.Fatal("expected VAST media constraints to fail")
	}
	resp.SeatBid[0].Bid[0].AdM = `<VAST version="4.3"><Ad><InLine><Impression>https://example.com/i</Impression><Creatives><Creative><Linear><Duration>00:00:30</Duration><MediaFiles><MediaFile delivery="progressive" type="video/mp4" width="1280" height="720">https://example.com/a.mp4</MediaFile></MediaFiles></Linear></Creative></Creatives></InLine></Ad></VAST>`
	if err := ValidateBidResponse(req, resp); err != nil {
		t.Fatal(err)
	}
}

func TestValidateBidResponseRequiresNativeRequiredAssets(t *testing.T) {
	req := &BidRequest{
		ID:   "auction",
		Cur:  []string{"USD"},
		Site: &Site{Domain: "example.com"},
		Imp: []Impression{{
			ID:       "1",
			BidFloor: 1,
			Native:   &Native{Request: `{"native":{"ver":"1.2","assets":[{"id":1,"required":1},{"id":2}]}}`},
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
				AdM:     `{"native":{"assets":[{"id":2,"title":{"text":"creative"}}],"link":{"url":"https://advertiser.com"},"imptrackers":["https://tracker.example/imp"]}}`,
				NURL:    "https://bidder.example/win",
			}},
		}},
	}
	if err := ValidateBidResponse(req, resp); err == nil {
		t.Fatal("expected missing required native asset rejection")
	}
	resp.SeatBid[0].Bid[0].AdM = `{"native":{"assets":[{"id":1,"title":{"text":"creative"}}],"link":{"url":"https://advertiser.com"},"imptrackers":["https://tracker.example/imp"]}}`
	if err := ValidateBidResponse(req, resp); err != nil {
		t.Fatal(err)
	}
}

func TestNativeResponseAssetIDsUsesRequiredAssets(t *testing.T) {
	imp := Impression{Native: &Native{Request: `{"native":{"assets":[{"id":2},{"id":7,"required":1},{"id":9,"required":1}]}}`}}
	got := NativeResponseAssetIDs(imp)
	if len(got) != 2 || got[0] != 7 || got[1] != 9 {
		t.Fatalf("required asset ids=%v", got)
	}

	imp.Native.Request = `{"native":{"assets":[{"id":4},{"id":5}]}}`
	got = NativeResponseAssetIDs(imp)
	if len(got) != 1 || got[0] != 4 {
		t.Fatalf("fallback asset ids=%v", got)
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
