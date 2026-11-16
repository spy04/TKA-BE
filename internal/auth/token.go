package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const tokenIssuer = "binasiswa"

type tokenManager struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

type tokenClaims struct {
	Subject   string `json:"sub"`
	Issuer    string `json:"iss"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

func newTokenManager(secret string, ttl time.Duration) tokenManager {
	return tokenManager{secret: []byte(secret), ttl: ttl, now: time.Now}
}

func (m tokenManager) issue(subject string) (string, time.Time, error) {
	now := m.now().UTC()
	expiresAt := now.Add(m.ttl)
	header, err := encodeSegment([]byte(`{"alg":"HS256","typ":"JWT"}`))
	if err != nil {
		return "", time.Time{}, err
	}
	claims := tokenClaims{
		Subject:   subject,
		Issuer:    tokenIssuer,
		IssuedAt:  now.Unix(),
		ExpiresAt: expiresAt.Unix(),
	}
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("encode access token claims: %w", err)
	}
	payload, err := encodeSegment(payloadBytes)
	if err != nil {
		return "", time.Time{}, err
	}
	unsigned := header + "." + payload
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(unsigned))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return unsigned + "." + signature, expiresAt, nil
}

func (m tokenManager) verify(token string) (tokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return tokenClaims{}, errors.New("invalid token")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || string(headerBytes) != `{"alg":"HS256","typ":"JWT"}` {
		return tokenClaims{}, errors.New("invalid token")
	}
	providedSignature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return tokenClaims{}, errors.New("invalid token")
	}
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(providedSignature, mac.Sum(nil)) {
		return tokenClaims{}, errors.New("invalid token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return tokenClaims{}, errors.New("invalid token")
	}
	var claims tokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return tokenClaims{}, errors.New("invalid token")
	}
	now := m.now().UTC().Unix()
	if claims.Subject == "" || claims.Issuer != tokenIssuer || claims.IssuedAt > now || claims.ExpiresAt <= now {
		return tokenClaims{}, errors.New("invalid token")
	}
	return claims, nil
}

func newRefreshToken() (string, string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(value)
	hash := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(hash[:]), nil
}

func encodeSegment(value []byte) (string, error) {
	return base64.RawURLEncoding.EncodeToString(value), nil
}
