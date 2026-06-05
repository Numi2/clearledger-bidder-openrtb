package certify

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
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Numi2/clearledger-bidder-openrtb/internal/openrtb"
)

type Options struct {
	Endpoint      string
	Token         string
	SigningSecret string
	BuyerID       string
	SeatID        string
	SamplePath    string
	SamplePaths   []string
	Timeout       time.Duration
}

type Report struct {
	OK          bool          `json:"ok"`
	Endpoint    string        `json:"endpoint"`
	GeneratedAt string        `json:"generated_at"`
	Contract    string        `json:"contract"`
	BuyerID     string        `json:"buyer_id,omitempty"`
	SeatID      string        `json:"seat_id,omitempty"`
	TimeoutMS   int64         `json:"timeout_ms"`
	Media       []MediaResult `json:"media"`
	Checks      []Check       `json:"checks"`
	Summary     Summary       `json:"summary"`
}

type Check struct {
	Name       string `json:"name"`
	OK         bool   `json:"ok"`
	Detail     string `json:"detail,omitempty"`
	LatencyMS  int64  `json:"latency_ms,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
}

type MediaResult struct {
	MediaType    string `json:"media_type"`
	Sample       string `json:"sample"`
	HTTPStatus   int    `json:"http_status,omitempty"`
	LatencyMS    int64  `json:"latency_ms,omitempty"`
	Bid          bool   `json:"bid"`
	ContractOK   bool   `json:"contract_ok"`
	Error        string `json:"error,omitempty"`
	ResponseSize int    `json:"response_size_bytes,omitempty"`
}

type Summary struct {
	TotalChecks     int      `json:"total_checks"`
	PassedChecks    int      `json:"passed_checks"`
	FailedChecks    int      `json:"failed_checks"`
	SupportedMedia  []string `json:"supported_media"`
	AuthTested      bool     `json:"auth_tested"`
	SignatureTested bool     `json:"signature_tested"`
	Ready           bool     `json:"ready"`
	NoBidOK         bool     `json:"no_bid_ok"`
	MalformedOK     bool     `json:"malformed_ok"`
	MaxLatencyMS    int64    `json:"max_latency_ms"`
}

func Run(ctx context.Context, options Options) (Report, error) {
	if options.Endpoint == "" {
		return Report{}, fmt.Errorf("endpoint is required")
	}
	if options.Timeout <= 0 {
		options.Timeout = 2 * time.Second
	}
	samplePaths := resolvedSamplePaths(options)
	client := &http.Client{Timeout: options.Timeout}
	report := Report{
		OK:          true,
		Endpoint:    options.Endpoint,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Contract:    "clearledger.openrtb.approved_buyer.v1",
		BuyerID:     strings.TrimSpace(options.BuyerID),
		SeatID:      strings.TrimSpace(options.SeatID),
		TimeoutMS:   options.Timeout.Milliseconds(),
	}
	add := func(check Check) {
		report.Checks = append(report.Checks, check)
		if check.OK {
			report.Summary.PassedChecks++
		} else {
			report.Summary.FailedChecks++
		}
		if check.LatencyMS > report.Summary.MaxLatencyMS {
			report.Summary.MaxLatencyMS = check.LatencyMS
		}
		if check.Name == "readyz" {
			report.Summary.Ready = check.OK
		}
		if check.Name == "clean_no_bid" {
			report.Summary.NoBidOK = check.OK
		}
		if check.Name == "malformed_rejected" {
			report.Summary.MalformedOK = check.OK
		}
		if !check.OK {
			report.OK = false
		}
	}
	addSimple := func(name string, ok bool, detail string) {
		add(Check{Name: name, OK: ok, Detail: detail})
	}

	readyOK, readyLatency, readyStatus := ready(ctx, client, options.Endpoint)
	add(Check{Name: "readyz", OK: readyOK, LatencyMS: readyLatency, StatusCode: readyStatus})
	for _, samplePath := range samplePaths {
		sample, err := os.ReadFile(samplePath)
		label := sampleLabel(samplePath)
		media := MediaResult{MediaType: label, Sample: samplePath}
		if err != nil {
			addSimple(label+"_readable", false, err.Error())
			media.Error = err.Error()
			report.Media = append(report.Media, media)
			continue
		}
		addSimple(label+"_readable", true, "")
		req, err := openrtb.DecodeRequest(sample)
		if err != nil {
			addSimple(label+"_request_valid", false, err.Error())
			media.Error = err.Error()
			report.Media = append(report.Media, media)
			continue
		}
		addSimple(label+"_request_valid", true, "")

		start := time.Now()
		status, body, err := postOpenRTB(ctx, client, options, sample)
		latency := time.Since(start).Milliseconds()
		media.HTTPStatus = status
		media.LatencyMS = latency
		media.ResponseSize = len(body)
		if err != nil {
			add(Check{Name: label + "_valid_bid_http", OK: false, Detail: err.Error(), LatencyMS: latency, StatusCode: status})
			media.Error = err.Error()
		} else {
			add(Check{Name: label + "_valid_bid_http", OK: status == http.StatusOK, Detail: fmt.Sprintf("status=%d", status), LatencyMS: latency, StatusCode: status})
			var resp openrtb.BidResponse
			if err := json.Unmarshal(body, &resp); err != nil {
				addSimple(label+"_valid_bid_json", false, err.Error())
				media.Error = err.Error()
			} else {
				media.Bid = true
				addSimple(label+"_valid_bid_json", true, "")
				contractDetail := validationDetail(req, &resp)
				media.ContractOK = contractDetail == ""
				if !media.ContractOK {
					media.Error = contractDetail
				} else {
					report.Summary.SupportedMedia = appendUnique(report.Summary.SupportedMedia, label)
				}
				addSimple(label+"_valid_bid_contract", media.ContractOK, contractDetail)
			}
		}
		report.Media = append(report.Media, media)
	}

	sample, err := os.ReadFile(samplePaths[0])
	if err != nil {
		finalizeSummary(&report, options)
		return report, nil
	}
	noBidBody, _ := mutateFloor(sample, 999999)
	start := time.Now()
	status, _, err := postOpenRTB(ctx, client, options, noBidBody)
	add(Check{Name: "clean_no_bid", OK: err == nil && status == http.StatusNoContent, Detail: statusDetail(status, err), LatencyMS: time.Since(start).Milliseconds(), StatusCode: status})

	start = time.Now()
	status, _, err = postOpenRTB(ctx, client, options, []byte(`{"id":"bad","site":{"domain":"example.com"},"imp":[]}`))
	add(Check{Name: "malformed_rejected", OK: err == nil && status == http.StatusBadRequest, Detail: statusDetail(status, err), LatencyMS: time.Since(start).Milliseconds(), StatusCode: status})
	finalizeSummary(&report, options)
	return report, nil
}

func finalizeSummary(report *Report, options Options) {
	report.Summary.TotalChecks = len(report.Checks)
	report.Summary.AuthTested = strings.TrimSpace(options.Token) != ""
	report.Summary.SignatureTested = strings.TrimSpace(options.SigningSecret) != ""
	report.Summary.SupportedMedia = append([]string(nil), report.Summary.SupportedMedia...)
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

func resolvedSamplePaths(options Options) []string {
	if len(options.SamplePaths) > 0 {
		return options.SamplePaths
	}
	if strings.TrimSpace(options.SamplePath) != "" {
		return []string{options.SamplePath}
	}
	return []string{
		"samples/openrtb-video-request.json",
		"samples/openrtb-audio-request.json",
		"samples/openrtb-display-request.json",
		"samples/openrtb-native-request.json",
	}
}

func sampleLabel(path string) string {
	name := path
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	name = strings.TrimSuffix(name, ".json")
	name = strings.TrimPrefix(name, "openrtb-")
	name = strings.TrimSuffix(name, "-request")
	return strings.NewReplacer("-", "_", ".", "_").Replace(name)
}

func ready(ctx context.Context, client *http.Client, endpoint string) (bool, int64, int) {
	readyURL, err := siblingURL(endpoint, "/readyz")
	if err != nil {
		return false, 0, 0
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, readyURL, nil)
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return false, time.Since(start).Milliseconds(), 0
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300, time.Since(start).Milliseconds(), resp.StatusCode
}

func postOpenRTB(ctx context.Context, client *http.Client, options Options, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, options.Endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-OpenRTB-Version", "2.6")
	if strings.TrimSpace(options.BuyerID) != "" {
		req.Header.Set("X-ClearLedger-Buyer-ID", strings.TrimSpace(options.BuyerID))
	}
	if strings.TrimSpace(options.SeatID) != "" {
		req.Header.Set("X-ClearLedger-Seat-ID", strings.TrimSpace(options.SeatID))
	}
	if options.Token != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimPrefix(options.Token, "Bearer "))
	}
	if options.SigningSecret != "" {
		auctionID, requestID := requestIDs(body)
		timestamp := time.Now().UTC().Format(time.RFC3339Nano)
		bodyHash := sha256Hex(body)
		base := timestamp + "\n" + auctionID + "\n" + requestID + "\n" + bodyHash
		mac := hmac.New(sha256.New, []byte(options.SigningSecret))
		mac.Write([]byte(base))
		req.Header.Set("X-ClearLedger-Buyer-Timestamp", timestamp)
		req.Header.Set("X-ClearLedger-Auction-ID", auctionID)
		req.Header.Set("X-ClearLedger-Request-ID", requestID)
		req.Header.Set("X-ClearLedger-Buyer-Body-SHA256", bodyHash)
		req.Header.Set("X-ClearLedger-Buyer-Signature", "hmac-sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, respBody, nil
}

func mutateFloor(body []byte, floor float64) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	imps, _ := payload["imp"].([]any)
	if len(imps) > 0 {
		if imp, ok := imps[0].(map[string]any); ok {
			imp["bidfloor"] = floor
		}
	}
	return json.Marshal(payload)
}

func requestIDs(body []byte) (string, string) {
	var payload struct {
		ID     string         `json:"id"`
		Source map[string]any `json:"source"`
	}
	_ = json.Unmarshal(body, &payload)
	requestID := strings.TrimSpace(payload.ID)
	if requestID == "" {
		requestID = "cert_request"
	}
	auctionID := requestID
	if payload.Source != nil {
		if tid, ok := payload.Source["tid"].(string); ok && strings.TrimSpace(tid) != "" {
			auctionID = strings.TrimSpace(tid)
		}
	}
	return auctionID, requestID
}

func siblingURL(endpoint string, path string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	parsed.Path = path
	parsed.RawQuery = ""
	return parsed.String(), nil
}

func validationDetail(req *openrtb.BidRequest, resp *openrtb.BidResponse) string {
	if err := openrtb.ValidateBidResponse(req, resp); err != nil {
		return err.Error()
	}
	return ""
}

func statusDetail(status int, err error) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("status=%d", status)
}

func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
