package deals

import (
	"bytes"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Amonochuka/ganji-backend/internal/auth"
)

func TestReconcileEndpointConfirmsPayoutAndReleases(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusReviewing
	deal.PayoutAttemptedAt = sql.NullTime{Time: time.Now(), Valid: true}

	service := NewService(repo, newLNbitsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"paid":true,"details":{"checking_id":"oc9","status":"COMPLETE"}}`))
	}))

	tokens := auth.NewTokenManager("access-secret", "refresh-secret")
	router := newArbitrationRouter(t, service, tokens)

	body := bytes.NewBufferString(`{"action":"confirm_payout","payout_checking_id":"oc9"}`)
	req := httptest.NewRequest(http.MethodPost, "/deals/deal-1/reconcile", body)
	req.Header.Set("Authorization", "Bearer "+bearerToken(t, tokens, true))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"status":"released"`) {
		t.Errorf("expected released deal in response, got %s", resp.Body.String())
	}
	if repo.deals[deal.ID].Status != StatusReleased {
		t.Fatalf("expected the deal to be released, got %s", repo.deals[deal.ID].Status)
	}
}

func TestReconcileEndpointResetsHungAttempt(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusWorkSubmitted
	deal.PayoutAttemptedAt = sql.NullTime{Time: time.Now(), Valid: true}

	tokens := auth.NewTokenManager("access-secret", "refresh-secret")
	router := newArbitrationRouter(t, newTestService(repo), tokens)

	body := bytes.NewBufferString(`{"action":"reset_payout"}`)
	req := httptest.NewRequest(http.MethodPost, "/deals/deal-1/reconcile", body)
	req.Header.Set("Authorization", "Bearer "+bearerToken(t, tokens, true))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if repo.deals[deal.ID].PayoutAttemptedAt.Valid {
		t.Fatal("expected the payout attempt marker to be cleared")
	}
	if repo.deals[deal.ID].Status != StatusWorkSubmitted {
		t.Fatalf("expected the deal to stay work_submitted, got %s", repo.deals[deal.ID].Status)
	}
}

func TestReconcileEndpointRejectsNonOperator(t *testing.T) {
	repo := newFakeDealRepo()
	escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	tokens := auth.NewTokenManager("access-secret", "refresh-secret")
	router := newArbitrationRouter(t, newTestService(repo), tokens)

	body := bytes.NewBufferString(`{"action":"confirm_payout","payout_checking_id":"oc9"}`)
	req := httptest.NewRequest(http.MethodPost, "/deals/deal-1/reconcile", body)
	req.Header.Set("Authorization", "Bearer "+bearerToken(t, tokens, false))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a non-operator, got %d", resp.Code)
	}
}

func TestReconcileEndpointRejectsUnknownAction(t *testing.T) {
	repo := newFakeDealRepo()
	escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	tokens := auth.NewTokenManager("access-secret", "refresh-secret")
	router := newArbitrationRouter(t, newTestService(repo), tokens)

	body := bytes.NewBufferString(`{"action":"wave_magic_wand"}`)
	req := httptest.NewRequest(http.MethodPost, "/deals/deal-1/reconcile", body)
	req.Header.Set("Authorization", "Bearer "+bearerToken(t, tokens, true))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unknown action, got %d", resp.Code)
	}
}
