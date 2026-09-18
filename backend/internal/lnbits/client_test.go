package lnbits

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *[]string) {
	t.Helper()

	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)

		handler(w, r)
	}))
	t.Cleanup(server.Close)

	return NewClient(Config{
		URL:        server.URL,
		APIKey:     "invoice-key",
		AdminKey:   "admin-key",
		WebhookURL: "http://ganji.test/webhooks/lnbits",
	}), &requests
}

func TestCreateHoldInvoice(t *testing.T) {
	client, requests := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "invoice-key" {
			t.Errorf("expected invoice key, got %q", r.Header.Get("X-Api-Key"))
		}

		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}

		if req["out"] != false {
			t.Errorf("expected out false, got %v", req["out"])
		}
		if req["unit"] != "sat" {
			t.Errorf("expected default unit sat, got %v", req["unit"])
		}
		if req["payment_hash"] != "aa" {
			t.Errorf("expected payment_hash aa, got %v", req["payment_hash"])
		}
		if req["webhook"] == "" {
			t.Errorf("expected configured webhook to be attached")
		}
		if req["memo"] != "Build a site" {
			t.Errorf("unexpected memo %v", req["memo"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"checking_id":"c1","payment_hash":"ph1","payment_request":"lnbc1"}`))
	})

	invoice, err := client.CreateHoldInvoice(context.Background(), CreateHoldInvoiceRequest{
		Out:         false,
		Amount:      5000,
		Memo:        "Build a site",
		PaymentHash: "aa",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if invoice.CheckingID != "c1" || invoice.PaymentHash != "ph1" || invoice.PaymentRequest != "lnbc1" {
		t.Fatalf("unexpected invoice: %+v", invoice)
	}

	if (*requests)[0] != "POST /api/v1/payments" {
		t.Fatalf("unexpected request %q", (*requests)[0])
	}
}

func TestCreateHoldInvoiceUsesConfiguredExpiry(t *testing.T) {
	var expiry int64
	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)

		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if req["unit"] != "sat" {
			t.Errorf("expected default unit sat, got %v", req["unit"])
		}
		expiry = int64(req["expiry"].(float64))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"checking_id":"c1","payment_hash":"ph1","payment_request":"lnbc1"}`))
	}))
	defer server.Close()

	client := NewClient(Config{
		URL:           server.URL,
		APIKey:        "invoice-key",
		HoldExpirySec: 2_592_000,
	})

	if _, err := client.CreateHoldInvoice(context.Background(), CreateHoldInvoiceRequest{
		Out:         false,
		Amount:      5000,
		PaymentHash: "aa",
		Memo:        "Build a site",
	}); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if expiry != 2_592_000 {
		t.Errorf("expected configured 30-day expiry, got %d", expiry)
	}
	if len(requests) != 1 {
		t.Fatalf("expected exactly one request, got %v", requests)
	}
}

func TestCreateHoldInvoiceError(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":"invalid preimage"}`))
	})

	_, err := client.CreateHoldInvoice(context.Background(), CreateHoldInvoiceRequest{
		Out:         false,
		Amount:      100,
		PaymentHash: "aa",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid preimage") {
		t.Fatalf("expected LNbits body in error, got %v", err)
	}
}

func TestSettleHoldUsesAdminKey(t *testing.T) {
	client, requests := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "admin-key" {
			t.Errorf("expected admin key, got %q", r.Header.Get("X-Api-Key"))
		}

		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if req["preimage"] != "deadbeef" {
			t.Errorf("expected preimage deadbeef, got %v", req["preimage"])
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"checking_id":"c1"}`))
	})

	if _, err := client.SettleHold(context.Background(), "deadbeef"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if (*requests)[0] != "POST /api/v1/payments/settle" {
		t.Fatalf("unexpected request %q", (*requests)[0])
	}
}

func TestSettleHoldRefused(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":false,"checking_id":"c1","error_message":"invoice not held"}`))
	})

	if _, err := client.SettleHold(context.Background(), "deadbeef"); err == nil {
		t.Fatal("expected error for refused settle, got nil")
	}
}

func TestSettleHoldError(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":"payment not accepted"}`))
	})

	_, err := client.SettleHold(context.Background(), "deadbeef")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "payment not accepted") {
		t.Fatalf("expected LNbits body in error, got %v", err)
	}
}

func TestCancelHoldUsesAdminKey(t *testing.T) {
	client, requests := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "admin-key" {
			t.Errorf("expected admin key, got %q", r.Header.Get("X-Api-Key"))
		}

		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if req["payment_hash"] != "ph1" {
			t.Errorf("expected payment_hash ph1, got %v", req["payment_hash"])
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"checking_id":"c1"}`))
	})

	if _, err := client.CancelHold(context.Background(), "ph1"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if (*requests)[0] != "POST /api/v1/payments/cancel" {
		t.Fatalf("unexpected request %q", (*requests)[0])
	}
}

func TestCancelHoldRefused(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":false,"checking_id":"c1","error_message":"invoice not held"}`))
	})

	if _, err := client.CancelHold(context.Background(), "ph1"); err == nil {
		t.Fatal("expected error for refused cancel, got nil")
	}
}

func TestPayInvoiceUsesAdminKey(t *testing.T) {
	client, requests := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "admin-key" {
			t.Errorf("expected admin key, got %q", r.Header.Get("X-Api-Key"))
		}

		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if req["out"] != true {
			t.Errorf("expected out true, got %v", req["out"])
		}
		if req["bolt11"] != "lnbc2" {
			t.Errorf("expected bolt11 lnbc2, got %v", req["bolt11"])
		}

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"payment_hash":"out1","checking_id":"oc1"}`))
	})

	_, err := client.PayInvoice(context.Background(), "lnbc2")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if (*requests)[0] != "POST /api/v1/payments" {
		t.Fatalf("unexpected request %q", (*requests)[0])
	}
}

func TestPayInvoiceError(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"no route"}`))
	})

	_, err := client.PayInvoice(context.Background(), "lnbc2")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no route") {
		t.Fatalf("expected LNbits body in error, got %v", err)
	}
	if errors.Is(err, ErrPayoutRefused) {
		t.Fatal("a 5xx is ambiguous and must NOT be classified as a definite refusal")
	}
}

func TestPayInvoice4xxIsClassifiedRefused(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":"invalid bolt11"}`))
	})

	_, err := client.PayInvoice(context.Background(), "lnbc-bad")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrPayoutRefused) {
		t.Fatalf("expected ErrPayoutRefused, got %v", err)
	}
}

func TestCheckPayment(t *testing.T) {
	client, requests := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"paid":true,"details":{"checking_id":"c1","status":"success"}}`))
	})

	resp, err := client.CheckPayment(context.Background(), "c1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !resp.Paid {
		t.Fatal("expected paid true")
	}
	if (*requests)[0] != "GET /api/v1/payments/c1" {
		t.Fatalf("unexpected request %q", (*requests)[0])
	}
}
