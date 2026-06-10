package openrtb

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

var defaultAcceptedVersions = []string{"2.6", "2.5"}

type DecodeOptions struct {
	VersionHeader           string
	AcceptedRequestVersions []string
	OutboundVersion         string
	CompatProfile           string
	PreservePartnerExt      bool
}

func DefaultDecodeOptions() DecodeOptions {
	return DecodeOptions{
		AcceptedRequestVersions: append([]string(nil), defaultAcceptedVersions...),
		OutboundVersion:         "2.6",
		CompatProfile:           "openrtb_json",
		PreservePartnerExt:      true,
	}
}

func NormalizeVersions(values []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		version := ClassifyVersion(value)
		if version == "" {
			continue
		}
		if _, ok := seen[version]; ok {
			continue
		}
		seen[version] = struct{}{}
		out = append(out, version)
	}
	return out
}

func NormalizeOutboundVersion(value string) string {
	token := cleanVersionToken(value)
	switch {
	case token == "":
		return ""
	case strings.HasPrefix(token, "2.6") || token == "26":
		return "2.6"
	case strings.HasPrefix(token, "2.5") || token == "25":
		return "2.5"
	case strings.HasPrefix(token, "2.4") || token == "24":
		return "2.4"
	case strings.HasPrefix(token, "2.3") || token == "23":
		return "2.3"
	case strings.HasPrefix(token, "2.2") || token == "22":
		return "2.2"
	case strings.HasPrefix(token, "2.1") || token == "21":
		return "2.1"
	case strings.HasPrefix(token, "2.0") || token == "20":
		return "2.0"
	case token == "legacy_2.x" || token == "legacy2x":
		return "2.5"
	default:
		return ""
	}
}

func ClassifyVersion(value string) string {
	token := cleanVersionToken(value)
	switch {
	case token == "":
		return ""
	case strings.HasPrefix(token, "2.6") || token == "26":
		return "2.6"
	case strings.HasPrefix(token, "2.5") || token == "25":
		return "2.5"
	case strings.HasPrefix(token, "2.4") || token == "24":
		return "2.4"
	case strings.HasPrefix(token, "2."):
		return "legacy_2.x"
	case token == "legacy_2.x" || token == "legacy2x":
		return "legacy_2.x"
	default:
		return ""
	}
}

func ShapeResponse(req *BidRequest, resp *BidResponse, options DecodeOptions) {
	if resp == nil {
		return
	}
	version := responseVersion(req, options)
	if resp.Ext == nil {
		resp.Ext = map[string]any{}
	}
	compat := map[string]any{
		"openrtb_outbound_version": version,
		"compat_profile":           firstNonEmpty(options.CompatProfile, "openrtb_json"),
	}
	if req != nil && req.Compat != nil {
		compat["openrtb_detected_version"] = req.Compat.DetectedVersion
		if len(req.Compat.NormalizedFields) > 0 {
			compat["normalized_fields"] = append([]string(nil), req.Compat.NormalizedFields...)
		}
		if len(req.Compat.PreservedExtKeys) > 0 {
			compat["preserved_ext_keys"] = append([]string(nil), req.Compat.PreservedExtKeys...)
		}
	}
	resp.Ext["openrtb_compat"] = compat
}

func responseVersion(req *BidRequest, options DecodeOptions) string {
	if version := NormalizeOutboundVersion(options.OutboundVersion); version != "" {
		return version
	}
	if req != nil && req.Compat != nil {
		switch req.Compat.DetectedVersion {
		case "2.6", "2.5", "2.4":
			return req.Compat.DetectedVersion
		case "legacy_2.x":
			return "2.5"
		}
	}
	return "2.6"
}

func applyCompatibility(req *BidRequest, body map[string]any, options DecodeOptions) error {
	if req == nil {
		return fmt.Errorf("%w: empty request", ErrMalformed)
	}
	options = normalizeDecodeOptions(options)
	detected := detectVersion(body, req, options.VersionHeader)
	if detected == "" {
		detected = "2.6"
	}
	if !versionAccepted(detected, options.AcceptedRequestVersions) {
		return fmt.Errorf("%w: OpenRTB version %s is not accepted by this bidder", ErrMalformed, detected)
	}
	req.Compat = &CompatProof{
		DetectedVersion:         detected,
		OutboundVersion:         NormalizeOutboundVersion(options.OutboundVersion),
		CompatProfile:           firstNonEmpty(options.CompatProfile, "openrtb_json"),
		AcceptedRequestVersions: append([]string(nil), options.AcceptedRequestVersions...),
	}
	if options.PreservePartnerExt {
		req.Compat.PreservedExtKeys = preservedExtKeys(req)
	}
	normalizeCommonAliases(req)
	return nil
}

func normalizeDecodeOptions(options DecodeOptions) DecodeOptions {
	if len(options.AcceptedRequestVersions) == 0 {
		options.AcceptedRequestVersions = append([]string(nil), defaultAcceptedVersions...)
	} else {
		options.AcceptedRequestVersions = NormalizeVersions(options.AcceptedRequestVersions)
	}
	if len(options.AcceptedRequestVersions) == 0 {
		options.AcceptedRequestVersions = append([]string(nil), defaultAcceptedVersions...)
	}
	options.OutboundVersion = NormalizeOutboundVersion(options.OutboundVersion)
	if options.OutboundVersion == "" {
		options.OutboundVersion = "2.6"
	}
	options.CompatProfile = normalizeToken(firstNonEmpty(options.CompatProfile, "openrtb_json"))
	return options
}

func detectVersion(body map[string]any, req *BidRequest, headerVersion string) string {
	for _, value := range []string{
		headerVersion,
		stringFromAny(body["openrtb_version"]),
		stringFromAny(body["version"]),
		stringFromAny(body["ver"]),
		stringFromAny(nestedAny(body, "ext", "openrtb_version")),
		stringFromAny(nestedAny(body, "ext", "ortb_version")),
		stringFromAny(nestedAny(body, "ext", "prebid", "version")),
		stringFromAny(req.Ext["openrtb_version"]),
		stringFromAny(req.Ext["ortb_version"]),
	} {
		if version := ClassifyVersion(value); version != "" {
			return version
		}
	}
	return ""
}

func versionAccepted(version string, accepted []string) bool {
	version = ClassifyVersion(version)
	if version == "" {
		return true
	}
	for _, candidate := range accepted {
		if candidate == version {
			return true
		}
	}
	return false
}

func normalizeCommonAliases(req *BidRequest) {
	addNormalized := func(field string) {
		if req.Compat == nil {
			return
		}
		req.Compat.NormalizedFields = appendUnique(req.Compat.NormalizedFields, field)
	}
	if req.User != nil && req.User.Ext != nil {
		if req.User.Consent == "" {
			if consent := firstNonEmpty(stringFromAny(req.User.Ext["consent"]), stringFromAny(req.User.Ext["tcf_consent"])); consent != "" {
				req.User.Consent = consent
				addNormalized("user.consent")
			}
		}
		if len(req.User.EIDs) == 0 {
			if eids := mapSliceFromAny(req.User.Ext["eids"]); len(eids) > 0 {
				req.User.EIDs = eids
				addNormalized("user.eids")
			}
		}
	}
	if req.Regs != nil {
		if regsExt := mapFromAny(req.Regs["ext"]); regsExt != nil {
			for _, key := range []string{"gpp", "gpp_sid", "us_privacy", "gdpr", "coppa"} {
				if _, ok := req.Regs[key]; !ok {
					if value, found := regsExt[key]; found {
						req.Regs[key] = value
						addNormalized("regs." + key)
					}
				}
			}
		}
	}
	if req.Source != nil {
		if _, ok := req.Source["schain"]; !ok {
			if sourceExt := mapFromAny(req.Source["ext"]); sourceExt != nil {
				if schain := mapFromAny(sourceExt["schain"]); schain != nil {
					req.Source["schain"] = schain
					addNormalized("source.schain")
				}
			}
		}
	}
	for index := range req.Imp {
		imp := &req.Imp[index]
		if imp.BidFloor == 0 {
			if floor := floatFromAny(firstPresent(imp.Ext, "bidfloor", "floor", "floor_cpm")); floor > 0 {
				imp.BidFloor = floor
				addNormalized("imp.bidfloor")
			}
		}
		if imp.BidFloorCur == "" {
			if cur := stringFromAny(firstPresent(imp.Ext, "bidfloorcur", "floorcur")); cur != "" {
				imp.BidFloorCur = cur
				addNormalized("imp.bidfloorcur")
			}
		}
		if imp.Native != nil {
			normalizeNativeVersion(imp.Native, addNormalized)
		}
		if imp.Video != nil {
			normalizePlacementAliases(&imp.Video.Placement, &imp.Video.Plcmt, "imp.video", addNormalized)
		}
		if imp.Audio != nil {
			normalizePlacementAliases(&imp.Audio.Placement, &imp.Audio.Plcmt, "imp.audio", addNormalized)
		}
	}
	if req.Compat != nil {
		sort.Strings(req.Compat.NormalizedFields)
	}
}

func normalizeNativeVersion(native *Native, addNormalized func(string)) {
	if native == nil || native.Request == "" {
		return
	}
	var wrapper map[string]any
	if json.Unmarshal([]byte(native.Request), &wrapper) != nil {
		return
	}
	target := wrapper
	if nested := mapFromAny(wrapper["native"]); nested != nil {
		target = nested
	}
	if _, ok := target["ver"]; !ok {
		if version := stringFromAny(target["version"]); version != "" {
			target["ver"] = version
			next, err := json.Marshal(wrapper)
			if err == nil {
				native.Request = string(next)
				addNormalized("imp.native.request.ver")
			}
		}
	}
}

func normalizePlacementAliases(placement *int, plcmt *int, prefix string, addNormalized func(string)) {
	if placement == nil || plcmt == nil {
		return
	}
	if *plcmt == 0 && *placement > 0 {
		*plcmt = *placement
		addNormalized(prefix + ".plcmt")
	}
	if *placement == 0 && *plcmt > 0 {
		*placement = *plcmt
		addNormalized(prefix + ".placement")
	}
}

func preservedExtKeys(req *BidRequest) []string {
	keys := []string{}
	collect := func(prefix string, values map[string]any) {
		for key := range values {
			keys = appendUnique(keys, prefix+key)
		}
	}
	collect("request.ext.", req.Ext)
	if req.User != nil {
		collect("user.ext.", req.User.Ext)
	}
	if req.Device != nil {
		collect("device.ext.", req.Device.Ext)
	}
	if req.Site != nil {
		collect("site.ext.", req.Site.Ext)
	}
	if req.App != nil {
		collect("app.ext.", req.App.Ext)
	}
	for _, imp := range req.Imp {
		collect("imp.ext.", imp.Ext)
	}
	sort.Strings(keys)
	return keys
}

func cleanVersionToken(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.TrimPrefix(normalized, "openrtb")
	normalized = strings.TrimPrefix(normalized, "ortb")
	return strings.TrimSpace(strings.Trim(normalized, "v=_-"))
}

func normalizeToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	return value
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	default:
		return ""
	}
}

func floatFromAny(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case json.Number:
		parsed, _ := typed.Float64()
		return parsed
	case string:
		parsed, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed
	default:
		return 0
	}
}

func nestedAny(values map[string]any, path ...string) any {
	var current any = values
	for _, key := range path {
		mapped, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = mapped[key]
	}
	return current
}

func firstPresent(values map[string]any, keys ...string) any {
	if values == nil {
		return nil
	}
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func mapFromAny(value any) map[string]any {
	if mapped, ok := value.(map[string]any); ok {
		return mapped
	}
	return nil
}

func mapSliceFromAny(value any) []map[string]any {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if mapped := mapFromAny(item); mapped != nil {
			out = append(out, mapped)
		}
	}
	return out
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
