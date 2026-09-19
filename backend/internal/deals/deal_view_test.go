package deals

import (
	"database/sql"
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
	deal.PayoutCheckingID = "payout-cc"
	deal.PayoutAttemptedAt = sql.NullTime{Time: time.Now(), Valid: true}

	service := newTestService(repo)
	router := newTestDealRouter(t, service, "client-user", "client@example.com")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/deals/deal-1", nil))

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()

	for _, secret := range []string{`"preimage"`, `"payee_invoice"`, `"payout_checking_id"`, `"payout_attempted_at"`} {
		if strings.Contains(body, secret) {
			t.Errorf("deal response leaks %q: %s", secret, body)
		}
	}
	for _, field := range []string{`"id":"deal-1"`, `"client_email":"client@example.com"`, `"status":"released"`, `"preimage_hash":"bb"`, `"share_token":"share-deal-1"`} {
		if !strings.Contains(body, field) {
			t.Errorf("deal response missing %q: %s", field, body)
		}
	}
}

// TestListDealsResponsesRedactSecrets covers the list endpoint, which must map
// every deal through the same redacted view.
func TestListDealsResponsesRedactSecrets(t *testing.T) {
	repo := newFakeDealRepo()
	deal := escrowDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	deal.PayoutCheckingID = "payout-cc"

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
