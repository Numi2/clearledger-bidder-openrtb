package openrtb

import (
	"encoding/json"
	"fmt"
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
			if bid.NURL == "" && bid.BURL == "" && bid.LURL == "" && !hasClearLedgerExt(bid.Ext) {
				return fmt.Errorf("bid requires notice URLs or ext.clearledger proof fields")
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
		if !LooksLikeVAST(adm) {
			return fmt.Errorf("adm must be VAST for %s impressions", imp.MediaType())
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
	trimmed := strings.TrimSpace(adm)
	upper := strings.ToUpper(trimmed)
	return strings.HasPrefix(upper, "<VAST") &&
		strings.Contains(upper, "<IMPRESSION") &&
		strings.Contains(upper, "<MEDIAFILE") &&
		strings.Contains(upper, "<DURATION>")
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

func hasClearLedgerExt(ext map[string]any) bool {
	if ext == nil {
		return false
	}
	_, ok := ext["clearledger"]
	return ok
}

func containsFold(values []string, needle string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(needle)) {
			return true
		}
	}
	return false
}
