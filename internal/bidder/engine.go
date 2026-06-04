package bidder

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Numi2/clearledger-bidder-openrtb/internal/config"
	"github.com/Numi2/clearledger-bidder-openrtb/internal/openrtb"
)

type Engine struct {
	cfg    config.Config
	mu     sync.Mutex
	spend  map[string]float64
	qps    map[string]rateState
	dayKey string
}

type rateState struct {
	sec   int64
	count int
}

type Decision struct {
	Response *openrtb.BidResponse
	NoBid    bool
	Reason   string
}

func NewEngine(cfg config.Config) *Engine {
	return &Engine{
		cfg:    cfg,
		spend:  map[string]float64{},
		qps:    map[string]rateState{},
		dayKey: time.Now().UTC().Format("2006-01-02"),
	}
}

func (e *Engine) Bid(ctx context.Context, req *openrtb.BidRequest, received time.Time) Decision {
	if req.TMax > 0 {
		deadline := received.Add(time.Duration(req.TMax) * time.Millisecond)
		if time.Until(deadline) <= 5*time.Millisecond {
			return Decision{NoBid: true, Reason: "tmax_too_low"}
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline.Add(-2*time.Millisecond))
		defer cancel()
	}
	select {
	case <-ctx.Done():
		return Decision{NoBid: true, Reason: "timeout"}
	default:
	}

	best := candidate{}
	for _, imp := range req.Imp {
		for _, campaign := range e.cfg.Campaigns {
			if !campaign.Enabled {
				continue
			}
			current, ok := e.evaluate(req, imp, campaign)
			if !ok {
				continue
			}
			if !best.ok || current.price > best.price {
				best = current
			}
		}
	}
	if !best.ok {
		return Decision{NoBid: true, Reason: "no_eligible_campaign"}
	}
	if !e.reserve(best.campaign.ID, best.price, best.campaign.DailyBudget, best.campaign.QPS) {
		return Decision{NoBid: true, Reason: "budget_or_qps_exhausted"}
	}

	bidID := stableID("bid", req.ID, best.imp.ID, best.campaign.ID, best.creative.ID, time.Now().UTC().Format(time.RFC3339Nano))
	bid := openrtb.Bid{
		ID:      bidID,
		ImpID:   best.imp.ID,
		Price:   roundCPM(best.price),
		AdID:    best.creative.ID,
		CID:     best.campaign.ID,
		CrID:    best.creative.ID,
		Adomain: best.creative.Adomain,
		DealID:  best.dealID,
		AdM:     renderMarkup(best.imp, best.creative, e.eventURL(best.creative, "imp", req.ID, bidID)),
		NURL:    e.eventURL(best.creative, "win", req.ID, bidID),
		BURL:    e.eventURL(best.creative, "bill", req.ID, bidID),
		LURL:    e.eventURL(best.creative, "loss", req.ID, bidID),
		W:       best.creative.W,
		H:       best.creative.H,
		Ext: map[string]any{
			"clearledger": map[string]any{
				"buyer_id":    e.cfg.BuyerID,
				"campaign_id": best.campaign.ID,
				"creative_id": best.creative.ID,
				"bidder":      "clearledger-bidder-openrtb",
			},
		},
	}
	return Decision{Response: &openrtb.BidResponse{
		ID:  req.ID,
		Cur: e.cfg.Currency,
		SeatBid: []openrtb.SeatBid{{
			Seat: best.campaign.Seat,
			Bid:  []openrtb.Bid{bid},
		}},
		Ext: map[string]any{"clearledger": map[string]any{"no_clearing_in_bidder": true}},
	}}
}

type candidate struct {
	ok       bool
	campaign config.Campaign
	creative config.Creative
	imp      openrtb.Impression
	dealID   string
	price    float64
}

func (e *Engine) evaluate(req *openrtb.BidRequest, imp openrtb.Impression, campaign config.Campaign) (candidate, bool) {
	if !contains(campaign.MediaTypes, imp.MediaType()) {
		return candidate{}, false
	}
	if !allowedSupply(req, imp, campaign) || !allowedPrivacy(req, campaign) {
		return candidate{}, false
	}
	dealID, dealFloor, ok := matchDeal(imp, campaign.DealIDs)
	if !ok {
		return candidate{}, false
	}
	floor := math.Max(imp.BidFloor, dealFloor)
	if campaign.BidCPM < floor {
		return candidate{}, false
	}
	if imp.BidFloorCur != "" && !strings.EqualFold(imp.BidFloorCur, e.cfg.Currency) {
		return candidate{}, false
	}
	creative, ok := chooseCreative(imp.MediaType(), req, campaign.Creatives)
	if !ok {
		return candidate{}, false
	}
	return candidate{ok: true, campaign: campaign, creative: creative, imp: imp, dealID: dealID, price: campaign.BidCPM}, true
}

func (e *Engine) reserve(campaignID string, priceCPM, dailyBudget float64, qps int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	day := time.Now().UTC().Format("2006-01-02")
	if e.dayKey != day {
		e.spend = map[string]float64{}
		e.qps = map[string]rateState{}
		e.dayKey = day
	}
	if qps > 0 {
		nowSec := time.Now().Unix()
		state := e.qps[campaignID]
		if state.sec != nowSec {
			state = rateState{sec: nowSec}
		}
		if state.count >= qps {
			e.qps[campaignID] = state
			return false
		}
		state.count++
		e.qps[campaignID] = state
	}
	next := e.spend[campaignID] + priceCPM/1000
	if dailyBudget > 0 && next > dailyBudget {
		return false
	}
	e.spend[campaignID] = next
	return true
}

func allowedSupply(req *openrtb.BidRequest, imp openrtb.Impression, c config.Campaign) bool {
	if len(c.AllowedPlacements) > 0 && !contains(c.AllowedPlacements, imp.TagID) {
		return false
	}
	if req.App != nil {
		if len(c.AllowedApps) > 0 && !contains(c.AllowedApps, req.App.ID) {
			return false
		}
		if len(c.AllowedBundles) > 0 && !contains(c.AllowedBundles, req.App.Bundle) {
			return false
		}
		return true
	}
	if req.Site != nil && len(c.AllowedDomains) > 0 {
		return contains(c.AllowedDomains, req.Site.Domain)
	}
	return true
}

func allowedPrivacy(req *openrtb.BidRequest, c config.Campaign) bool {
	coppa, _ := req.Regs["coppa"].(float64)
	for _, cr := range c.Creatives {
		if cr.BlockedCOPPA && int(coppa) == 1 {
			return false
		}
	}
	if req.Device != nil && req.Device.LMT == 1 {
		for _, cr := range c.Creatives {
			if cr.RequiresIFA {
				return false
			}
		}
	}
	if len(c.GeoCountries) > 0 {
		country := ""
		if req.Device != nil && req.Device.Geo != nil {
			if raw, ok := req.Device.Geo["country"].(string); ok {
				country = raw
			}
		}
		return contains(c.GeoCountries, country)
	}
	return true
}

func matchDeal(imp openrtb.Impression, allowed []string) (string, float64, bool) {
	if len(allowed) == 0 {
		if imp.PMP != nil && imp.PMP.PrivateAuction == 1 {
			return "", 0, false
		}
		return "", 0, true
	}
	if imp.PMP == nil {
		return "", 0, false
	}
	for _, deal := range imp.PMP.Deals {
		if contains(allowed, deal.ID) {
			return deal.ID, deal.BidFloor, true
		}
	}
	return "", 0, false
}

func chooseCreative(mediaType string, req *openrtb.BidRequest, creatives []config.Creative) (config.Creative, bool) {
	for _, creative := range creatives {
		if !creative.Approved || !strings.EqualFold(creative.MediaType, mediaType) {
			continue
		}
		if creative.RequiresIFA && (req.Device == nil || req.Device.IFA == "") {
			continue
		}
		return creative, true
	}
	return config.Creative{}, false
}

func renderMarkup(imp openrtb.Impression, cr config.Creative, impURL string) string {
	if cr.Markup != "" {
		return strings.ReplaceAll(cr.Markup, "{{IMPRESSION_URL}}", xmlEscape(impURL))
	}
	switch imp.MediaType() {
	case "video", "audio":
		duration := cr.Duration
		if duration <= 0 {
			duration = 30
		}
		return fmt.Sprintf(`<VAST version="4.3"><Ad id="%s"><InLine><AdSystem>ClearLedger Bidder OpenRTB</AdSystem><AdTitle>%s</AdTitle><Impression><![CDATA[%s]]></Impression><Creatives><Creative id="%s"><Linear><Duration>00:00:%02d</Duration><MediaFiles><MediaFile delivery="progressive" type="video/mp4" width="%d" height="%d"><![CDATA[%s]]></MediaFile></MediaFiles><VideoClicks><ClickThrough><![CDATA[%s]]></ClickThrough></VideoClicks></Linear></Creative></Creatives></InLine></Ad></VAST>`, cr.ID, cr.ID, impURL, cr.ID, duration, max(cr.W, 640), max(cr.H, 360), cr.AssetURL, cr.LandingURL)
	case "native":
		body, _ := json.Marshal(map[string]any{"native": map[string]any{"link": map[string]any{"url": cr.LandingURL}, "assets": []map[string]any{{"id": 1, "title": map[string]any{"text": cr.ID}}}, "imptrackers": []string{impURL}}})
		return string(body)
	default:
		w, h := max(cr.W, 300), max(cr.H, 250)
		return fmt.Sprintf(`<a href="%s" target="_blank" rel="noopener"><img src="%s" width="%d" height="%d" alt=""></a><img src="%s" width="1" height="1" alt="">`, htmlEscape(cr.LandingURL), htmlEscape(cr.AssetURL), w, h, htmlEscape(impURL))
	}
}

func (e *Engine) eventURL(cr config.Creative, event, auctionID, bidID string) string {
	base := cr.NoticeBaseURL
	if base == "" {
		base = e.cfg.PublicEndpoint
	}
	if base == "" {
		return ""
	}
	u, err := url.Parse(strings.TrimRight(base, "/") + "/events/" + event)
	if err != nil {
		return ""
	}
	q := u.Query()
	q.Set("auction_id", auctionID)
	q.Set("bid_id", bidID)
	q.Set("creative_id", cr.ID)
	u.RawQuery = q.Encode()
	return u.String()
}

func stableID(prefix string, parts ...string) string {
	h := sha1.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return prefix + "_" + hex.EncodeToString(h.Sum(nil))[:16]
}

func contains(values []string, needle string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == needle {
			return true
		}
	}
	return false
}

func roundCPM(v float64) float64 { return math.Round(v*10000) / 10000 }
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func htmlEscape(s string) string {
	return strings.NewReplacer(`&`, `&amp;`, `"`, `&#34;`, `<`, `&lt;`, `>`, `&gt;`).Replace(s)
}
func xmlEscape(s string) string { return htmlEscape(s) }
