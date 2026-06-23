package oauthissuer

import (
	"errors"
	"testing"
	"time"
	"truckapi/pkg/config"

	"github.com/golang-jwt/jwt/v5"
)

// blankIssuerEnv ensures the issuer reads as "not configured" even when .env
// values leaked into config.Env at startup.
func blankIssuerEnv(t *testing.T) {
	t.Helper()
	origID := config.Env["CALLBACK_OAUTH_CLIENT_ID"]
	origSecret := config.Env["CALLBACK_OAUTH_CLIENT_SECRET"]
	origJWT := config.Env["CALLBACK_OAUTH_JWT_SECRET"]
	config.Env["CALLBACK_OAUTH_CLIENT_ID"] = ""
	config.Env["CALLBACK_OAUTH_CLIENT_SECRET"] = ""
	config.Env["CALLBACK_OAUTH_JWT_SECRET"] = ""
	t.Setenv("CALLBACK_OAUTH_CLIENT_ID", "")
	t.Setenv("CALLBACK_OAUTH_CLIENT_SECRET", "")
	t.Setenv("CALLBACK_OAUTH_JWT_SECRET", "")
	t.Cleanup(func() {
		config.Env["CALLBACK_OAUTH_CLIENT_ID"] = origID
		config.Env["CALLBACK_OAUTH_CLIENT_SECRET"] = origSecret
		config.Env["CALLBACK_OAUTH_JWT_SECRET"] = origJWT
	})
}

func setIssuerEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CALLBACK_OAUTH_CLIENT_ID", "test-client")
	t.Setenv("CALLBACK_OAUTH_CLIENT_SECRET", "test-secret")
	t.Setenv("CALLBACK_OAUTH_JWT_SECRET", "test-signing-key-must-be-long-enough")
	t.Setenv("CALLBACK_OAUTH_TOKEN_TTL_SECONDS", "3600")
}

func TestIssueAndValidateToken_RoundTrip(t *testing.T) {
	setIssuerEnv(t)

	resp, err := IssueToken("test-client", "test-secret")
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if resp.TokenType != "Bearer" {
		t.Fatalf("TokenType=%q, want Bearer", resp.TokenType)
	}
	if resp.ExpiresIn != 3600 {
		t.Fatalf("ExpiresIn=%d, want 3600", resp.ExpiresIn)
	}
	if resp.AccessToken == "" {
		t.Fatal("AccessToken empty")
	}

	if err := ValidateToken(resp.AccessToken); err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
}

func TestIssueToken_InvalidClient(t *testing.T) {
	setIssuerEnv(t)

	_, err := IssueToken("test-client", "WRONG")
	if !errors.Is(err, ErrInvalidClient) {
		t.Fatalf("wrong secret: got %v, want ErrInvalidClient", err)
	}

	_, err = IssueToken("WRONG", "test-secret")
	if !errors.Is(err, ErrInvalidClient) {
		t.Fatalf("wrong id: got %v, want ErrInvalidClient", err)
	}
}

func TestIssueToken_NotConfigured(t *testing.T) {
	blankIssuerEnv(t)

	_, err := IssueToken("any", "any")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("got %v, want ErrNotConfigured", err)
	}
}

func TestValidateToken_TamperedSignature(t *testing.T) {
	setIssuerEnv(t)
	resp, err := IssueToken("test-client", "test-secret")
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	// Flip the last char of the signature.
	tampered := resp.AccessToken[:len(resp.AccessToken)-1] + "A"
	if tampered == resp.AccessToken {
		tampered = resp.AccessToken[:len(resp.AccessToken)-1] + "B"
	}
	if err := ValidateToken(tampered); err == nil {
		t.Fatal("expected error for tampered token")
	}
}

func TestValidateToken_ExpiredJWT(t *testing.T) {
	setIssuerEnv(t)

	// Manually sign a JWT that's already expired with the same secret.
	key, _ := signingKey()
	expired := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    tokenIssuer,
		Subject:   "test-client",
		Audience:  jwt.ClaimStrings{tokenAudience},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		NotBefore: jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
	})
	signed, _ := expired.SignedString(key)

	if err := ValidateToken(signed); err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestValidateToken_WrongIssuer(t *testing.T) {
	setIssuerEnv(t)

	key, _ := signingKey()
	wrongIssuer := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    "someone-else",
		Subject:   "test-client",
		Audience:  jwt.ClaimStrings{tokenAudience},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		NotBefore: jwt.NewNumericDate(time.Now()),
	})
	signed, _ := wrongIssuer.SignedString(key)

	if err := ValidateToken(signed); err == nil {
		t.Fatal("expected error for wrong issuer")
	}
}

func TestValidateToken_WrongAudience(t *testing.T) {
	setIssuerEnv(t)

	key, _ := signingKey()
	wrongAud := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    tokenIssuer,
		Subject:   "test-client",
		Audience:  jwt.ClaimStrings{"not-truckapi-callback"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		NotBefore: jwt.NewNumericDate(time.Now()),
	})
	signed, _ := wrongAud.SignedString(key)

	if err := ValidateToken(signed); err == nil {
		t.Fatal("expected error for wrong audience")
	}
}

func TestValidateToken_RejectsNoneAlg(t *testing.T) {
	setIssuerEnv(t)

	// An unsigned token (alg=none) should be rejected even if claims are valid.
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.RegisteredClaims{
		Issuer:    tokenIssuer,
		Audience:  jwt.ClaimStrings{tokenAudience},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})
	signed, _ := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)

	if err := ValidateToken(signed); err == nil {
		t.Fatal("expected error rejecting alg=none token")
	}
}

func TestJWTSigningConfigured(t *testing.T) {
	blankIssuerEnv(t)
	if JWTSigningConfigured() {
		t.Fatal("expected false when JWT secret unset")
	}
	// Now set a value — t.Setenv with non-empty IS picked up by GetEnv,
	// no Env-map dance needed.
	t.Setenv("CALLBACK_OAUTH_JWT_SECRET", "anything")
	if !JWTSigningConfigured() {
		t.Fatal("expected true when JWT secret set")
	}
}
