package auth

import "testing"

func TestAccessTokenCarriesJTI(t *testing.T) {
	tokens := NewTokenManager("access-secret", "refresh-secret")

	first, err := tokens.GenerateAccessToken("user-1", "client@example.com", false)
	if err != nil {
		t.Fatalf("generate first access token: %v", err)
	}
	second, err := tokens.GenerateAccessToken("user-1", "client@example.com", false)
	if err != nil {
		t.Fatalf("generate second access token: %v", err)
	}

	firstClaims, err := tokens.VerifyAccessToken(first)
	if err != nil {
		t.Fatalf("verify first access token: %v", err)
	}
	secondClaims, err := tokens.VerifyAccessToken(second)
	if err != nil {
		t.Fatalf("verify second access token: %v", err)
	}
	if firstClaims.ID == "" || secondClaims.ID == "" {
		t.Fatal("expected access tokens to carry a jti")
	}
	if firstClaims.ID == secondClaims.ID {
		t.Fatal("expected access token jtis to differ")
	}
}

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

// Refresh tokens must be unique even when minted in the same second: the DB
// enforces UNIQUE on the token hash, so identical tokens turn a concurrent
// login into a 500.
func TestRefreshTokensAreUniqueWithinSameSecond(t *testing.T) {
	tokens := NewTokenManager("access-secret", "refresh-secret")

	first, err := tokens.GenerateRefreshToken("user-1", "client@example.com")
	if err != nil {
		t.Fatalf("generate first refresh token: %v", err)
	}
	second, err := tokens.GenerateRefreshToken("user-1", "client@example.com")
	if err != nil {
		t.Fatalf("generate second refresh token: %v", err)
	}

	if first == second {
		t.Fatal("expected two refresh tokens issued in the same second to differ")
	}

	claims, err := tokens.VerifyRefreshToken(first)
	if err != nil {
		t.Fatalf("verify refresh token: %v", err)
	}
	if claims.ID == "" {
		t.Error("expected refresh token to carry a jti")
	}
}
