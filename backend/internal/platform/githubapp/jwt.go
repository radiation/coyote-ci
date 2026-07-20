package githubapp

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"
	"time"
)

const (
	jwtIssuedAtSkew = time.Minute
	jwtLifetime     = 9 * time.Minute
)

type JWTSigner struct {
	now func() time.Time
}

func NewJWTSigner() *JWTSigner {
	return &JWTSigner{now: time.Now}
}

func (s *JWTSigner) Sign(appID string, privateKeyPEM string) (string, error) {
	trimmedAppID := strings.TrimSpace(appID)
	if trimmedAppID == "" {
		return "", ErrAuthentication
	}
	privateKey, err := parseRSAPrivateKey(privateKeyPEM)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	if s != nil && s.now != nil {
		now = s.now().UTC()
	}
	issuedAt := now.Add(-jwtIssuedAtSkew).Unix()
	expiresAt := now.Add(jwtLifetime).Unix()

	headerBytes, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	claimsBytes, err := json.Marshal(map[string]any{"iss": trimmedAppID, "iat": issuedAt, "exp": expiresAt})
	if err != nil {
		return "", err
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(headerBytes)
	encodedClaims := base64.RawURLEncoding.EncodeToString(claimsBytes)
	signingInput := encodedHeader + "." + encodedClaims
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func parseRSAPrivateKey(privateKeyPEM string) (*rsa.PrivateKey, error) {
	trimmed := strings.TrimSpace(privateKeyPEM)
	if trimmed == "" {
		return nil, ErrPrivateKeyMissing
	}
	block, _ := pem.Decode([]byte(trimmed))
	if block == nil {
		return nil, ErrPrivateKeyMalformed
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, ErrPrivateKeyMalformed
	}
	rsaKey, ok := parsedKey.(*rsa.PrivateKey)
	if !ok {
		return nil, ErrPrivateKeyNotRSA
	}
	return rsaKey, nil
}

func verifyJWTSignature(token string, publicKey *rsa.PublicKey) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return errors.New("invalid token")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	return rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature)
}
