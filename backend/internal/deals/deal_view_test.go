package deals

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// newTestDealRouter mounts the authenticated deal routes behind a stub auth
// context, mirroring what middleware.AuthRequired sets for a real request.
func newTestDealRouter(t *testing.T, service *Service, userID, email string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()

	protected := router.Group("/")
	protected.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Set("email", email)
		c.Next()
	})
	RegisterRoutes(protected, NewHandler(service))
	return router
}

// TestDealResponsesRedactSecrets is the regression guard for the audit finding
// (explained.md §10 #1): the raw escrow preimage, the freelancer's payee
// invoice, and the payout-tracking fields must never appear in an authed deal
// response. It hits GET /deals/:id as the client — the party the docs' threat
// model says must not see them.
func TestDealResponsesRedactSecrets(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.Status = StatusReleased

	service := newTestService(repo)
	router := newTestDealRouter(t, service, "client-user", "client@example.com")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/deals/deal-1", nil))

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()

	for _, secret := range []string{`"preimage"`, `"payee_invoice"`, `"payout_checking_id"`} {
		if strings.Contains(body, secret) {
			t.Errorf("deal response leaks %q: %s", secret, body)
		}
	}
	for _, field := range []string{`"id":"deal-1"`, `"client_email":"client@example.com"`, `"preimage_hash":"bb"`, `"share_token":"share-deal-1"`} {
		if !strings.Contains(body, field) {
			t.Errorf("deal response missing %q: %s", field, body)
		}
	}
}

// TestListDealsResponsesRedactSecrets covers the list endpoint, which must map
// every deal through the same redacted view.
func TestRespondPublicDealErrorMapsPayoutFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		err        error
		wantStatus int
		wantBody   string
	}{
		{fmt.Errorf("%w: paying freelancer for milestone was refused", ErrPayoutFailed), http.StatusBadGateway, "paying freelancer"},
		{fmt.Errorf("%w: verifying milestone payout", ErrPayoutInFlight), http.StatusConflict, "verifying milestone payout"},
	}
	for _, tt := range tests {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)

		respondPublicDealError(c, tt.err, "public accept-next")

		if recorder.Code != tt.wantStatus {
			t.Fatalf("expected status %d, got %d: %s", tt.wantStatus, recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(recorder.Body.String(), tt.wantBody) {
			t.Fatalf("expected body to contain %q, got %s", tt.wantBody, recorder.Body.String())
		}
	}
}

func newTestPublicRouter(t *testing.T, service *Service, email string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", "client-user")
		c.Set("email", email)
		c.Next()
	})
	RegisterPublicRoutes(router, NewHandler(service))
	return router
}

func TestPublicMoneyActionsRejectWrongClientEmail(t *testing.T) {
	repo := newFakeDealRepo()
	escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	service := newTestService(repo)
	router := newTestPublicRouter(t, service, "intruder@example.com")

	routes := []string{
		"/public/deals/share-deal-1/approve",
		"/public/deals/share-deal-1/payout/reset",
		"/public/deals/share-deal-1/milestones/deal-1-m1/accept-next",
		"/public/deals/share-deal-1/milestones/deal-1-m1/reject",
		"/public/deals/share-deal-1/exit",
	}
	for _, route := range routes {
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, route, nil))
		if resp.Code != http.StatusForbidden {
			t.Errorf("%s expected 403 for wrong client email, got %d: %s", route, resp.Code, resp.Body.String())
		}
	}
}

func TestFreelancerDealDetailIncludesPayeeInvoice(t *testing.T) {
	repo := newFakeDealRepo()
	escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	service := newTestService(repo)
	router := newTestDealRouter(t, service, "freelancer-1", "freelancer-1@example.com")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/deals/deal-1", nil))

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"payee_invoice":"lnbc5000payee"`) {
		t.Fatalf("freelancer detail response missing payee invoice: %s", resp.Body.String())
	}
}

func TestListDealsResponsesRedactSecrets(t *testing.T) {
	repo := newFakeDealRepo()
	escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	service := newTestService(repo)
	router := newTestDealRouter(t, service, "freelancer-1", "freelancer-1@example.com")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/deals", nil))

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()

	if !strings.Contains(body, `"id":"deal-1"`) {
		t.Errorf("expected the deal in the list, got %s", body)
	}
	for _, secret := range []string{`"preimage"`, `"payee_invoice"`, `"payout_checking_id"`} {
		if strings.Contains(body, secret) {
			t.Errorf("list response leaks %q: %s", secret, body)
		}
	}
}

func TestDealDetailSerializesPayoutResetAvailable(t *testing.T) {
	repo := newFakeDealRepo()
	escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	m := repo.milestones["deal-1-m1"]
	m.PayoutAttemptedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
	repo.milestones[m.ID] = m

	service := newTestService(repo)
	router := newTestDealRouter(t, service, "client-user", "client@example.com")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/deals/deal-1", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Deal struct {
			PayoutResetAvailable bool `json:"payout_reset_available"`
			Milestones           []struct {
				PayoutResetAvailable bool `json:"payout_reset_available"`
			} `json:"milestones"`
		} `json:"deal"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Deal.PayoutResetAvailable {
		t.Fatalf("deal response missing top-level payout_reset_available: %s", resp.Body.String())
	}
	if len(payload.Deal.Milestones) != 1 || !payload.Deal.Milestones[0].PayoutResetAvailable {
		t.Fatalf("milestone response missing payout_reset_available: %s", resp.Body.String())
	}
}
