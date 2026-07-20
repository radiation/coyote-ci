package githubapp

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
	"time"
)

func TestJWTSigner_SignsRS256WithExpectedClaims(t *testing.T) {
	privateKeyPEM, privateKey := testRSAPrivateKeyPEM(t)
	clock := time.Date(2026, 7, 17, 15, 0, 0, 0, time.UTC)
	signer := NewJWTSigner()
	signer.now = func() time.Time { return clock }

	token, err := signer.Sign("12345", privateKeyPEM)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected jwt with 3 parts, got %d", len(parts))
	}
	var header map[string]string
	decodeJWTPart(t, parts[0], &header)
	if header["alg"] != "RS256" || header["typ"] != "JWT" {
		t.Fatalf("unexpected jwt header: %+v", header)
	}
	var claims struct {
		Iss string `json:"iss"`
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
	}
	decodeJWTPart(t, parts[1], &claims)
	if claims.Iss != "12345" {
		t.Fatalf("expected iss claim 12345, got %v", claims.Iss)
	}
	if got := claims.Iat; got != clock.Add(-jwtIssuedAtSkew).Unix() {
		t.Fatalf("expected iat %d, got %d", clock.Add(-jwtIssuedAtSkew).Unix(), got)
	}
	if got := claims.Exp; got != clock.Add(jwtLifetime).Unix() {
		t.Fatalf("expected exp %d, got %d", clock.Add(jwtLifetime).Unix(), got)
	}
	if err := verifyJWTSignature(token, &privateKey.PublicKey); err != nil {
		t.Fatalf("verify signature: %v", err)
	}
}

func TestJWTSigner_PrivateKeyValidation(t *testing.T) {
	signer := NewJWTSigner()
	if _, err := signer.Sign("", "not-used"); err != ErrAuthentication {
		t.Fatalf("expected blank app id auth error, got %v", err)
	}
	if _, err := signer.Sign("12345", ""); err != ErrPrivateKeyMissing {
		t.Fatalf("expected missing key error, got %v", err)
	}
	if _, err := signer.Sign("12345", "not-pem"); err != ErrPrivateKeyMalformed {
		t.Fatalf("expected malformed key error, got %v", err)
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ec key: %v", err)
	}
	ecDER, err := x509.MarshalPKCS8PrivateKey(ecKey)
	if err != nil {
		t.Fatalf("marshal ec key: %v", err)
	}
	ecPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: ecDER}))
	if _, err := signer.Sign("12345", ecPEM); err != ErrPrivateKeyNotRSA {
		t.Fatalf("expected non-rsa key error, got %v", err)
	}
}

func TestJWTSigner_SupportsPKCS8AndRejectsInvalidSignatures(t *testing.T) {
	pkcs1PEM, privateKey := testRSAPrivateKeyPEM(t)
	parsedKey, parseErr := parseRSAPrivateKey(pkcs1PEM)
	if parseErr != nil {
		t.Fatalf("parse pkcs1 private key: %v", parseErr)
	}
	pkcs8DER, marshalErr := x509.MarshalPKCS8PrivateKey(parsedKey)
	if marshalErr != nil {
		t.Fatalf("marshal pkcs8 private key: %v", marshalErr)
	}
	pkcs8PEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8DER}))
	signer := NewJWTSigner()
	token, signErr := signer.Sign("12345", pkcs8PEM)
	if signErr != nil {
		t.Fatalf("sign pkcs8 jwt: %v", signErr)
	}
	if err := verifyJWTSignature(token, &privateKey.PublicKey); err != nil {
		t.Fatalf("verify pkcs8 signature: %v", err)
	}
	if err := verifyJWTSignature("bad.token", &privateKey.PublicKey); err == nil {
		t.Fatal("expected invalid token verification failure")
	}
	parts := strings.Split(token, ".")
	tamperedClaims := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"tampered"}`))
	tampered := parts[0] + "." + tamperedClaims + "." + parts[2]
	if err := verifyJWTSignature(tampered, &privateKey.PublicKey); err == nil {
		t.Fatal("expected tampered token verification failure")
	}
}

func decodeJWTPart(t *testing.T, value string, dest any) {
	t.Helper()
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		t.Fatalf("decode jwt part: %v", err)
	}
	if err := json.Unmarshal(decoded, dest); err != nil {
		t.Fatalf("unmarshal jwt part: %v", err)
	}
}

func testRSAPrivateKeyPEM(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})), privateKey
}
