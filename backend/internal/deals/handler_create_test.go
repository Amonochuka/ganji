package deals

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Amonochuka/ganji-backend/internal/lnbits"
)

func TestCreateDealReportsInvoiceFailureClearly(t *testing.T) {
	repo := newFakeDealRepo()
	service := NewService(repo, lnbits.NewClient(lnbits.Config{URL: "http://127.0.0.1:1", APIKey: "invoice-key"}))
	router := newTestDealRouter(t, service, "freelancer-1", "freelancer@example.com")
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/deals", strings.NewReader(`{"title":"Build a site","amount_sats":5000,"source_platform":"telegram","client_email":"client@example.com","payee_invoice":"lnbc1"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "Check backend logs") {
		t.Fatalf("expected generic non-leaking error, got %s", resp.Body.String())
	}
}

func TestCreateDealReportsPersistenceFailureAndCancelsOrphanHold(t *testing.T) {
	var cancelled bool
	lnbitsServer := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/payments/cancel" {
			cancelled = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		var body struct {
			PaymentHash string `json:"payment_hash"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode create invoice request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"checking_id":"chk-1","payment_hash":"` + body.PaymentHash + `","payment_request":"lnbc1"}`))
	})
	defer lnbitsServer.Close()

	repo := newFakeDealRepo()
	repo.createErr = errors.New("repository: create deal: constraint violation")
	service := NewService(repo, lnbits.NewClient(lnbits.Config{URL: lnbitsServer.URL, APIKey: "invoice-key", AdminKey: "admin-key"}))
	router := newTestDealRouter(t, service, "freelancer-1", "freelancer@example.com")
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/deals", strings.NewReader(`{"title":"Build a site","amount_sats":5000,"source_platform":"telegram","client_email":"client@example.com","payee_invoice":"lnbc1"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "Check backend logs") {
		t.Fatalf("expected generic non-leaking error, got %s", resp.Body.String())
	}
	if !cancelled {
		t.Fatal("expected service to cancel hold after database insert failure")
	}
}

func TestCreateDealCancelsInvoiceWhenLNbitsSuccessResponseIsIncomplete(t *testing.T) {
	var cancelledHash string
	lnbitsServer := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/payments/cancel" {
			var body struct {
				PaymentHash string `json:"payment_hash"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode cancel request: %v", err)
			}
			cancelledHash = body.PaymentHash
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		var body struct {
			PaymentHash string `json:"payment_hash"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode create request: %v", err)
		}
		// Simulate LNbits creating the invoice but omitting its bolt11 in a 2xx response.
		_, _ = w.Write([]byte(`{"checking_id":"chk-1","payment_hash":"` + body.PaymentHash + `"}`))
	})
	defer lnbitsServer.Close()

	repo := newFakeDealRepo()
	service := NewService(repo, lnbits.NewClient(lnbits.Config{URL: lnbitsServer.URL, APIKey: "invoice-key", AdminKey: "admin-key"}))
	router := newTestDealRouter(t, service, "freelancer-1", "freelancer@example.com")
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/deals", strings.NewReader(`{"title":"Build a site","amount_sats":5000,"source_platform":"telegram","client_email":"client@example.com","payee_invoice":"lnbc1"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", resp.Code, resp.Body.String())
	}
	if len(repo.deals) != 0 {
		t.Fatalf("incomplete provider response must not persist a deal, got %d rows", len(repo.deals))
	}
	if cancelledHash == "" {
		t.Fatal("expected the possibly-created hold invoice to be cancelled")
	}
}

// newTestServer keeps endpoint tests independent from a running LNbits service.
func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler(w, r) }))
}
