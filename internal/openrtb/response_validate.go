package openrtb

import (
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

func LooksLikeVAST(adm string) bool {
	trimmed := strings.TrimSpace(adm)
	return strings.HasPrefix(trimmed, "<VAST") && strings.Contains(trimmed, "<Impression") && strings.Contains(trimmed, "<MediaFile")
}
