package deals

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Amonochuka/ganji-backend/internal/auth"
)

func TestReconcileEndpointRejectsNonOperator(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	ms := func() []Milestone {
		var out []Milestone
		for _, m := range repo.milestones {
			if m.DealID == deal.ID {
				out = append(out, *m)
			}
		}
		return out
	}()
	if len(ms) == 0 {
		t.Fatal("no milestones")
	}

	tokens := auth.NewTokenManager("access-secret", "refresh-secret")
	router := newArbitrationRouter(t, newTestService(repo), tokens)

	body := bytes.NewBufferString(`{"action":"confirm_payout","payout_checking_id":"oc9"}`)
	req := httptest.NewRequest(http.MethodPost, "/deals/deal-1/milestones/"+ms[0].ID+"/reconcile", body)
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
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	ms := func() []Milestone {
		var out []Milestone
		for _, m := range repo.milestones {
			if m.DealID == deal.ID {
				out = append(out, *m)
			}
		}
		return out
	}()
	if len(ms) == 0 {
		t.Fatal("no milestones")
	}

	tokens := auth.NewTokenManager("access-secret", "refresh-secret")
	router := newArbitrationRouter(t, newTestService(repo), tokens)

	body := bytes.NewBufferString(`{"action":"wave_magic_wand"}`)
	req := httptest.NewRequest(http.MethodPost, "/deals/deal-1/milestones/"+ms[0].ID+"/reconcile", body)
	req.Header.Set("Authorization", "Bearer "+bearerToken(t, tokens, true))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unknown action, got %d", resp.Code)
	}
}
