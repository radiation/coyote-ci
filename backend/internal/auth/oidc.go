package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var ErrOIDCEmailRequired = errors.New("oidc email claim is required")
var ErrOIDCEmailNotVerified = errors.New("oidc email claim is not verified")

type OIDCConfig struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

type OIDCIdentity struct {
	Email       string
	DisplayName *string
}

type OIDCAuthenticator interface {
	AuthCodeURL(state string, nonce string) string
	Exchange(ctx context.Context, code string, nonce string) (OIDCIdentity, error)
}

type CoreOSOIDCAuthenticator struct {
	oauthConfig oauth2.Config
	verifier    *oidc.IDTokenVerifier
}

type oidcClaims struct {
	EmailVerified     *bool  `json:"email_verified"`
	Email             string `json:"email"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
}

func NewOIDCAuthenticator(ctx context.Context, cfg OIDCConfig) (*CoreOSOIDCAuthenticator, error) {
	issuerURL := strings.TrimSpace(cfg.IssuerURL)
	clientID := strings.TrimSpace(cfg.ClientID)
	clientSecret := strings.TrimSpace(cfg.ClientSecret)
	redirectURL := strings.TrimSpace(cfg.RedirectURL)
	if issuerURL == "" || clientID == "" || clientSecret == "" || redirectURL == "" {
		return nil, errors.New("OIDC_ISSUER_URL, OIDC_CLIENT_ID, OIDC_CLIENT_SECRET, and OIDC_REDIRECT_URL are required")
	}

	provider, providerErr := oidc.NewProvider(ctx, issuerURL)
	if providerErr != nil {
		return nil, providerErr
	}

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = DefaultOIDCScopes()
	}

	return &CoreOSOIDCAuthenticator{
		oauthConfig: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       scopes,
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: clientID}),
	}, nil
}

func DefaultOIDCScopes() []string {
	return []string{oidc.ScopeOpenID, "email", "profile"}
}

func ParseOIDCScopes(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return DefaultOIDCScopes()
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return DefaultOIDCScopes()
	}
	return fields
}

func (a *CoreOSOIDCAuthenticator) AuthCodeURL(state string, nonce string) string {
	return a.oauthConfig.AuthCodeURL(state, oidc.Nonce(nonce))
}

func (a *CoreOSOIDCAuthenticator) Exchange(ctx context.Context, code string, nonce string) (OIDCIdentity, error) {
	token, exchangeErr := a.oauthConfig.Exchange(ctx, strings.TrimSpace(code))
	if exchangeErr != nil {
		return OIDCIdentity{}, exchangeErr
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || strings.TrimSpace(rawIDToken) == "" {
		return OIDCIdentity{}, errors.New("oidc provider did not return an id_token")
	}
	idToken, verifyErr := a.verifier.Verify(ctx, rawIDToken)
	if verifyErr != nil {
		return OIDCIdentity{}, verifyErr
	}
	if idToken.Nonce != strings.TrimSpace(nonce) {
		return OIDCIdentity{}, errors.New("oidc nonce mismatch")
	}

	var claims oidcClaims
	if claimsErr := idToken.Claims(&claims); claimsErr != nil {
		return OIDCIdentity{}, claimsErr
	}
	return identityFromOIDCClaims(claims)
}

func identityFromOIDCClaims(claims oidcClaims) (OIDCIdentity, error) {
	email := strings.TrimSpace(claims.Email)
	if email == "" {
		return OIDCIdentity{}, ErrOIDCEmailRequired
	}
	if claims.EmailVerified == nil || !*claims.EmailVerified {
		return OIDCIdentity{}, ErrOIDCEmailNotVerified
	}

	var displayName *string
	if name := strings.TrimSpace(claims.Name); name != "" {
		displayName = &name
	} else if username := strings.TrimSpace(claims.PreferredUsername); username != "" {
		displayName = &username
	}

	return OIDCIdentity{Email: email, DisplayName: displayName}, nil
}
