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
	cfg       config.Config
	campaigns []compiledCampaign
	mu        sync.Mutex
	spend     map[string]float64
	qps       map[string]rateState
	dayKey    string
}

type rateState struct {
	sec   int64
	count int
}

type compiledCampaign struct {
	cfg               config.Campaign
	mediaTypes        stringSet
	allowedApps       stringSet
	allowedBundles    stringSet
	allowedDomains    stringSet
	allowedPlacements stringSet
	dealIDs           stringSet
	geoCountries      stringSet
	creativesByMedia  map[string][]config.Creative
	approvedCreatives int
}

type stringSet map[string]struct{}

type Decision struct {
	Response *openrtb.BidResponse
	NoBid    bool
	Reason   string
}

type CampaignSnapshot struct {
	ID                    string   `json:"id"`
	Enabled               bool     `json:"enabled"`
	Seat                  string   `json:"seat"`
	MediaTypes            []string `json:"media_types"`
	BidCPM                float64  `json:"bid_cpm"`
	DailyBudget           float64  `json:"daily_budget"`
	SpendToday            float64  `json:"spend_today"`
	PacingMode            string   `json:"pacing_mode,omitempty"`
	PacingTolerance       float64  `json:"pacing_tolerance,omitempty"`
	QPSLimit              int      `json:"qps_limit,omitempty"`
	QPSCurrentWindowUnix  int64    `json:"qps_current_window_unix,omitempty"`
	QPSCurrentWindowCount int      `json:"qps_current_window_count,omitempty"`
	CreativeCount         int      `json:"creative_count"`
	ApprovedCreativeCount int      `json:"approved_creative_count"`
	DealCount             int      `json:"deal_count"`
	PlacementCount        int      `json:"placement_count"`
}

func NewEngine(cfg config.Config) *Engine {
	return &Engine{
		cfg:       cfg,
		campaigns: compileCampaigns(cfg.Campaigns),
		spend:     map[string]float64{},
		qps:       map[string]rateState{},
		dayKey:    time.Now().UTC().Format("2006-01-02"),
	}
}

func (e *Engine) Snapshot(now time.Time) []CampaignSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	day := now.UTC().Format("2006-01-02")
	nowSec := now.Unix()
	out := make([]CampaignSnapshot, 0, len(e.campaigns))
	for _, compiled := range e.campaigns {
		campaign := compiled.cfg
		spend := 0.0
		if e.dayKey == day {
			spend = e.spend[campaign.ID]
		}
		qps := e.qps[campaign.ID]
		qpsCount := 0
		if qps.sec == nowSec {
			qpsCount = qps.count
		}
		out = append(out, CampaignSnapshot{
			ID:                    campaign.ID,
			Enabled:               campaign.Enabled,
			Seat:                  campaign.Seat,
			MediaTypes:            append([]string(nil), campaign.MediaTypes...),
			BidCPM:                campaign.BidCPM,
			DailyBudget:           campaign.DailyBudget,
			SpendToday:            roundMoney(spend),
			PacingMode:            campaign.PacingMode,
			PacingTolerance:       campaign.PacingTolerance,
			QPSLimit:              campaign.QPS,
			QPSCurrentWindowUnix:  qps.sec,
			QPSCurrentWindowCount: qpsCount,
			CreativeCount:         len(campaign.Creatives),
			ApprovedCreativeCount: compiled.approvedCreatives,
			DealCount:             len(campaign.DealIDs),
			PlacementCount:        len(campaign.AllowedPlacements),
		})
	}
	return out
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
		for i := range e.campaigns {
			campaign := &e.campaigns[i]
			if !campaign.cfg.Enabled {
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
	if !e.reserve(best.campaign.cfg, best.price, received) {
		return Decision{NoBid: true, Reason: "budget_or_qps_exhausted"}
	}

	bidID := stableID("bid", req.ID, best.imp.ID, best.campaign.cfg.ID, best.creative.ID)
	extClearLedger := clearLedgerBidExt(best.imp, e.cfg.BuyerID, best.campaign.cfg.ID, best.creative.ID)
	bid := openrtb.Bid{
		ID:      bidID,
		ImpID:   best.imp.ID,
		Price:   roundCPM(best.price),
		AdID:    best.creative.ID,
		CID:     best.campaign.cfg.ID,
		CrID:    best.creative.ID,
		Adomain: best.creative.Adomain,
		DealID:  best.dealID,
		AdM:     renderMarkup(best.imp, best.creative, e.eventURL(best.creative, "imp", req.ID, bidID, extClearLedger)),
		NURL:    e.eventURL(best.creative, "win", req.ID, bidID, extClearLedger),
		BURL:    e.eventURL(best.creative, "bill", req.ID, bidID, extClearLedger),
		LURL:    e.eventURL(best.creative, "loss", req.ID, bidID, extClearLedger),
		W:       best.creative.W,
		H:       best.creative.H,
		Ext:     map[string]any{"clearledger": extClearLedger},
	}
	return Decision{Response: &openrtb.BidResponse{
		ID:  req.ID,
		Cur: e.cfg.Currency,
		SeatBid: []openrtb.SeatBid{{
			Seat: best.campaign.cfg.Seat,
			Bid:  []openrtb.Bid{bid},
		}},
		Ext: map[string]any{"clearledger": map[string]any{"no_clearing_in_bidder": true}},
	}}
}

type candidate struct {
	ok       bool
	campaign *compiledCampaign
	creative config.Creative
	imp      openrtb.Impression
	dealID   string
	price    float64
}

func (e *Engine) evaluate(req *openrtb.BidRequest, imp openrtb.Impression, campaign *compiledCampaign) (candidate, bool) {
	if len(req.Cur) > 0 && !contains(req.Cur, e.cfg.Currency) {
		return candidate{}, false
	}
	mediaType := imp.MediaType()
	if !campaign.mediaTypes.has(mediaType) {
		return candidate{}, false
	}
	if !allowedSupply(req, imp, campaign) || !allowedPrivacy(req, campaign) {
		return candidate{}, false
	}
	dealID, dealFloor, ok := matchDeal(imp, campaign.dealIDs, e.cfg.Currency)
	if !ok {
		return candidate{}, false
	}
	floor := math.Max(imp.BidFloor, dealFloor)
	if campaign.cfg.BidCPM < floor {
		return candidate{}, false
	}
	if imp.BidFloorCur != "" && !strings.EqualFold(imp.BidFloorCur, e.cfg.Currency) {
		return candidate{}, false
	}
	creative, ok := chooseCreative(mediaType, req, campaign.creativesByMedia[mediaType])
	if !ok {
		return candidate{}, false
	}
	if !creativeMatchesRequest(imp, creative) {
		return candidate{}, false
	}
	if blockedAdvertiser(req, creative) {
		return candidate{}, false
	}
	return candidate{ok: true, campaign: campaign, creative: creative, imp: imp, dealID: dealID, price: campaign.cfg.BidCPM}, true
}

func (e *Engine) reserve(campaign config.Campaign, priceCPM float64, now time.Time) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	day := now.UTC().Format("2006-01-02")
	if e.dayKey != day {
		e.spend = map[string]float64{}
		e.qps = map[string]rateState{}
		e.dayKey = day
	}
	if campaign.QPS > 0 {
		nowSec := now.Unix()
		state := e.qps[campaign.ID]
		if state.sec != nowSec {
			state = rateState{sec: nowSec}
		}
		if state.count >= campaign.QPS {
			e.qps[campaign.ID] = state
			return false
		}
		state.count++
		e.qps[campaign.ID] = state
	}
	next := e.spend[campaign.ID] + priceCPM/1000
	if campaign.DailyBudget > 0 && next > campaign.DailyBudget {
		return false
	}
	if !pacingAllows(campaign, next, now) {
		return false
	}
	e.spend[campaign.ID] = next
	return true
}

func pacingAllows(campaign config.Campaign, nextSpend float64, now time.Time) bool {
	if !strings.EqualFold(strings.TrimSpace(campaign.PacingMode), "even") || campaign.DailyBudget <= 0 {
		return true
	}
	tolerance := campaign.PacingTolerance
	if tolerance <= 0 {
		tolerance = 1.25
	}
	utc := now.UTC()
	elapsed := time.Duration(utc.Hour())*time.Hour + time.Duration(utc.Minute())*time.Minute + time.Duration(utc.Second())*time.Second
	fraction := math.Max(float64(elapsed)/float64(24*time.Hour), 1.0/1440.0)
	allowed := campaign.DailyBudget * fraction * tolerance
	minOneBid := campaign.BidCPM / 1000
	if allowed < minOneBid {
		allowed = minOneBid
	}
	return nextSpend <= allowed
}

func allowedSupply(req *openrtb.BidRequest, imp openrtb.Impression, c *compiledCampaign) bool {
	if len(c.allowedPlacements) > 0 && !c.allowedPlacements.has(imp.TagID) {
		return false
	}
	if req.App != nil {
		if len(c.allowedApps) > 0 && !c.allowedApps.has(req.App.ID) {
			return false
		}
		if len(c.allowedBundles) > 0 && !c.allowedBundles.has(req.App.Bundle) {
			return false
		}
		return true
	}
	if req.Site != nil && len(c.allowedDomains) > 0 {
		return c.allowedDomains.has(req.Site.Domain)
	}
	return true
}

func allowedPrivacy(req *openrtb.BidRequest, c *compiledCampaign) bool {
	coppa, _ := req.Regs["coppa"].(float64)
	for _, cr := range c.cfg.Creatives {
		if cr.BlockedCOPPA && int(coppa) == 1 {
			return false
		}
	}
	if req.Device != nil && req.Device.LMT == 1 {
		for _, cr := range c.cfg.Creatives {
			if cr.RequiresIFA {
				return false
			}
		}
	}
	if len(c.geoCountries) > 0 {
		country := ""
		if req.Device != nil && req.Device.Geo != nil {
			if raw, ok := req.Device.Geo["country"].(string); ok {
				country = raw
			}
		}
		return c.geoCountries.has(country)
	}
	return true
}

func matchDeal(imp openrtb.Impression, allowed stringSet, currency string) (string, float64, bool) {
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
		if allowed.has(deal.ID) {
			if deal.BidFloorCur != "" && !strings.EqualFold(deal.BidFloorCur, currency) {
				return "", 0, false
			}
			return deal.ID, deal.BidFloor, true
		}
	}
	return "", 0, false
}

func chooseCreative(mediaType string, req *openrtb.BidRequest, creatives []config.Creative) (config.Creative, bool) {
	for _, creative := range creatives {
		if !creative.Approved {
			continue
		}
		if creative.RequiresIFA && (req.Device == nil || req.Device.IFA == "") {
			continue
		}
		return creative, true
	}
	return config.Creative{}, false
}

func creativeMatchesRequest(imp openrtb.Impression, creative config.Creative) bool {
	switch imp.MediaType() {
	case "video":
		return mimeAllowed(imp.Video.Mimes, assetMime(creative, "video")) &&
			durationAllowed(imp.Video.MinDuration, imp.Video.MaxDuration, creative.Duration) &&
			dimensionsAllowed(imp.Video.W, imp.Video.H, creative.W, creative.H)
	case "audio":
		return mimeAllowed(imp.Audio.Mimes, assetMime(creative, "audio")) &&
			durationAllowed(imp.Audio.MinDuration, imp.Audio.MaxDuration, creative.Duration)
	case "display":
		if imp.Banner == nil {
			return true
		}
		return dimensionsAllowed(imp.Banner.W, imp.Banner.H, creative.W, creative.H)
	default:
		return true
	}
}

func durationAllowed(minDuration, maxDuration, creativeDuration int) bool {
	if creativeDuration <= 0 {
		return true
	}
	if minDuration > 0 && creativeDuration < minDuration {
		return false
	}
	if maxDuration > 0 && creativeDuration > maxDuration {
		return false
	}
	return true
}

func dimensionsAllowed(requestW, requestH, creativeW, creativeH int) bool {
	if requestW > 0 && creativeW > 0 && requestW != creativeW {
		return false
	}
	if requestH > 0 && creativeH > 0 && requestH != creativeH {
		return false
	}
	return true
}

func mimeAllowed(allowed []string, mime string) bool {
	if len(allowed) == 0 || strings.TrimSpace(mime) == "" {
		return true
	}
	for _, value := range allowed {
		if strings.EqualFold(strings.TrimSpace(value), mime) {
			return true
		}
	}
	return false
}

func blockedAdvertiser(req *openrtb.BidRequest, creative config.Creative) bool {
	for _, domain := range creative.Adomain {
		if contains(req.BAdv, domain) {
			return true
		}
	}
	return false
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
		mime := assetMime(cr, imp.MediaType())
		return fmt.Sprintf(`<VAST version="4.3"><Ad id="%s"><InLine><AdSystem>ClearLedger Bidder OpenRTB</AdSystem><AdTitle>%s</AdTitle><Impression><![CDATA[%s]]></Impression><Creatives><Creative id="%s"><Linear><Duration>%s</Duration><MediaFiles><MediaFile delivery="progressive" type="%s" width="%d" height="%d"><![CDATA[%s]]></MediaFile></MediaFiles><VideoClicks><ClickThrough><![CDATA[%s]]></ClickThrough></VideoClicks></Linear></Creative></Creatives></InLine></Ad></VAST>`, cr.ID, cr.ID, impURL, cr.ID, vastDuration(duration), mime, max(cr.W, 640), max(cr.H, 360), cr.AssetURL, cr.LandingURL)
	case "native":
		body, _ := json.Marshal(map[string]any{"native": map[string]any{"link": map[string]any{"url": cr.LandingURL}, "assets": []map[string]any{{"id": 1, "title": map[string]any{"text": cr.ID}}}, "imptrackers": []string{impURL}}})
		return string(body)
	default:
		w, h := max(cr.W, 300), max(cr.H, 250)
		return fmt.Sprintf(`<a href="%s" target="_blank" rel="noopener"><img src="%s" width="%d" height="%d" alt=""></a><img src="%s" width="1" height="1" alt="">`, htmlEscape(cr.LandingURL), htmlEscape(cr.AssetURL), w, h, htmlEscape(impURL))
	}
}

func assetMime(cr config.Creative, mediaType string) string {
	lower := strings.ToLower(strings.TrimSpace(cr.AssetURL))
	switch {
	case strings.HasSuffix(lower, ".mp3"):
		return "audio/mpeg"
	case strings.HasSuffix(lower, ".m4a"):
		return "audio/mp4"
	case strings.HasSuffix(lower, ".ogg"):
		return "audio/ogg"
	case strings.HasSuffix(lower, ".webm"):
		if mediaType == "audio" {
			return "audio/webm"
		}
		return "video/webm"
	case strings.HasSuffix(lower, ".mov"):
		return "video/quicktime"
	default:
		if mediaType == "audio" {
			return "audio/mpeg"
		}
		return "video/mp4"
	}
}

func vastDuration(seconds int) string {
	if seconds <= 0 {
		seconds = 30
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	remainingSeconds := seconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, remainingSeconds)
}

func clearLedgerBidExt(imp openrtb.Impression, buyerID, campaignID, creativeID string) map[string]any {
	ext := map[string]any{
		"buyer_id":    buyerID,
		"campaign_id": campaignID,
		"creative_id": creativeID,
		"bidder":      "clearledger-bidder-openrtb",
	}
	source, ok := imp.Ext["clearledger"].(map[string]any)
	if !ok {
		return ext
	}
	for _, key := range []string{
		"lane_id",
		"private_market_id",
		"package_id",
		"placement_id",
		"proof_run_id",
		"receipt_required",
	} {
		if value, ok := source[key]; ok {
			ext[key] = value
		}
	}
	return ext
}

func (e *Engine) eventURL(cr config.Creative, event, auctionID, bidID string, clearLedgerExt map[string]any) string {
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
	for _, key := range []string{"buyer_id", "campaign_id", "lane_id", "package_id", "placement_id", "proof_run_id"} {
		if value, ok := clearLedgerExt[key].(string); ok && strings.TrimSpace(value) != "" {
			q.Set(key, value)
		}
	}
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
		value = strings.ToLower(strings.TrimSpace(value))
		if value == needle || (value == "banner" && needle == "display") {
			return true
		}
	}
	return false
}

func compileCampaigns(campaigns []config.Campaign) []compiledCampaign {
	out := make([]compiledCampaign, 0, len(campaigns))
	for _, campaign := range campaigns {
		compiled := compiledCampaign{
			cfg:               campaign,
			mediaTypes:        newStringSet(campaign.MediaTypes),
			allowedApps:       newStringSet(campaign.AllowedApps),
			allowedBundles:    newStringSet(campaign.AllowedBundles),
			allowedDomains:    newStringSet(campaign.AllowedDomains),
			allowedPlacements: newStringSet(campaign.AllowedPlacements),
			dealIDs:           newStringSet(campaign.DealIDs),
			geoCountries:      newStringSet(campaign.GeoCountries),
			creativesByMedia:  map[string][]config.Creative{},
		}
		for _, creative := range campaign.Creatives {
			if !creative.Approved {
				continue
			}
			compiled.approvedCreatives++
			mediaType := normalizeToken(creative.MediaType)
			compiled.creativesByMedia[mediaType] = append(compiled.creativesByMedia[mediaType], creative)
		}
		out = append(out, compiled)
	}
	return out
}

func newStringSet(values []string) stringSet {
	if len(values) == 0 {
		return nil
	}
	out := make(stringSet, len(values))
	for _, value := range values {
		normalized := normalizeToken(value)
		if normalized != "" {
			out[normalized] = struct{}{}
		}
	}
	return out
}

func (s stringSet) has(value string) bool {
	if len(s) == 0 {
		return false
	}
	_, ok := s[normalizeToken(value)]
	return ok
}

func normalizeToken(value string) string {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", "_"))
	if value == "banner" {
		return "display"
	}
	return value
}

func roundCPM(v float64) float64 { return math.Round(v*10000) / 10000 }
func roundMoney(v float64) float64 {
	return math.Round(v*1000000) / 1000000
}
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
