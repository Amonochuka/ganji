package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Amonochuka/ganji-backend/internal/auth"
)

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
