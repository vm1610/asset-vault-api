package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const testKid = "test-key-1"

// newTestJWKSServer starts an httptest server that publishes the public half
// of the given RSA key as a JWKS document, and returns the server together
// with a signing function bound to the private key.
func newTestJWKSServer(t *testing.T) (*httptest.Server, *rsa.PrivateKey) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating rsa key: %v", err)
	}

	n := base64.RawURLEncoding.EncodeToString(priv.PublicKey.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big64(priv.PublicKey.E))

	set := jwkSet{Keys: []jwk{{
		Kty: "RSA",
		Kid: testKid,
		Alg: "RS256",
		Use: "sig",
		N:   n,
		E:   e,
	}}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(set)
	}))

	return srv, priv
}

func big64(v int) []byte {
	// Encode a small int (the RSA public exponent) as minimal big-endian bytes.
	if v == 0 {
		return []byte{0}
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte(v & 0xff)}, b...)
		v >>= 8
	}
	return b
}

func signToken(t *testing.T, priv *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("signing token: %v", err)
	}
	return signed
}

func newTestRouter(keySet *KeySet, audience, issuer string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/protected", Middleware(keySet, audience, issuer), func(c *gin.Context) {
		claims, _ := Claims(c)
		c.JSON(http.StatusOK, gin.H{"sub": claims["sub"]})
	})
	return r
}

func TestMiddleware_ValidToken(t *testing.T) {
	srv, priv := newTestJWKSServer(t)
	defer srv.Close()

	keySet := NewKeySet(srv.URL, time.Minute)
	router := newTestRouter(keySet, "", "")

	token := signToken(t, priv, testKid, jwt.MapClaims{
		"sub": "user-123",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMiddleware_ExpiredToken(t *testing.T) {
	srv, priv := newTestJWKSServer(t)
	defer srv.Close()

	keySet := NewKeySet(srv.URL, time.Minute)
	router := newTestRouter(keySet, "", "")

	token := signToken(t, priv, testKid, jwt.MapClaims{
		"sub": "user-123",
		"exp": time.Now().Add(-time.Hour).Unix(),
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for expired token, got %d", w.Code)
	}
}

func TestMiddleware_TamperedSignature(t *testing.T) {
	srv, priv := newTestJWKSServer(t)
	defer srv.Close()

	// A second, unrelated key signs the token, so it will not match the
	// public key published at the JWKS endpoint.
	otherPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating rsa key: %v", err)
	}
	_ = priv

	keySet := NewKeySet(srv.URL, time.Minute)
	router := newTestRouter(keySet, "", "")

	token := signToken(t, otherPriv, testKid, jwt.MapClaims{
		"sub": "user-123",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for tampered signature, got %d", w.Code)
	}
}

func TestMiddleware_UnknownKid(t *testing.T) {
	srv, priv := newTestJWKSServer(t)
	defer srv.Close()

	keySet := NewKeySet(srv.URL, time.Minute)
	router := newTestRouter(keySet, "", "")

	token := signToken(t, priv, "no-such-kid", jwt.MapClaims{
		"sub": "user-123",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unknown kid, got %d", w.Code)
	}
}

func TestMiddleware_MissingHeader(t *testing.T) {
	srv, _ := newTestJWKSServer(t)
	defer srv.Close()

	keySet := NewKeySet(srv.URL, time.Minute)
	router := newTestRouter(keySet, "", "")

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing header, got %d", w.Code)
	}
}

func TestMiddleware_AudienceAndIssuerEnforced(t *testing.T) {
	srv, priv := newTestJWKSServer(t)
	defer srv.Close()

	keySet := NewKeySet(srv.URL, time.Minute)
	router := newTestRouter(keySet, "asset-vault", "https://issuer.example.com")

	wrongAudience := signToken(t, priv, testKid, jwt.MapClaims{
		"sub": "user-123",
		"aud": "other-service",
		"iss": "https://issuer.example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+wrongAudience)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong audience, got %d", w.Code)
	}

	valid := signToken(t, priv, testKid, jwt.MapClaims{
		"sub": "user-123",
		"aud": "asset-vault",
		"iss": "https://issuer.example.com",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	req2 := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req2.Header.Set("Authorization", "Bearer "+valid)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 for matching audience/issuer, got %d: %s", w2.Code, w2.Body.String())
	}
}
