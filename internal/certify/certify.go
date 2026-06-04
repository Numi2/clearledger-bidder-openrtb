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
	OK     bool    `json:"ok"`
	Checks []Check `json:"checks"`
}

type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
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
	report := Report{OK: true}
	add := func(name string, ok bool, detail string) {
		report.Checks = append(report.Checks, Check{Name: name, OK: ok, Detail: detail})
		if !ok {
			report.OK = false
		}
	}

	add("readyz", ready(ctx, client, options.Endpoint), "")
	for _, samplePath := range samplePaths {
		sample, err := os.ReadFile(samplePath)
		label := sampleLabel(samplePath)
		if err != nil {
			add(label+"_readable", false, err.Error())
			continue
		}
		add(label+"_readable", true, "")
		req, err := openrtb.DecodeRequest(sample)
		if err != nil {
			add(label+"_request_valid", false, err.Error())
			continue
		}
		add(label+"_request_valid", true, "")

		status, body, err := postOpenRTB(ctx, client, options, sample)
		if err != nil {
			add(label+"_valid_bid_http", false, err.Error())
		} else {
			add(label+"_valid_bid_http", status == http.StatusOK, fmt.Sprintf("status=%d", status))
			var resp openrtb.BidResponse
			if err := json.Unmarshal(body, &resp); err != nil {
				add(label+"_valid_bid_json", false, err.Error())
			} else {
				add(label+"_valid_bid_json", true, "")
				add(label+"_valid_bid_contract", openrtb.ValidateBidResponse(req, &resp) == nil, validationDetail(req, &resp))
			}
		}
	}

	sample, err := os.ReadFile(samplePaths[0])
	if err != nil {
		return report, nil
	}
	noBidBody, _ := mutateFloor(sample, 999999)
	status, _, err := postOpenRTB(ctx, client, options, noBidBody)
	add("clean_no_bid", err == nil && status == http.StatusNoContent, statusDetail(status, err))

	status, _, err = postOpenRTB(ctx, client, options, []byte(`{"id":"bad","site":{"domain":"example.com"},"imp":[]}`))
	add("malformed_rejected", err == nil && status == http.StatusBadRequest, statusDetail(status, err))
	return report, nil
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

func ready(ctx context.Context, client *http.Client, endpoint string) bool {
	readyURL, err := siblingURL(endpoint, "/readyz")
	if err != nil {
		return false
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, readyURL, nil)
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
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
