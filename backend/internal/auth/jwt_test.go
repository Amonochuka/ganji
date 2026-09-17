package auth

import "testing"

func TestAccessTokenCarriesOperatorFlag(t *testing.T) {
	tokens := NewTokenManager("access-secret", "refresh-secret")

	operator, err := tokens.GenerateAccessToken("user-1", "arbiter@example.com", true)
	if err != nil {
		t.Fatalf("generate operator token: %v", err)
	}
	regular, err := tokens.GenerateAccessToken("user-2", "client@example.com", false)
	if err != nil {
		t.Fatalf("generate regular token: %v", err)
	}

	operatorClaims, err := tokens.VerifyAccessToken(operator)
	if err != nil {
		t.Fatalf("verify operator token: %v", err)
	}
	if !operatorClaims.IsOperator {
		t.Error("expected operator token to carry is_operator=true")
	}

	regularClaims, err := tokens.VerifyAccessToken(regular)
	if err != nil {
		t.Fatalf("verify regular token: %v", err)
	}
	if regularClaims.IsOperator {
		t.Error("expected regular token to carry is_operator=false")
	}
}
