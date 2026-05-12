package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestNewOIDCAuthenticator_RequiresCompleteConfig(t *testing.T) {
	_, err := NewOIDCAuthenticator(context.Background(), OIDCConfig{})
	if err == nil || err.Error() != "OIDC_ISSUER_URL, OIDC_CLIENT_ID, OIDC_CLIENT_SECRET, and OIDC_REDIRECT_URL are required" {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestDefaultAndParsedOIDCScopes(t *testing.T) {
	defaults := DefaultOIDCScopes()
	if !reflect.DeepEqual(defaults, []string{"openid", "email", "profile"}) {
		t.Fatalf("unexpected default scopes: %v", defaults)
	}

	parsed := ParseOIDCScopes(" profile email custom-scope ")
	if !reflect.DeepEqual(parsed, []string{"profile", "email", "custom-scope"}) {
		t.Fatalf("unexpected parsed scopes: %v", parsed)
	}

	if !reflect.DeepEqual(ParseOIDCScopes("   "), defaults) {
		t.Fatalf("expected blank scopes to use defaults")
	}
}

func TestCoreOSOIDCAuthenticator_AuthCodeURLIncludesNonce(t *testing.T) {
	authenticator := &CoreOSOIDCAuthenticator{
		oauthConfig: oauth2.Config{
			ClientID:    "client-id",
			RedirectURL: "https://coyote.example.com/auth/callback",
			Endpoint: oauth2.Endpoint{
				AuthURL: "https://issuer.example.com/auth",
			},
			Scopes: DefaultOIDCScopes(),
		},
	}

	rawURL := authenticator.AuthCodeURL("state-1", "nonce-1")
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse auth code url failed: %v", err)
	}
	query := parsed.Query()
	if got := query.Get("state"); got != "state-1" {
		t.Fatalf("expected state, got %q", got)
	}
	if got := query.Get("nonce"); got != "nonce-1" {
		t.Fatalf("expected nonce, got %q", got)
	}
	if got := query.Get("client_id"); got != "client-id" {
		t.Fatalf("expected client id, got %q", got)
	}
	if got := query.Get("scope"); got != "openid email profile" {
		t.Fatalf("expected default scopes, got %q", got)
	}
}

func TestCoreOSOIDCAuthenticator_Exchange(t *testing.T) {
	issuer, closeServer := newTestOIDCProvider(t, true)
	defer closeServer()

	authenticator, err := NewOIDCAuthenticator(context.Background(), OIDCConfig{
		IssuerURL:    issuer,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "https://coyote.example.com/auth/callback",
	})
	if err != nil {
		t.Fatalf("create oidc authenticator failed: %v", err)
	}

	identity, err := authenticator.Exchange(context.Background(), "code-1", "nonce-1")
	if err != nil {
		t.Fatalf("exchange failed: %v", err)
	}
	if identity.Email != "user@example.com" {
		t.Fatalf("expected email user@example.com, got %q", identity.Email)
	}
	if identity.DisplayName == nil || *identity.DisplayName != "OIDC User" {
		t.Fatalf("expected display name OIDC User, got %v", identity.DisplayName)
	}
}

func TestCoreOSOIDCAuthenticator_ExchangeRequiresIDToken(t *testing.T) {
	issuer, closeServer := newTestOIDCProvider(t, false)
	defer closeServer()

	authenticator, err := NewOIDCAuthenticator(context.Background(), OIDCConfig{
		IssuerURL:    issuer,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "https://coyote.example.com/auth/callback",
	})
	if err != nil {
		t.Fatalf("create oidc authenticator failed: %v", err)
	}

	_, err = authenticator.Exchange(context.Background(), "code-1", "nonce-1")
	if err == nil || !strings.Contains(err.Error(), "id_token") {
		t.Fatalf("expected missing id_token error, got %v", err)
	}
}

func newTestOIDCProvider(t *testing.T, includeIDToken bool) (string, func()) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key failed: %v", err)
	}
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeJSON(t, w, map[string]any{
				"issuer":                 serverURL,
				"authorization_endpoint": serverURL + "/auth",
				"token_endpoint":         serverURL + "/token",
				"jwks_uri":               serverURL + "/keys",
			})
		case "/keys":
			writeJSON(t, w, map[string]any{
				"keys": []map[string]any{{
					"kty": "RSA",
					"kid": "test-key",
					"alg": "RS256",
					"use": "sig",
					"n":   base64.RawURLEncoding.EncodeToString(privateKey.N.Bytes()),
					"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.E)).Bytes()),
				}},
			})
		case "/token":
			response := map[string]any{
				"access_token": "access-token",
				"token_type":   "Bearer",
			}
			if includeIDToken {
				response["id_token"] = signIDToken(t, privateKey, map[string]any{
					"iss":            serverURL,
					"sub":            "user-1",
					"aud":            "client-id",
					"exp":            time.Now().Add(time.Hour).Unix(),
					"iat":            time.Now().Add(-time.Minute).Unix(),
					"nonce":          "nonce-1",
					"email":          "user@example.com",
					"email_verified": true,
					"name":           "OIDC User",
				})
			}
			writeJSON(t, w, response)
		default:
			http.NotFound(w, r)
		}
	}))
	serverURL = server.URL
	return server.URL, server.Close
}

func signIDToken(t *testing.T, privateKey *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "RS256", "kid": "test-key", "typ": "JWT"})
	if err != nil {
		t.Fatalf("marshal header failed: %v", err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims failed: %v", err)
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	unsigned := encodedHeader + "." + encodedPayload
	hash := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatalf("sign jwt failed: %v", err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, fmt.Sprintf("encode json failed: %v", err), http.StatusInternalServerError)
	}
}
