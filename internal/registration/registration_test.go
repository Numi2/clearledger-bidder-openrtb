package registration

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/Numi2/clearledger-bidder-openrtb/internal/config"
)

func TestPayloadDerivesOpenRTBEndpointAndContractFields(t *testing.T) {
	payload, err := Payload(config.Config{
		PublicEndpoint:   "https://agency-bidder.example.com",
		BuyerID:          "agency_bidder_1",
		Seat:             "agency_seat_1",
		RequireAuth:      true,
		RequireSignature: true,
		Campaigns: []config.Campaign{
			{MediaTypes: []string{"video", "banner"}},
			{MediaTypes: []string{"native", "video", "audio"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload["endpoint"] != "https://agency-bidder.example.com/openrtb" {
		t.Fatalf("endpoint=%#v", payload["endpoint"])
	}
	if payload["buyer_id"] != "agency_bidder_1" || payload["seat"] != "agency_seat_1" {
		t.Fatalf("identity fields missing: %#v", payload)
	}
	if !reflect.DeepEqual(payload["supported_media"], []string{"audio", "display", "native", "video"}) {
		t.Fatalf("supported media=%#v", payload["supported_media"])
	}
	if payload["contract"] != "clearledger.openrtb.approved_buyer.v1" {
		t.Fatalf("contract=%#v", payload["contract"])
	}
	auth := payload["auth"].(map[string]any)
	if auth["bearer"] != true || auth["hmac_sha256"] != true {
		t.Fatalf("auth=%#v", auth)
	}
	if len(auth["signature_headers"].([]string)) != 5 {
		t.Fatalf("signature headers=%#v", auth["signature_headers"])
	}
	if payload["no_bid"].(map[string]any)["http_status"] != http.StatusNoContent {
		t.Fatalf("no_bid=%#v", payload["no_bid"])
	}
	certification := payload["certification"].(map[string]any)
	if certification["required"] != true || len(certification["checks"].([]string)) == 0 {
		t.Fatalf("certification=%#v", certification)
	}
	operatorEndpoints := payload["operator_endpoints"].(map[string]any)
	if operatorEndpoints["ready"] != "https://agency-bidder.example.com/readyz" {
		t.Fatalf("operator endpoints=%#v", operatorEndpoints)
	}
}

func TestPayloadUsesExplicitOpenRTBEndpoint(t *testing.T) {
	payload, err := Payload(config.Config{
		PublicEndpoint:  "https://agency-bidder.example.com",
		OpenRTBEndpoint: "https://rtb.example.com/buyers/agency/openrtb",
		BuyerID:         "buyer",
		Seat:            "seat",
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload["endpoint"] != "https://rtb.example.com/buyers/agency/openrtb" {
		t.Fatalf("endpoint=%#v", payload["endpoint"])
	}
}

func TestPayloadRequiresEndpoint(t *testing.T) {
	if _, err := Payload(config.Config{BuyerID: "buyer", Seat: "seat"}); err == nil {
		t.Fatal("expected endpoint error")
	}
}
