package deals

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Amonochuka/ganji-backend/internal/auth"
	"github.com/Amonochuka/ganji-backend/internal/middleware"
)

// newArbitrationRouter mounts the operator-only arbitration routes behind the
// real auth + operator middleware, so these tests exercise the full gate:
// valid token -> is_operator claim -> OperatorRequired.
func newArbitrationRouter(t *testing.T, service *Service, tokens *auth.TokenManager) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()

	group := router.Group("/")
	group.Use(middleware.AuthRequired(tokens))
	group.Use(middleware.OperatorRequired())
	RegisterArbitrationRoutes(group, NewHandler(service))
	return router
}

func bearerToken(t *testing.T, tokens *auth.TokenManager, isOperator bool) string {
	t.Helper()
	token, err := tokens.GenerateAccessToken("operator-1", "arbiter@example.com", isOperator)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return token
}

func TestListDisputesEndpointReturnsQueue(t *testing.T) {
	repo := newFakeDealRepo()
	disputedDeal(repo, "deal-1", "freelancer-1", "client@example.com")
	escrowDeal(repo, "deal-2", "freelancer-1", "client@example.com")

	tokens := auth.NewTokenManager("access-secret", "refresh-secret")
	router := newArbitrationRouter(t, newTestService(repo), tokens)

	req := httptest.NewRequest(http.MethodGet, "/disputes", nil)
	req.Header.Set("Authorization", "Bearer "+bearerToken(t, tokens, true))
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"id":"deal-1"`) {
		t.Errorf("expected the disputed deal in the queue, got %s", resp.Body.String())
	}
	if strings.Contains(resp.Body.String(), `"id":"deal-2"`) {
		t.Error("non-disputed deals must not appear in the queue")
	}
}

func TestResolveDisputeEndpointReleases(t *testing.T) {
	repo := newFakeDealRepo()
	deal := disputedDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	service := NewService(repo, newLNbitsClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"checking_id":"bb"}`))
	}))

	tokens := auth.NewTokenManager("access-secret", "refresh-secret")
	router := newArbitrationRouter(t, service, tokens)

	body := bytes.NewBufferString(`{"resolution":"release"}`)
	req := httptest.NewRequest(http.MethodPost, "/disputes/deal-1/resolve", body)
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

func TestResolveDisputeEndpointRejectsUnknownResolution(t *testing.T) {
	repo := newFakeDealRepo()
	disputedDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	tokens := auth.NewTokenManager("access-secret", "refresh-secret")
	router := newArbitrationRouter(t, newTestService(repo), tokens)

	body := bytes.NewBufferString(`{"resolution":"shrug"}`)
	req := httptest.NewRequest(http.MethodPost, "/disputes/deal-1/resolve", body)
	req.Header.Set("Authorization", "Bearer "+bearerToken(t, tokens, true))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestArbitrationRoutesRejectNonOperator(t *testing.T) {
	repo := newFakeDealRepo()
	disputedDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	tokens := auth.NewTokenManager("access-secret", "refresh-secret")
	router := newArbitrationRouter(t, newTestService(repo), tokens)
	token := bearerToken(t, tokens, false)

	getReq := httptest.NewRequest(http.MethodGet, "/disputes", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getResp := httptest.NewRecorder()
	router.ServeHTTP(getResp, getReq)
	if getResp.Code != http.StatusForbidden {
		t.Errorf("expected non-operator GET /disputes to get 403, got %d", getResp.Code)
	}

	body := bytes.NewBufferString(`{"resolution":"refund"}`)
	postReq := httptest.NewRequest(http.MethodPost, "/disputes/deal-1/resolve", body)
	postReq.Header.Set("Authorization", "Bearer "+token)
	postReq.Header.Set("Content-Type", "application/json")
	postResp := httptest.NewRecorder()
	router.ServeHTTP(postResp, postReq)
	if postResp.Code != http.StatusForbidden {
		t.Errorf("expected non-operator POST resolve to get 403, got %d", postResp.Code)
	}
}

func TestArbitrationRoutesRequireAuth(t *testing.T) {
	repo := newFakeDealRepo()
	disputedDeal(repo, "deal-1", "freelancer-1", "client@example.com")

	tokens := auth.NewTokenManager("access-secret", "refresh-secret")
	router := newArbitrationRouter(t, newTestService(repo), tokens)

	req := httptest.NewRequest(http.MethodGet, "/disputes", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a token, got %d", resp.Code)
	}
}

// jsonRoundTrip is a tiny guard that the resolve request body decodes into the
// handler's request type — catches a renamed/removed JSON field.
func TestResolveDisputeRequestDecodes(t *testing.T) {
	var req resolveDisputeRequest
	if err := json.Unmarshal([]byte(`{"resolution":"refund"}`), &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Resolution != DisputeResolutionRefund {
		t.Fatalf("expected refund, got %q", req.Resolution)
	}
}
