package openrtb

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var ErrMalformed = errors.New("malformed_openrtb")

func DecodeRequest(body []byte) (*BidRequest, error) {
	var req BidRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&req); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing JSON data", ErrMalformed)
	}
	req.Raw = append(req.Raw[:0], body...)
	if err := ValidateRequest(&req); err != nil {
		return nil, err
	}
	return &req, nil
}

func ValidateRequest(req *BidRequest) error {
	if req == nil {
		return fmt.Errorf("%w: empty request", ErrMalformed)
	}
	if req.ID == "" {
		return fmt.Errorf("%w: id is required", ErrMalformed)
	}
	if len(req.Imp) == 0 {
		return fmt.Errorf("%w: at least one imp is required", ErrMalformed)
	}
	if req.TMax < 0 {
		return fmt.Errorf("%w: tmax must be non-negative", ErrMalformed)
	}
	if (req.App == nil && req.Site == nil) || (req.App != nil && req.Site != nil) {
		return fmt.Errorf("%w: exactly one of app or site is required", ErrMalformed)
	}
	seen := map[string]struct{}{}
	for idx, imp := range req.Imp {
		if imp.ID == "" {
			return fmt.Errorf("%w: imp[%d].id is required", ErrMalformed, idx)
		}
		if _, ok := seen[imp.ID]; ok {
			return fmt.Errorf("%w: duplicate imp.id %q", ErrMalformed, imp.ID)
		}
		seen[imp.ID] = struct{}{}
		mediaObjects := 0
		if imp.Banner != nil {
			mediaObjects++
		}
		if imp.Video != nil {
			mediaObjects++
		}
		if imp.Audio != nil {
			mediaObjects++
		}
		if imp.Native != nil {
			mediaObjects++
		}
		if mediaObjects != 1 {
			return fmt.Errorf("%w: imp[%d] must include exactly one media object", ErrMalformed, idx)
		}
		if err := validateMediaObject(idx, imp); err != nil {
			return err
		}
		if imp.BidFloor < 0 {
			return fmt.Errorf("%w: imp[%d].bidfloor must be non-negative", ErrMalformed, idx)
		}
		if imp.PMP != nil {
			for dealIdx, deal := range imp.PMP.Deals {
				if deal.ID == "" {
					return fmt.Errorf("%w: imp[%d].pmp.deals[%d].id is required", ErrMalformed, idx, dealIdx)
				}
				if deal.BidFloor < 0 {
					return fmt.Errorf("%w: imp[%d].pmp.deals[%d].bidfloor must be non-negative", ErrMalformed, idx, dealIdx)
				}
			}
		}
	}
	return nil
}

func validateMediaObject(idx int, imp Impression) error {
	if imp.Banner != nil {
		if imp.Banner.W < 0 || imp.Banner.H < 0 {
			return fmt.Errorf("%w: imp[%d].banner dimensions must be non-negative", ErrMalformed, idx)
		}
	}
	if imp.Video != nil {
		if imp.Video.MinDuration < 0 || imp.Video.MaxDuration < 0 {
			return fmt.Errorf("%w: imp[%d].video duration bounds must be non-negative", ErrMalformed, idx)
		}
		if imp.Video.MinDuration > 0 && imp.Video.MaxDuration > 0 && imp.Video.MinDuration > imp.Video.MaxDuration {
			return fmt.Errorf("%w: imp[%d].video minduration cannot exceed maxduration", ErrMalformed, idx)
		}
		if imp.Video.W < 0 || imp.Video.H < 0 {
			return fmt.Errorf("%w: imp[%d].video dimensions must be non-negative", ErrMalformed, idx)
		}
	}
	if imp.Audio != nil {
		if imp.Audio.MinDuration < 0 || imp.Audio.MaxDuration < 0 {
			return fmt.Errorf("%w: imp[%d].audio duration bounds must be non-negative", ErrMalformed, idx)
		}
		if imp.Audio.MinDuration > 0 && imp.Audio.MaxDuration > 0 && imp.Audio.MinDuration > imp.Audio.MaxDuration {
			return fmt.Errorf("%w: imp[%d].audio minduration cannot exceed maxduration", ErrMalformed, idx)
		}
	}
	if imp.Native != nil {
		info, err := parseNativeRequest(imp.Native.Request)
		if err != nil {
			return fmt.Errorf("%w: imp[%d].native.request %v", ErrMalformed, idx, err)
		}
		if len(info.assets) == 0 {
			return fmt.Errorf("%w: imp[%d].native.request must include assets", ErrMalformed, idx)
		}
	}
	return nil
}
