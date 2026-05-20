package jwt

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

func genPKCS1PEM(t *testing.T) string {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa gen: %v", err)
	}
	der := x509.MarshalPKCS1PrivateKey(k)
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}))
}

func genPKCS8PEM(t *testing.T) string {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa gen: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatalf("pkcs8 marshal: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func TestNewManager_PKCS1(t *testing.T) {
	if _, err := NewManager(genPKCS1PEM(t)); err != nil {
		t.Fatalf("PKCS1 should load: %v", err)
	}
}

func TestNewManager_PKCS8(t *testing.T) {
	if _, err := NewManager(genPKCS8PEM(t)); err != nil {
		t.Fatalf("PKCS8 should load: %v", err)
	}
}

func TestNewManager_BadPEM(t *testing.T) {
	if _, err := NewManager("not a pem"); err == nil {
		t.Fatal("expected error on garbage PEM")
	}
}

func TestNewManager_UnknownType(t *testing.T) {
	bad := string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: []byte("xx")}))
	if _, err := NewManager(bad); err == nil {
		t.Fatal("expected error on unknown PEM type")
	}
}

func TestIssueAccess_VerifyRoundTrip(t *testing.T) {
	m, err := NewManager(genPKCS1PEM(t))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	tok, jti, err := m.IssueAccess("user-1", "italy", "editor")
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}
	if jti == "" {
		t.Fatal("jti must be non-empty")
	}
	claims, err := m.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "user-1" {
		t.Errorf("subject = %q", claims.Subject)
	}
	if claims.Market != "italy" {
		t.Errorf("market = %q", claims.Market)
	}
	if claims.Role != "editor" {
		t.Errorf("role = %q", claims.Role)
	}
	if claims.ID != jti {
		t.Errorf("jti mismatch: claim=%q issued=%q", claims.ID, jti)
	}
}

func TestIssueRefresh_VerifyRoundTrip(t *testing.T) {
	m, err := NewManager(genPKCS1PEM(t))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	tok, jti, err := m.IssueRefresh("user-2")
	if err != nil {
		t.Fatalf("IssueRefresh: %v", err)
	}
	claims, err := m.Verify(tok)
	if err != nil {
		t.Fatalf("Verify refresh: %v", err)
	}
	if claims.Subject != "user-2" {
		t.Errorf("subject = %q", claims.Subject)
	}
	if claims.ID != jti {
		t.Errorf("jti mismatch")
	}
	if claims.Market != "" || claims.Role != "" {
		t.Errorf("refresh should not carry market/role: %+v", claims)
	}
}

func TestVerify_WrongKey(t *testing.T) {
	a, _ := NewManager(genPKCS1PEM(t))
	b, _ := NewManager(genPKCS1PEM(t))
	tok, _, _ := a.IssueAccess("u", "italy", "editor")
	if _, err := b.Verify(tok); err == nil {
		t.Fatal("token signed by A must fail verify under B")
	}
}

func TestVerify_Malformed(t *testing.T) {
	m, _ := NewManager(genPKCS1PEM(t))
	if _, err := m.Verify("not.a.jwt"); err == nil {
		t.Fatal("expected error on malformed token")
	}
}

func TestVerify_WrongAlg(t *testing.T) {
	m, _ := NewManager(genPKCS1PEM(t))
	claims := Claims{RegisteredClaims: gojwt.RegisteredClaims{
		Subject:   "u",
		ExpiresAt: gojwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
	}}
	tok, err := gojwt.NewWithClaims(gojwt.SigningMethodHS256, claims).SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("sign HS256: %v", err)
	}
	if _, err := m.Verify(tok); err == nil {
		t.Fatal("HS256 token must be rejected (RS256 expected)")
	}
}

func TestVerify_NoneAlg(t *testing.T) {
	m, _ := NewManager(genPKCS1PEM(t))
	claims := Claims{RegisteredClaims: gojwt.RegisteredClaims{Subject: "u"}}
	tok, err := gojwt.NewWithClaims(gojwt.SigningMethodNone, claims).SignedString(gojwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none: %v", err)
	}
	if _, err := m.Verify(tok); err == nil {
		t.Fatal("alg=none must be rejected")
	}
}

func TestVerify_Expired(t *testing.T) {
	m, _ := NewManager(genPKCS1PEM(t))

	// Build expired claims and sign with manager's private key.
	claims := Claims{RegisteredClaims: gojwt.RegisteredClaims{
		Subject:   "u",
		IssuedAt:  gojwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
		ExpiresAt: gojwt.NewNumericDate(time.Now().Add(-1 * time.Minute)),
	}}
	expiredTok := signWithManager(t, m, claims)
	if _, err := m.Verify(expiredTok); err == nil {
		t.Fatal("expired token must be rejected")
	}

	// Sanity: a fresh token issued by the same manager still verifies.
	tok, _, err := m.IssueAccess("u", "italy", "editor")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := m.Verify(tok); err != nil {
		t.Fatalf("fresh token rejected: %v", err)
	}
}

// signWithManager helps tests sign arbitrary claims using the manager's private key.
// Lives in test file (not production) — exposed via unexported field accessor.
func signWithManager(t *testing.T, m *Manager, c Claims) string {
	t.Helper()
	tok, err := gojwt.NewWithClaims(gojwt.SigningMethodRS256, c).SignedString(m.private)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return tok
}

func TestIssueAccess_ExpiresIn15min(t *testing.T) {
	m, _ := NewManager(genPKCS1PEM(t))
	before := time.Now()
	tok, _, err := m.IssueAccess("u", "italy", "editor")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	claims, err := m.Verify(tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	delta := claims.ExpiresAt.Sub(before)
	if delta < 14*time.Minute || delta > 16*time.Minute {
		t.Errorf("access exp delta = %v, want ~15min", delta)
	}
}

func TestIssueRefresh_ExpiresIn7days(t *testing.T) {
	m, _ := NewManager(genPKCS1PEM(t))
	before := time.Now()
	tok, _, err := m.IssueRefresh("u")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	claims, err := m.Verify(tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	delta := claims.ExpiresAt.Sub(before)
	want := 7 * 24 * time.Hour
	if delta < want-time.Minute || delta > want+time.Minute {
		t.Errorf("refresh exp delta = %v, want ~7d", delta)
	}
}

func TestIssueAccess_UniqueJTI(t *testing.T) {
	m, _ := NewManager(genPKCS1PEM(t))
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		_, jti, err := m.IssueAccess("u", "italy", "editor")
		if err != nil {
			t.Fatalf("issue: %v", err)
		}
		if seen[jti] {
			t.Fatalf("duplicate jti at i=%d: %s", i, jti)
		}
		seen[jti] = true
	}
}

func TestNewManager_MalformedPKCS8(t *testing.T) {
	// Garbage bytes inside a PRIVATE KEY block: ParsePKCS8PrivateKey fails
	// before the RSA-type check. Exercises the malformed-PKCS8 error path.
	bad := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("garbage")}))
	if _, err := NewManager(bad); err == nil {
		t.Fatal("expected error on malformed PKCS8")
	}
}
