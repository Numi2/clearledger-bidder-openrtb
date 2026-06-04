package registration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Numi2/clearledger-bidder-openrtb/internal/config"
)

func Register(ctx context.Context, cfg config.Config) error {
	if cfg.ClearLedgerRegisterURL == "" {
		return fmt.Errorf("CLEARLEDGER_REGISTER_URL is required")
	}
	payload, err := Payload(cfg)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.ClearLedgerRegisterURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.ClearLedgerAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.ClearLedgerAPIKey)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("registration failed status=%d body=%s", resp.StatusCode, string(respBody))
	}
	return nil
}

func Payload(cfg config.Config) (map[string]any, error) {
	endpoint := cfg.RegistrationEndpoint()
	if endpoint == "" {
		return nil, fmt.Errorf("BIDDER_OPENRTB_ENDPOINT or BIDDER_PUBLIC_ENDPOINT is required for registration")
	}
	return map[string]any{
		"buyer_id": cfg.BuyerID,
		"seat":     cfg.Seat,
		"endpoint": endpoint,
		"protocol": "openrtb-2.6-json",
		"auth": map[string]any{
			"bearer":      cfg.RequireAuth,
			"hmac_sha256": cfg.RequireSignature,
			"signature_headers": []string{
				"X-ClearLedger-Buyer-Timestamp",
				"X-ClearLedger-Auction-ID",
				"X-ClearLedger-Request-ID",
				"X-ClearLedger-Buyer-Body-SHA256",
				"X-ClearLedger-Buyer-Signature",
			},
		},
		"supported_media": supportedMedia(cfg),
		"no_bid":          map[string]any{"http_status": http.StatusNoContent},
	}, nil
}

func supportedMedia(cfg config.Config) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, campaign := range cfg.Campaigns {
		for _, mediaType := range campaign.MediaTypes {
			mediaType = normalizeMediaType(mediaType)
			if _, ok := seen[mediaType]; ok {
				continue
			}
			seen[mediaType] = struct{}{}
			out = append(out, mediaType)
		}
	}
	sort.Strings(out)
	return out
}

func normalizeMediaType(value string) string {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", "_"))
	if value == "banner" {
		return "display"
	}
	return value
}
