package jwt

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Claims struct {
	jwt.RegisteredClaims
	Market string `json:"market,omitempty"`
	Role   string `json:"role,omitempty"`
}

type Manager struct {
	private *rsa.PrivateKey
	public  *rsa.PublicKey
}

func NewManager(privateKeyPEM string) (*Manager, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("jwt: failed to decode PEM block")
	}

	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("jwt: parse PKCS1: %w", err)
		}
		key = k
	case "PRIVATE KEY":
		k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("jwt: parse PKCS8: %w", err)
		}
		var ok bool
		key, ok = k.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("jwt: not an RSA key")
		}
	default:
		return nil, fmt.Errorf("jwt: unknown PEM type %q", block.Type)
	}

	return &Manager{private: key, public: &key.PublicKey}, nil
}

func (m *Manager) IssueAccess(userID, market, role string) (string, string, error) {
	jti := uuid.NewString()
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
		},
		Market: market,
		Role:   role,
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(m.private)
	return tok, jti, err
}

func (m *Manager) IssueRefresh(userID string) (string, string, error) {
	jti := uuid.NewString()
	now := time.Now()
	claims := jwt.RegisteredClaims{
		ID:        jti,
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(7 * 24 * time.Hour)),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(m.private)
	return tok, jti, err
}

func (m *Manager) Verify(tokenStr string) (*Claims, error) {
	tok, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("jwt: unexpected signing method %v", t.Header["alg"])
		}
		return m.public, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := tok.Claims.(*Claims)
	if !ok || !tok.Valid {
		return nil, fmt.Errorf("jwt: invalid token")
	}
	return claims, nil
}
