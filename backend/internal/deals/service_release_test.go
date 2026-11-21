package deals

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Amonochuka/ganji-backend/internal/lnbits"
)

func releaseDeal(repo *fakeDealRepo, id string) *Deal {
	d := &Deal{
		ID:           id,
		ShareToken:   "share-" + id,
		FreelancerID: "freelancer-1",
		ClientEmail:  "client@example.com",
		Title:        "deal",
		AmountSats:   1000,
		Status:       StatusWorkSubmitted,
		Preimage:     "pre-" + id,
		PreimageHash: "hash-" + id,
		CheckingID:   "chk-hold-" + id,
		PayeeInvoice: "lnbc-payee-" + id,
		CreatedAt:    time.Now(),
	}
	repo.deals[id] = d
	return d
}

func releaseServer(t *testing.T, holdStatus, payoutStatus string, payErr error) (*httptest.Server, *int32, *int32) {
	t.Helper()
	var settleCount, payCount int32
	payID := 0
	payoutStatuses := map[string]string{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/payments/settle", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&settleCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"checking_id":"chk-settle","payment_hash":"hash"}`))
	})
	mux.HandleFunc("/api/v1/payments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		atomic.AddInt32(&payCount, 1)
		if payErr != nil {
			if errors.Is(payErr, lnbits.ErrPayoutRefused) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"ok":false,"error_message":"invoice expired"}`))
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"ok":false,"error_message":"temporary"}`))
			return
		}
		payID++
		id := "chk-pay-" + string(rune('0'+payID))
		payoutStatuses[id] = payoutStatus
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"checking_id":"` + id + `","payment_request":"lnbc"}`))
	})
	mux.HandleFunc("/api/v1/payments/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/api/v1/payments/"):]
		status := holdStatus
		if s, ok := payoutStatuses[id]; ok {
			status = s
		} else if len(id) >= 8 && id[:8] == "chk-pay-" {
			status = payoutStatus
		}
		paid := status == "SETTLED" || status == "SUCCESS" || status == "PAID" || status == "SUCCEEDED"
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"checking_id":"` + id + `","paid":` + map[bool]string{true: "true", false: "false"}[paid] + `,"payment_hash":"hash","details":{"status":"` + status + `"}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &settleCount, &payCount
}

func releaseServerWithStatuses(t *testing.T, holdStatus string, fixed map[string]string, newPayoutStatus string, payErr error) (*httptest.Server, *int32, *int32) {
	t.Helper()
	var settleCount, payCount int32
	payID := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/payments/settle", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&settleCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"checking_id":"chk-settle","payment_hash":"hash"}`))
	})
	mux.HandleFunc("/api/v1/payments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		atomic.AddInt32(&payCount, 1)
		if payErr != nil {
			if errors.Is(payErr, lnbits.ErrPayoutRefused) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"ok":false,"error_message":"invoice expired"}`))
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"ok":false,"error_message":"temporary"}`))
			return
		}
		payID++
		id := "chk-pay-" + string(rune('0'+payID))
		fixed[id] = newPayoutStatus
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"checking_id":"` + id + `","payment_request":"lnbc"}`))
	})
	mux.HandleFunc("/api/v1/payments/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/api/v1/payments/"):]
		status := holdStatus
		if s, ok := fixed[id]; ok {
			status = s
		}
		paid := status == "SETTLED" || status == "SUCCESS" || status == "PAID" || status == "SUCCEEDED"
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"checking_id":"` + id + `","paid":` + map[bool]string{true: "true", false: "false"}[paid] + `,"payment_hash":"hash","details":{"status":"` + status + `"}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &settleCount, &payCount
}
func releaseService(repo *fakeDealRepo, srv *httptest.Server) *Service {
	return NewService(repo, lnbits.NewClient(lnbits.Config{URL: srv.URL, APIKey: "k"}))
}

func TestReleaseEscrowSettledHoldPayoutRefusedKeepsTracking(t *testing.T) {
	repo := newFakeDealRepo()
	deal := releaseDeal(repo, "deal-r1")
	srv, _, payCount := releaseServer(t, "SETTLED", "FAILED", lnbits.ErrPayoutRefused)
	svc := releaseService(repo, srv)

	_, err := svc.releaseEscrow(context.Background(), deal.ID, releaseEscrowOptions{
		authorize: func(d *Deal) error { return nil },
		finalize: func(r DealRepository, id string) error {
			return r.UpdateStatus(context.Background(), id, StatusReleased)
		},
	})
	if !errors.Is(err, ErrPayoutFailed) {
		t.Fatalf("expected ErrPayoutFailed, got %v", err)
	}
	if deal.Status == StatusReleased {
		t.Fatal("must not release on refused payout")
	}
	if !deal.PayoutAttemptedAt.Valid {
		t.Fatal("attempt marker must be kept when hold settled but payout refused")
	}
	if atomic.LoadInt32(payCount) != 1 {
		t.Fatalf("expected 1 payout attempt, got %d", atomic.LoadInt32(payCount))
	}
}

func TestReleaseEscrowSettledHoldFreshPayoutAmbiguous(t *testing.T) {
	repo := newFakeDealRepo()
	deal := releaseDeal(repo, "deal-r2")
	srv, _, _ := releaseServer(t, "SETTLED", "PENDING", errors.New("temp"))
	svc := releaseService(repo, srv)

	_, err := svc.releaseEscrow(context.Background(), deal.ID, releaseEscrowOptions{
		authorize: func(d *Deal) error { return nil },
		finalize: func(r DealRepository, id string) error {
			return r.UpdateStatus(context.Background(), id, StatusReleased)
		},
	})
	if !errors.Is(err, ErrPayoutInFlight) {
		t.Fatalf("expected ErrPayoutInFlight, got %v", err)
	}
	if !deal.PayoutAttemptedAt.Valid {
		t.Fatal("ambiguous payout must keep attempt marker")
	}
	if deal.Status == StatusReleased {
		t.Fatal("must not release on ambiguous payout")
	}
}

func TestReleaseEscrowSettledHoldFailedRecordedPayoutRetries(t *testing.T) {
	repo := newFakeDealRepo()
	deal := releaseDeal(repo, "deal-r3")
	deal.PayoutCheckingID = "chk-pay-old"
	deal.PayoutAttemptedAt = sql.NullTime{Time: time.Now(), Valid: true}
	srv, _, payCount := releaseServerWithStatuses(t, "SETTLED", map[string]string{"chk-pay-old": "FAILED"}, "SETTLED", nil)
	svc := releaseService(repo, srv)

	out, err := svc.releaseEscrow(context.Background(), deal.ID, releaseEscrowOptions{
		authorize: func(d *Deal) error { return nil },
		finalize: func(r DealRepository, id string) error {
			return r.UpdateStatus(context.Background(), id, StatusReleased)
		},
	})
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if out.Status != StatusReleased {
		t.Fatalf("expected released, got %s", out.Status)
	}
	if atomic.LoadInt32(payCount) != 1 {
		t.Fatalf("expected 1 fresh payout, got %d", atomic.LoadInt32(payCount))
	}
}

func TestReleaseEscrowCrashWindowSettledHoldRetriesPayout(t *testing.T) {
	repo := newFakeDealRepo()
	deal := releaseDeal(repo, "deal-r4")
	deal.PayoutAttemptedAt = sql.NullTime{Time: time.Now(), Valid: true}
	srv, settleCount, payCount := releaseServer(t, "SETTLED", "SETTLED", nil)
	svc := releaseService(repo, srv)

	out, err := svc.releaseEscrow(context.Background(), deal.ID, releaseEscrowOptions{
		authorize: func(d *Deal) error { return nil },
		finalize: func(r DealRepository, id string) error {
			return r.UpdateStatus(context.Background(), id, StatusReleased)
		},
	})
	if err != nil {
		t.Fatalf("expected settled crash-window retry, got %v", err)
	}
	if out.Status != StatusReleased {
		t.Fatalf("expected released, got %s", out.Status)
	}
	if atomic.LoadInt32(settleCount) != 1 || atomic.LoadInt32(payCount) != 1 {
		t.Fatalf("expected settle+pay once, got settle=%d pay=%d", atomic.LoadInt32(settleCount), atomic.LoadInt32(payCount))
	}
}

func TestReleaseEscrowCrashWindowUnsettledHoldBlocks(t *testing.T) {
	repo := newFakeDealRepo()
	deal := releaseDeal(repo, "deal-r5")
	deal.PayoutAttemptedAt = sql.NullTime{Time: time.Now(), Valid: true}
	srv, _, payCount := releaseServer(t, "ACCEPTED", "SETTLED", nil)
	svc := releaseService(repo, srv)

	_, err := svc.releaseEscrow(context.Background(), deal.ID, releaseEscrowOptions{
		authorize: func(d *Deal) error { return nil },
		finalize: func(r DealRepository, id string) error {
			return r.UpdateStatus(context.Background(), id, StatusReleased)
		},
	})
	if !errors.Is(err, ErrPayoutInFlight) {
		t.Fatalf("expected ErrPayoutInFlight, got %v", err)
	}
	if atomic.LoadInt32(payCount) != 0 {
		t.Fatal("must not pay when hold not settled")
	}
}

func TestReleaseEscrowPayeeCanceledInvoiceKeepsSettledTracking(t *testing.T) {
	repo := newFakeDealRepo()
	deal := releaseDeal(repo, "deal-r6")
	srv, _, _ := releaseServer(t, "SETTLED", "FAILED", lnbits.ErrPayoutRefused)
	svc := releaseService(repo, srv)

	_, err := svc.releaseEscrow(context.Background(), deal.ID, releaseEscrowOptions{
		authorize: func(d *Deal) error { return nil },
		finalize: func(r DealRepository, id string) error {
			return r.UpdateStatus(context.Background(), id, StatusReleased)
		},
	})
	if !errors.Is(err, ErrPayoutFailed) {
		t.Fatalf("expected ErrPayoutFailed, got %v", err)
	}
	if !deal.PayoutAttemptedAt.Valid {
		t.Fatal("canceled/refused payee invoice after settle must keep attempt marker")
	}
	if deal.Status == StatusReleased {
		t.Fatal("must not release")
	}
}

func TestReleaseEscrowPayoutNotInitiatedAmbiguous(t *testing.T) {
	repo := newFakeDealRepo()
	deal := releaseDeal(repo, "deal-r7")
	deal.PayoutCheckingID = "chk-pay-old"
	deal.PayoutAttemptedAt = sql.NullTime{Time: time.Now(), Valid: true}
	srv, _, payCount := releaseServerWithStatuses(t, "SETTLED", map[string]string{"chk-pay-old": "PENDING"}, "SETTLED", nil)
	svc := releaseService(repo, srv)

	_, err := svc.releaseEscrow(context.Background(), deal.ID, releaseEscrowOptions{
		authorize: func(d *Deal) error { return nil },
		finalize: func(r DealRepository, id string) error {
			return r.UpdateStatus(context.Background(), id, StatusReleased)
		},
	})
	if !errors.Is(err, ErrPayoutInFlight) {
		t.Fatalf("expected ErrPayoutInFlight for not-initiated/ambiguous payout, got %v", err)
	}
	if atomic.LoadInt32(payCount) != 0 {
		t.Fatal("must not re-pay ambiguous payout")
	}
}
