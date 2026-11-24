package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Amonochuka/ganji-backend/internal/auth"
)

type fakeAccessTokenRevoker struct {
	revoked map[string]bool
}

func (f *fakeAccessTokenRevoker) RevokeAccessToken(_ context.Context, jti, _ string, _ time.Time) error {
	f.revoked[jti] = true
	return nil
}

func (f *fakeAccessTokenRevoker) IsAccessTokenRevoked(_ context.Context, jti string) (bool, error) {
	return f.revoked[jti], nil
}

func newAuthTestRouter(tokens *auth.TokenManager, operatorOnly bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	group := router.Group("/")
	group.Use(AuthRequired(tokens))
	if operatorOnly {
		group.Use(OperatorRequired())
	}
	group.GET("/whoami", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"is_operator": c.GetBool("isOperator")})
	})
	return router
}

func doAuthedGet(t *testing.T, router *gin.Engine, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}

func TestAuthRequiredCarriesOperatorClaim(t *testing.T) {
	tokens := auth.NewTokenManager("access-secret", "refresh-secret")

	operator, err := tokens.GenerateAccessToken("user-1", "arbiter@example.com", true)
	if err != nil {
		t.Fatalf("generate operator token: %v", err)
	}
	regular, err := tokens.GenerateAccessToken("user-2", "client@example.com", false)
	if err != nil {
		t.Fatalf("generate regular token: %v", err)
	}

	router := newAuthTestRouter(tokens, false)

	if resp := doAuthedGet(t, router, operator); resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"is_operator":true`) {
		t.Errorf("expected operator flag true, got %d: %s", resp.Code, resp.Body.String())
	}
	if resp := doAuthedGet(t, router, regular); resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"is_operator":false`) {
		t.Errorf("expected operator flag false, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestAuthRequiredRejectsRevokedAccessToken(t *testing.T) {
	tokens := auth.NewTokenManager("access-secret", "refresh-secret")
	revoker := &fakeAccessTokenRevoker{revoked: map[string]bool{}}

	token, err := tokens.GenerateAccessToken("user-1", "client@example.com", false)
	if err != nil {
		t.Fatalf("generate access token: %v", err)
	}
	claims, err := tokens.VerifyAccessToken(token)
	if err != nil {
		t.Fatalf("verify access token: %v", err)
	}
	if err := revoker.RevokeAccessToken(context.Background(), claims.ID, claims.UserID, time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("revoke access token: %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(AuthRequired(tokens, revoker))
	router.GET("/whoami", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected revoked token to be rejected, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestOperatorRequiredAllowsOperatorRejectsOthers(t *testing.T) {
	tokens := auth.NewTokenManager("access-secret", "refresh-secret")

	operator, _ := tokens.GenerateAccessToken("user-1", "arbiter@example.com", true)
	regular, _ := tokens.GenerateAccessToken("user-2", "client@example.com", false)

	router := newAuthTestRouter(tokens, true)

	if resp := doAuthedGet(t, router, operator); resp.Code != http.StatusOK {
		t.Errorf("expected operator to pass, got %d: %s", resp.Code, resp.Body.String())
	}
	if resp := doAuthedGet(t, router, regular); resp.Code != http.StatusForbidden {
		t.Errorf("expected non-operator to get 403, got %d: %s", resp.Code, resp.Body.String())
	}
}
