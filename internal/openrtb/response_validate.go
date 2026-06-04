package openrtb

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func ValidateBidResponse(req *BidRequest, resp *BidResponse) error {
	if req == nil || resp == nil {
		return fmt.Errorf("request and response are required")
	}
	if resp.ID != req.ID {
		return fmt.Errorf("response id must match request id")
	}
	if resp.Cur != "" && len(req.Cur) > 0 && !containsFold(req.Cur, resp.Cur) {
		return fmt.Errorf("response currency not allowed by request")
	}
	impIDs := map[string]Impression{}
	dealIDs := map[string]struct{}{}
	floors := map[string]float64{}
	for _, imp := range req.Imp {
		impIDs[imp.ID] = imp
		floors[imp.ID] = imp.BidFloor
		if imp.PMP != nil {
			for _, deal := range imp.PMP.Deals {
				dealIDs[deal.ID] = struct{}{}
				if deal.BidFloor > floors[imp.ID] {
					floors[imp.ID] = deal.BidFloor
				}
			}
		}
	}
	if len(resp.SeatBid) == 0 {
		return fmt.Errorf("seatbid is required for bid responses")
	}
	for _, seatBid := range resp.SeatBid {
		if seatBid.Seat == "" {
			return fmt.Errorf("seat is required")
		}
		for _, bid := range seatBid.Bid {
			if bid.ID == "" || bid.ImpID == "" || bid.CrID == "" {
				return fmt.Errorf("bid id, impid, and crid are required")
			}
			if _, ok := impIDs[bid.ImpID]; !ok {
				return fmt.Errorf("bid impid %q does not match request", bid.ImpID)
			}
			if bid.Price < floors[bid.ImpID] {
				return fmt.Errorf("bid price below floor")
			}
			if len(bid.Adomain) == 0 {
				return fmt.Errorf("adomain is required")
			}
			if bid.AdM == "" {
				return fmt.Errorf("adm is required")
			}
			if err := ValidateAdMarkup(impIDs[bid.ImpID], bid.AdM); err != nil {
				return err
			}
			if err := validateBidMediaConstraints(impIDs[bid.ImpID], bid); err != nil {
				return err
			}
			if bid.NURL == "" && bid.BURL == "" && bid.LURL == "" && !hasClearLedgerExt(bid.Ext) {
				return fmt.Errorf("bid requires notice URLs or ext.clearledger proof fields")
			}
			if err := validateClearLedgerProofExt(impIDs[bid.ImpID], bid); err != nil {
				return err
			}
			if len(dealIDs) > 0 {
				if bid.DealID == "" {
					return fmt.Errorf("dealid is required for PMP requests")
				}
				if _, ok := dealIDs[bid.DealID]; !ok {
					return fmt.Errorf("bid dealid does not match request")
				}
			}
		}
	}
	return nil
}

func ValidateAdMarkup(imp Impression, adm string) error {
	switch imp.MediaType() {
	case "video", "audio":
		if err := validateVASTMarkup(imp, adm); err != nil {
			return err
		}
	case "native":
		if !LooksLikeNativeAdM(adm) {
			return fmt.Errorf("adm must be OpenRTB native response JSON for native impressions")
		}
	case "display":
		if !LooksLikeDisplayAdM(adm) {
			return fmt.Errorf("adm must include display markup for banner impressions")
		}
	default:
		return fmt.Errorf("unknown impression media type")
	}
	return nil
}

func LooksLikeVAST(adm string) bool {
	return parseVAST(adm).shapeOK()
}

func LooksLikeDisplayAdM(adm string) bool {
	lower := strings.ToLower(strings.TrimSpace(adm))
	return strings.Contains(lower, "<img") || strings.Contains(lower, "<script") || strings.Contains(lower, "<iframe")
}

func LooksLikeNativeAdM(adm string) bool {
	var body struct {
		Native struct {
			Assets []struct {
				ID int `json:"id"`
			} `json:"assets"`
			Link struct {
				URL string `json:"url"`
			} `json:"link"`
			ImpTrackers []string `json:"imptrackers"`
		} `json:"native"`
	}
	if err := json.Unmarshal([]byte(adm), &body); err != nil {
		return false
	}
	return len(body.Native.Assets) > 0 && strings.TrimSpace(body.Native.Link.URL) != ""
}

func validateBidMediaConstraints(imp Impression, bid Bid) error {
	switch imp.MediaType() {
	case "display":
		if imp.Banner == nil {
			return nil
		}
		if imp.Banner.W > 0 && bid.W > 0 && bid.W != imp.Banner.W {
			return fmt.Errorf("bid width does not match banner request")
		}
		if imp.Banner.H > 0 && bid.H > 0 && bid.H != imp.Banner.H {
			return fmt.Errorf("bid height does not match banner request")
		}
	case "video":
		if imp.Video == nil {
			return nil
		}
		info := parseVAST(bid.AdM)
		if imp.Video.W > 0 && info.mediaWidth > 0 && info.mediaWidth != imp.Video.W {
			return fmt.Errorf("VAST media width does not match video request")
		}
		if imp.Video.H > 0 && info.mediaHeight > 0 && info.mediaHeight != imp.Video.H {
			return fmt.Errorf("VAST media height does not match video request")
		}
	}
	return nil
}

type vastInfo struct {
	hasRoot       bool
	hasImpression bool
	hasMediaFile  bool
	hasDuration   bool
	duration      float64
	mediaTypes    []string
	mediaWidth    int
	mediaHeight   int
	parseErr      error
}

func (v vastInfo) shapeOK() bool {
	return v.parseErr == nil && v.hasRoot && v.hasImpression && v.hasMediaFile && v.hasDuration
}

func validateVASTMarkup(imp Impression, adm string) error {
	info := parseVAST(adm)
	if !info.shapeOK() {
		return fmt.Errorf("adm must be VAST for %s impressions", imp.MediaType())
	}
	var mimes []string
	minDuration := 0
	maxDuration := 0
	switch imp.MediaType() {
	case "video":
		if imp.Video != nil {
			mimes = imp.Video.Mimes
			minDuration = imp.Video.MinDuration
			maxDuration = imp.Video.MaxDuration
		}
	case "audio":
		if imp.Audio != nil {
			mimes = imp.Audio.Mimes
			minDuration = imp.Audio.MinDuration
			maxDuration = imp.Audio.MaxDuration
		}
	}
	if minDuration > 0 && info.duration > 0 && info.duration < float64(minDuration) {
		return fmt.Errorf("VAST duration below request minimum")
	}
	if maxDuration > 0 && info.duration > 0 && info.duration > float64(maxDuration) {
		return fmt.Errorf("VAST duration above request maximum")
	}
	if len(mimes) > 0 {
		for _, mediaType := range info.mediaTypes {
			if strings.TrimSpace(mediaType) == "" {
				return fmt.Errorf("VAST MediaFile type is required when request mimes are present")
			}
			if containsFold(mimes, mediaType) {
				return nil
			}
		}
		return fmt.Errorf("VAST MediaFile type not allowed by request mimes")
	}
	return nil
}

func parseVAST(adm string) vastInfo {
	decoder := xml.NewDecoder(strings.NewReader(adm))
	info := vastInfo{}
	var inImpression, inDuration, inMediaFile bool
	var impressionText, durationText, mediaText strings.Builder
	currentMediaType := ""
	currentMediaWidth := 0
	currentMediaHeight := 0
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				return info
			}
			info.parseErr = err
			return info
		}
		switch t := token.(type) {
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			switch name {
			case "vast":
				info.hasRoot = true
			case "impression":
				inImpression = true
				impressionText.Reset()
			case "duration":
				inDuration = true
				durationText.Reset()
			case "mediafile":
				inMediaFile = true
				mediaText.Reset()
				currentMediaType = ""
				currentMediaWidth = 0
				currentMediaHeight = 0
				for _, attr := range t.Attr {
					switch strings.ToLower(attr.Name.Local) {
					case "type":
						currentMediaType = strings.TrimSpace(attr.Value)
					case "width":
						currentMediaWidth, _ = strconv.Atoi(strings.TrimSpace(attr.Value))
					case "height":
						currentMediaHeight, _ = strconv.Atoi(strings.TrimSpace(attr.Value))
					}
				}
			}
		case xml.CharData:
			if inImpression {
				impressionText.Write([]byte(t))
			}
			if inDuration {
				durationText.Write([]byte(t))
			}
			if inMediaFile {
				mediaText.Write([]byte(t))
			}
		case xml.EndElement:
			name := strings.ToLower(t.Name.Local)
			switch name {
			case "impression":
				if strings.TrimSpace(impressionText.String()) != "" {
					info.hasImpression = true
				}
				inImpression = false
			case "duration":
				if seconds, ok := parseVASTDuration(durationText.String()); ok {
					info.hasDuration = true
					info.duration = seconds
				}
				inDuration = false
			case "mediafile":
				if strings.TrimSpace(mediaText.String()) != "" {
					info.hasMediaFile = true
					info.mediaTypes = append(info.mediaTypes, currentMediaType)
					if info.mediaWidth == 0 {
						info.mediaWidth = currentMediaWidth
					}
					if info.mediaHeight == 0 {
						info.mediaHeight = currentMediaHeight
					}
				}
				inMediaFile = false
			}
		}
	}
}

func parseVASTDuration(value string) (float64, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 {
		return 0, false
	}
	hours, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, false
	}
	minutes, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, false
	}
	seconds, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return 0, false
	}
	return float64(hours*3600+minutes*60) + seconds, true
}

func hasClearLedgerExt(ext map[string]any) bool {
	if ext == nil {
		return false
	}
	_, ok := ext["clearledger"]
	return ok
}

func validateClearLedgerProofExt(imp Impression, bid Bid) error {
	reqExt, ok := clearLedgerExt(imp.Ext)
	if !ok {
		return nil
	}
	receiptRequired, _ := reqExt["receipt_required"].(bool)
	if !receiptRequired {
		return nil
	}
	bidExt, ok := clearLedgerExt(bid.Ext)
	if !ok {
		return fmt.Errorf("bid.ext.clearledger is required when receipt_required is true")
	}
	for _, key := range []string{"buyer_id", "campaign_id", "creative_id"} {
		if strings.TrimSpace(stringValue(bidExt[key])) == "" {
			return fmt.Errorf("bid.ext.clearledger.%s is required when receipt_required is true", key)
		}
	}
	for _, key := range []string{"lane_id", "private_market_id", "package_id", "placement_id", "proof_run_id"} {
		reqValue := strings.TrimSpace(stringValue(reqExt[key]))
		if reqValue == "" {
			continue
		}
		if strings.TrimSpace(stringValue(bidExt[key])) != reqValue {
			return fmt.Errorf("bid.ext.clearledger.%s must match request proof field", key)
		}
	}
	if value, ok := bidExt["receipt_required"].(bool); !ok || !value {
		return fmt.Errorf("bid.ext.clearledger.receipt_required must be true")
	}
	return nil
}

func clearLedgerExt(ext map[string]any) (map[string]any, bool) {
	if ext == nil {
		return nil, false
	}
	value, ok := ext["clearledger"].(map[string]any)
	return value, ok
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		return ""
	}
}

func containsFold(values []string, needle string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(needle)) {
			return true
		}
	}
	return false
}
