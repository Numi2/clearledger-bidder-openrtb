package clearledger

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Numi2/clearledger-bidder-openrtb/internal/openrtb"
)

type HarnessOptions struct {
	ManifestPath     string
	PrivateMarketID  string
	BuyerID          string
	SamplePath       string
	EndpointOverride string
	AuthToken        string
	SigningSecret    string
	Timeout          time.Duration
}

type Report struct {
	OK              bool            `json:"ok"`
	CheckedAt       string          `json:"checked_at"`
	LaneID          string          `json:"lane_id"`
	PrivateMarketID string          `json:"private_market_id"`
	Winner          *WinnerSummary  `json:"winner,omitempty"`
	SupplyResponse  *SupplyResponse `json:"supply_response,omitempty"`
	ProofSteps      []ProofStep     `json:"proof_steps"`
	Checks          []Check         `json:"checks"`
}

type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type ProofStep struct {
	Name     string         `json:"name"`
	Owner    string         `json:"owner"`
	Complete bool           `json:"complete"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type WinnerSummary struct {
	BuyerID string  `json:"buyer_id"`
	SeatID  string  `json:"seat_id"`
	BidID   string  `json:"bid_id"`
	ImpID   string  `json:"impid"`
	Price   float64 `json:"price"`
	CrID    string  `json:"crid"`
	DealID  string  `json:"dealid,omitempty"`
}

type SupplyResponse struct {
	Type         string `json:"type"`
	RequestID    string `json:"request_id"`
	AdMarkup     string `json:"adm,omitempty"`
	VAST         string `json:"vast,omitempty"`
	ClearingNote string `json:"clearing_note"`
}

type candidate struct {
	buyer ApprovedBuyer
	resp  openrtb.BidResponse
	bid   openrtb.Bid
}

func RunHarness(ctx context.Context, options HarnessOptions) (Report, error) {
	if options.Timeout <= 0 {
		options.Timeout = 2 * time.Second
	}
	report := Report{OK: true, CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	add := func(name string, ok bool, detail string) {
		report.Checks = append(report.Checks, Check{Name: name, OK: ok, Detail: detail})
		if !ok {
			report.OK = false
		}
	}
	step := func(name, owner string, complete bool, metadata map[string]any) {
		report.ProofSteps = append(report.ProofSteps, ProofStep{Name: name, Owner: owner, Complete: complete, Metadata: metadata})
		if !complete {
			report.OK = false
		}
	}

	manifest, err := LoadManifest(options.ManifestPath)
	if err != nil {
		return report, err
	}
	lane, ok := manifest.Lane(options.PrivateMarketID)
	add("lane_found", ok, options.PrivateMarketID)
	if !ok {
		return report, nil
	}
	report.LaneID = lane.LaneID
	report.PrivateMarketID = lane.PrivateMarketID
	add("lane_active", strings.EqualFold(lane.Status, "active"), lane.Status)
	buyers := selectedBuyers(lane.ActiveApprovedBuyers(), options.BuyerID)
	add("approved_buyers", len(buyers) > 0, fmt.Sprintf("count=%d", len(buyers)))
	if len(buyers) == 0 {
		return report, nil
	}

	req, err := requestFromSample(options.SamplePath)
	if err != nil {
		return report, err
	}
	applyLaneToRequest(&req, lane)
	if reason := enforceLaneRequest(lane, req); reason != "" {
		add("lane_request_allowed", false, reason)
		return report, nil
	}
	add("lane_request_allowed", true, "")
	step("auction_request_received", "clearledger", true, map[string]any{"request_id": req.ID})
	step("approved_buyer_lane_enforced", "clearledger", true, map[string]any{"buyers": len(buyers), "lane_id": lane.LaneID})

	raw, _ := json.Marshal(req)
	client := &http.Client{Timeout: options.Timeout}
	candidates := []candidate{}
	for _, buyer := range buyers {
		resp, status, err := callBuyer(ctx, client, buyer, options, lane, req, raw)
		if err != nil {
			add("buyer_call_"+buyer.BuyerID, false, err.Error())
			continue
		}
		if status == http.StatusNoContent {
			add("buyer_call_"+buyer.BuyerID, true, "no_bid")
			continue
		}
		if status < 200 || status >= 300 {
			add("buyer_call_"+buyer.BuyerID, false, fmt.Sprintf("status=%d", status))
			continue
		}
		bid, reason, ok := validCandidate(lane, buyer, req, resp)
		add("bid_response_valid_"+buyer.BuyerID, ok, reason)
		if ok {
			candidates = append(candidates, candidate{buyer: buyer, resp: resp, bid: bid})
		}
	}
	step("openrtb_fanout_completed", "clearledger", true, map[string]any{"candidates": len(candidates)})
	if len(candidates) == 0 {
		add("winner_selected", false, "no_valid_bid")
		return report, nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].bid.Price > candidates[j].bid.Price })
	winner := candidates[0]
	report.Winner = &WinnerSummary{
		BuyerID: winner.buyer.BuyerID,
		SeatID:  winner.buyer.SeatID,
		BidID:   winner.bid.ID,
		ImpID:   winner.bid.ImpID,
		Price:   winner.bid.Price,
		CrID:    winner.bid.CrID,
		DealID:  winner.bid.DealID,
	}
	add("winner_selected", true, winner.bid.ID)
	step("winner_selected", "clearledger", true, map[string]any{"buyer_id": winner.buyer.BuyerID, "price": winner.bid.Price})
	report.SupplyResponse = buildSupplyResponse(req, winner.bid)
	step("supply_response_built", "clearledger", true, map[string]any{"type": report.SupplyResponse.Type})
	step("delivery_tracking_authority", "clearledger", true, map[string]any{"outside_bidder": true})
	step("billing_settlement_final_receipt", "clearledger", true, map[string]any{"outside_bidder": true, "bidder_receives_no_settlement_state": true})
	return report, nil
}

func requestFromSample(path string) (openrtb.BidRequest, error) {
	if path == "" {
		path = "samples/openrtb-video-request.json"
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return openrtb.BidRequest{}, err
	}
	req, err := openrtb.DecodeRequest(body)
	if err != nil {
		return openrtb.BidRequest{}, err
	}
	return *req, nil
}

func applyLaneToRequest(req *openrtb.BidRequest, lane Lane) {
	if req.ID == "" {
		req.ID = "clearledger_harness_auction"
	}
	req.Cur = []string{currency(lane.Currency)}
	if len(req.Imp) == 0 {
		return
	}
	imp := &req.Imp[0]
	imp.BidFloor = lane.FloorCPM
	imp.BidFloorCur = currency(lane.Currency)
	if len(lane.PlacementIDs) > 0 {
		imp.TagID = lane.PlacementIDs[0]
	}
	dealID := dealID(lane)
	if dealID != "" {
		imp.PMP = &openrtb.PMP{PrivateAuction: 1, Deals: []openrtb.Deal{{ID: dealID, BidFloor: lane.FloorCPM, BidFloorCur: currency(lane.Currency), AT: 1}}}
	}
	if req.App != nil && len(lane.AppBundles) > 0 {
		req.App.Bundle = lane.AppBundles[0]
	}
	if req.Source == nil {
		req.Source = map[string]any{}
	}
	req.Source["tid"] = req.ID
	if imp.Ext == nil {
		imp.Ext = map[string]any{}
	}
	imp.Ext["clearledger"] = map[string]any{
		"lane_id":           lane.LaneID,
		"private_market_id": lane.PrivateMarketID,
		"placement_id":      imp.TagID,
		"receipt_required":  true,
	}
}

func enforceLaneRequest(lane Lane, req openrtb.BidRequest) string {
	if !strings.EqualFold(lane.Status, "active") {
		return "lane_not_active"
	}
	if len(req.Imp) == 0 {
		return "missing_imp"
	}
	imp := req.Imp[0]
	if !contains(lane.Formats, imp.MediaType()) && !(imp.MediaType() == "display" && contains(lane.Formats, "banner")) {
		return "format_not_allowed"
	}
	if len(lane.PlacementIDs) > 0 && !contains(lane.PlacementIDs, imp.TagID) {
		return "placement_not_allowed"
	}
	if req.App != nil && len(lane.AppBundles) > 0 && !contains(lane.AppBundles, req.App.Bundle) {
		return "app_bundle_not_allowed"
	}
	if imp.BidFloor < lane.FloorCPM {
		return "floor_below_lane"
	}
	return ""
}

func callBuyer(ctx context.Context, client *http.Client, buyer ApprovedBuyer, options HarnessOptions, lane Lane, bidReq openrtb.BidRequest, raw []byte) (openrtb.BidResponse, int, error) {
	endpoint := strings.TrimSpace(buyer.Endpoint)
	if options.EndpointOverride != "" {
		endpoint = options.EndpointOverride
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return openrtb.BidResponse{}, 0, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("X-OpenRTB-Version", "2.6")
	httpReq.Header.Set("X-ClearLedger-Request-ID", bidReq.ID)
	httpReq.Header.Set("X-ClearLedger-Auction-ID", bidReq.ID)
	httpReq.Header.Set("X-ClearLedger-Buyer-ID", buyer.BuyerID)
	httpReq.Header.Set("X-ClearLedger-Seat-ID", buyer.SeatID)
	applyAuthHeaders(httpReq, buyer, options, bidReq.ID, bidReq.ID, raw)
	resp, err := client.Do(httpReq)
	if err != nil {
		return openrtb.BidResponse{}, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return openrtb.BidResponse{}, resp.StatusCode, nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var bidResp openrtb.BidResponse
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := json.Unmarshal(body, &bidResp); err != nil {
			return openrtb.BidResponse{}, resp.StatusCode, err
		}
	}
	return bidResp, resp.StatusCode, nil
}

func applyAuthHeaders(req *http.Request, buyer ApprovedBuyer, options HarnessOptions, auctionID, requestID string, body []byte) {
	token := secret(options.AuthToken, buyer.AuthTokenEnv, buyer.AuthToken)
	if token != "" {
		headerName := buyer.AuthHeaderName
		if headerName == "" {
			headerName = "Authorization"
		}
		if strings.EqualFold(headerName, "Authorization") && !strings.HasPrefix(strings.ToLower(token), "bearer ") {
			token = "Bearer " + token
		}
		req.Header.Set(headerName, token)
	}
	signingSecret := secret(options.SigningSecret, buyer.SigningSecretEnv, buyer.SigningSecret)
	if signingSecret == "" {
		return
	}
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	bodyHash := sha256Hex(body)
	base := timestamp + "\n" + auctionID + "\n" + requestID + "\n" + bodyHash
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(base))
	req.Header.Set("X-ClearLedger-Buyer-Timestamp", timestamp)
	req.Header.Set("X-ClearLedger-Buyer-Body-SHA256", bodyHash)
	req.Header.Set("X-ClearLedger-Buyer-Signature", "hmac-sha256="+hex.EncodeToString(mac.Sum(nil)))
}

func validCandidate(lane Lane, buyer ApprovedBuyer, req openrtb.BidRequest, resp openrtb.BidResponse) (openrtb.Bid, string, bool) {
	if err := openrtb.ValidateBidResponse(&req, &resp); err != nil {
		return openrtb.Bid{}, err.Error(), false
	}
	expectedSeat := strings.TrimSpace(buyer.SeatID)
	for _, seat := range resp.SeatBid {
		if expectedSeat != "" && strings.TrimSpace(seat.Seat) != "" && strings.TrimSpace(seat.Seat) != expectedSeat {
			return openrtb.Bid{}, "seat_mismatch", false
		}
		for _, bid := range seat.Bid {
			if lane.RequireAdomain && len(bid.Adomain) == 0 {
				return openrtb.Bid{}, "missing_adomain", false
			}
			if (req.Imp[0].MediaType() == "video" || req.Imp[0].MediaType() == "audio") && !openrtb.LooksLikeVAST(bid.AdM) {
				return openrtb.Bid{}, "invalid_vast", false
			}
			return bid, "", true
		}
	}
	return openrtb.Bid{}, "empty_bid", false
}

func buildSupplyResponse(req openrtb.BidRequest, bid openrtb.Bid) *SupplyResponse {
	resp := &SupplyResponse{
		Type:         "openrtb",
		RequestID:    req.ID,
		AdMarkup:     bid.AdM,
		ClearingNote: "ClearLedger would return this adm/VAST to supply and record delivery, billing, settlement, publisher net, fee, and final receipt outside the bidder.",
	}
	if len(req.Imp) > 0 && (req.Imp[0].MediaType() == "video" || req.Imp[0].MediaType() == "audio") {
		resp.Type = "vast"
		resp.VAST = bid.AdM
		resp.AdMarkup = ""
	}
	return resp
}

func selectedBuyers(buyers []ApprovedBuyer, buyerID string) []ApprovedBuyer {
	if buyerID == "" {
		return buyers
	}
	out := []ApprovedBuyer{}
	for _, buyer := range buyers {
		if strings.EqualFold(buyer.BuyerID, buyerID) {
			out = append(out, buyer)
		}
	}
	return out
}

func dealID(lane Lane) string {
	if lane.Metadata != nil {
		for _, key := range []string{"deal_id", "pmp_deal_id", "package_id"} {
			if value, ok := lane.Metadata[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func secret(override, envName, inline string) string {
	if override != "" {
		return strings.TrimSpace(override)
	}
	if envName != "" {
		if value := os.Getenv(envName); strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return strings.TrimSpace(inline)
}

func contains(values []string, needle string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == needle || (value == "banner" && needle == "display") {
			return true
		}
	}
	return false
}

func currency(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return "USD"
	}
	return value
}

func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
