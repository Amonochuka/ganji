package lnd

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const testPaymentHash = "1111111111111111111111111111111111111111111111111111111111111111"

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, string) {
	t.Helper()

	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)

	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls.cert")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	macaroonPath := filepath.Join(dir, "admin.macaroon")
	if err := os.WriteFile(macaroonPath, []byte("test-macaroon"), 0o600); err != nil {
		t.Fatalf("write macaroon: %v", err)
	}

	client, err := NewClient(Config{URL: server.URL, CertPath: certPath, MacaroonPath: macaroonPath})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client, hex.EncodeToString([]byte("test-macaroon"))
}

func TestLookupInvoiceAcceptsHeldInvoice(t *testing.T) {
	hashBytes, err := hex.DecodeString(testPaymentHash)
	if err != nil {
		t.Fatalf("decode hash: %v", err)
	}
	hashBase64 := base64.StdEncoding.EncodeToString(hashBytes)

	var gotMacaroon string
	client, wantMacaroon := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/invoice/"+testPaymentHash {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		gotMacaroon = r.Header.Get("Grpc-Metadata-macaroon")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"r_hash":"` + hashBase64 + `","state":"ACCEPTED","settled":false,"settle_date":"0","value":"5000","amt_paid_sat":"5000"}`))
	})

	invoice, err := client.LookupInvoice(context.Background(), testPaymentHash)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if gotMacaroon != wantMacaroon {
		t.Fatalf("unexpected macaroon header")
	}
	if !invoice.IsAcceptedFor(testPaymentHash, 5000) {
		t.Fatalf("expected accepted hold, got %+v", invoice)
	}
}

func TestInvoiceAcceptedChecks(t *testing.T) {
	hash := base64.StdEncoding.EncodeToString(mustDecodeHash(t, testPaymentHash))
	tests := []struct {
		name   string
		body   string
		hash   string
		amount int64
		want   bool
	}{
		{
			name:   "open invoice",
			body:   `{"r_hash":"` + hash + `","state":"OPEN","settled":false,"settle_date":"0","value":"5000","amt_paid_sat":"0"}`,
			hash:   testPaymentHash,
			amount: 5000,
			want:   false,
		},
		{
			name:   "settled invoice",
			body:   `{"r_hash":"` + hash + `","state":"SETTLED","settled":true,"settle_date":"1","value":"5000","amt_paid_sat":"5000"}`,
			hash:   testPaymentHash,
			amount: 5000,
			want:   false,
		},
		{
			name:   "wrong hash",
			body:   `{"r_hash":"` + hash + `","state":"ACCEPTED","settled":false,"settle_date":"0","value":"5000","amt_paid_sat":"5000"}`,
			hash:   "2222222222222222222222222222222222222222222222222222222222222222",
			amount: 5000,
			want:   false,
		},
		{
			name:   "partial amount",
			body:   `{"r_hash":"` + hash + `","state":"ACCEPTED","settled":false,"settle_date":"0","value":"5000","amt_paid_sat":"4000"}`,
			hash:   testPaymentHash,
			amount: 5000,
			want:   false,
		},
		{
			name:   "mismatched amount",
			body:   `{"r_hash":"` + hash + `","state":"ACCEPTED","settled":false,"settle_date":"0","value":"4000","amt_paid_sat":"5000"}`,
			hash:   testPaymentHash,
			amount: 5000,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			})
			invoice, err := client.LookupInvoice(context.Background(), testPaymentHash)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := invoice.IsAcceptedFor(tt.hash, tt.amount); got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestLookupInvoiceRejectsInvalidHash(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request should not be sent")
	})

	_, err := client.LookupInvoice(context.Background(), "not-a-hash")
	if !errors.Is(err, ErrInvalidPaymentHash) {
		t.Fatalf("expected ErrInvalidPaymentHash, got %v", err)
	}
}

func TestLookupInvoiceReturnsHTTPError(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	})

	_, err := client.LookupInvoice(context.Background(), testPaymentHash)
	if err == nil {
		t.Fatal("expected error")
	}
}

func mustDecodeHash(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("decode hash: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("expected 32-byte hash, got %d", len(decoded))
	}
	return decoded
}
