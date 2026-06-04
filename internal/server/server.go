package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Numi2/clearledger-bidder-openrtb/internal/bidder"
	"github.com/Numi2/clearledger-bidder-openrtb/internal/config"
	"github.com/Numi2/clearledger-bidder-openrtb/internal/openrtb"
)

const maxRequestBody = 256 << 10

type Server struct {
	cfg    config.Config
	engine *bidder.Engine
	mu     sync.Mutex
	counts map[string]uint64
}

func New(cfg config.Config, engine *bidder.Engine) http.Handler {
	s := &Server{cfg: cfg, engine: engine, counts: map[string]uint64{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.health)
	mux.HandleFunc("/readyz", s.ready)
	mux.HandleFunc("/metrics", s.metrics)
	mux.HandleFunc("/openrtb", s.openrtb)
	mux.HandleFunc("/buyers/", s.buyerOpenRTB)
	mux.HandleFunc("/events/", s.event)
	return mux
}

func (s *Server) openrtb(w http.ResponseWriter, r *http.Request) {
	start := time.Now().UTC()
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
	if err != nil || len(body) > maxRequestBody {
		s.observe("request_too_large")
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "request_too_large"})
		return
	}
	if err := s.authorize(r, body, start); err != nil {
		s.observe("unauthorized")
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": err.Error()})
		return
	}
	req, err := openrtb.DecodeRequest(body)
	if err != nil {
		s.observe("malformed")
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "malformed_openrtb", "detail": err.Error()})
		return
	}
	decision := s.engine.Bid(r.Context(), req, start)
	if decision.NoBid || decision.Response == nil {
		s.observe("nobid_" + metricLabel(decision.Reason))
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.observe("bid")
	writeJSON(w, http.StatusOK, decision.Response)
}

func (s *Server) buyerOpenRTB(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/buyers/"), "/")
	if !strings.HasSuffix(path, "/openrtb") {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
		return
	}
	s.openrtb(w, r)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"service":  "clearledger-bidder-openrtb",
		"buyer_id": s.cfg.BuyerID,
		"seat":     s.cfg.Seat,
		"endpoint": "/openrtb",
	})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ok := len(s.cfg.Campaigns) > 0
	status := http.StatusOK
	if !ok {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{
		"ok":        ok,
		"campaigns": len(s.cfg.Campaigns),
		"auth": map[string]any{
			"bearer_required":    s.cfg.RequireAuth,
			"signature_required": s.cfg.RequireSignature,
		},
	})
}

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	s.mu.Lock()
	defer s.mu.Unlock()
	for label, count := range s.counts {
		_, _ = fmt.Fprintf(w, "clearledger_bidder_openrtb_requests_total{result=%q} %d\n", label, count)
	}
}

func (s *Server) event(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	s.observe("event_" + metricLabel(strings.TrimPrefix(r.URL.Path, "/events/")))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authorize(r *http.Request, body []byte, now time.Time) error {
	if s.cfg.RequireAuth {
		got := strings.TrimSpace(r.Header.Get("Authorization"))
		want := "Bearer " + s.cfg.AuthToken
		if s.cfg.AuthToken == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			return errors.New("unauthorized")
		}
	}
	if s.cfg.RequireSignature {
		if s.cfg.SigningSecret == "" {
			return errors.New("signature_not_configured")
		}
		if productionSignatureHeadersPresent(r) {
			if err := verifyProductionBuyerSignature(r, body, now, s.cfg.SigningSecret, s.cfg.SignatureSkewDuration()); err != nil {
				return err
			}
		} else if err := verifyLocalSignature(r, body, now, s.cfg.SigningSecret, s.cfg.SignatureSkewDuration()); err != nil {
			return err
		}
	}
	return nil
}

func productionSignatureHeadersPresent(r *http.Request) bool {
	return strings.TrimSpace(r.Header.Get("X-ClearLedger-Buyer-Timestamp")) != "" ||
		strings.TrimSpace(r.Header.Get("X-ClearLedger-Buyer-Signature")) != "" ||
		strings.TrimSpace(r.Header.Get("X-ClearLedger-Buyer-Body-SHA256")) != ""
}

func verifyProductionBuyerSignature(r *http.Request, body []byte, now time.Time, secret string, maxSkew time.Duration) error {
	timestamp := strings.TrimSpace(r.Header.Get("X-ClearLedger-Buyer-Timestamp"))
	auctionID := strings.TrimSpace(r.Header.Get("X-ClearLedger-Auction-ID"))
	requestID := strings.TrimSpace(r.Header.Get("X-ClearLedger-Request-ID"))
	providedBodyHash := strings.ToLower(strings.TrimSpace(r.Header.Get("X-ClearLedger-Buyer-Body-SHA256")))
	providedSignature := strings.ToLower(strings.TrimSpace(r.Header.Get("X-ClearLedger-Buyer-Signature")))
	if timestamp == "" || auctionID == "" || requestID == "" || providedBodyHash == "" || providedSignature == "" {
		return errors.New("missing_signature_headers")
	}
	parsed, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, timestamp)
	}
	if err != nil {
		return errors.New("invalid_signature_timestamp")
	}
	if age := now.Sub(parsed); age < -maxSkew || age > maxSkew {
		return errors.New("stale_signature")
	}
	bodyHash := sha256Hex(body)
	if subtle.ConstantTimeCompare([]byte(providedBodyHash), []byte(bodyHash)) != 1 {
		return errors.New("body_hash_mismatch")
	}
	base := timestamp + "\n" + auctionID + "\n" + requestID + "\n" + bodyHash
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(base))
	want := "hmac-sha256=" + hex.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(providedSignature), []byte(want)) != 1 {
		return errors.New("signature_mismatch")
	}
	return nil
}

func verifyLocalSignature(r *http.Request, body []byte, now time.Time, secret string, maxSkew time.Duration) error {
	timestamp := strings.TrimSpace(r.Header.Get("X-ClearLedger-Timestamp"))
	signature := strings.TrimSpace(r.Header.Get("X-ClearLedger-Signature"))
	if timestamp == "" || signature == "" {
		return errors.New("missing_signature")
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return errors.New("invalid_signature_timestamp")
	}
	age := now.Sub(time.Unix(ts, 0))
	if age < -maxSkew || age > maxSkew {
		return errors.New("stale_signature")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(signature), []byte(want)) != 1 {
		return errors.New("bad_signature")
	}
	return nil
}

func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func (s *Server) observe(label string) {
	s.mu.Lock()
	s.counts[metricLabel(label)]++
	s.mu.Unlock()
}

func metricLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unknown"
	}
	var out strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			out.WriteRune(r)
			continue
		}
		out.WriteByte('_')
	}
	return out.String()
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}
