// Command mockjwks runs a minimal, non-production JWKS issuer for local
// development and manual testing of asset-vault-api. It generates an RSA
// keypair on startup, publishes the public key as a JWKS document, and can
// mint signed tokens against it. It is not an OIDC provider and performs no
// authentication of its own -- anyone who can reach it can mint a token for
// any subject. Never point a real deployment's JWKS_URL at this.
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const kid = "mockjwks-dev-key"

func main() {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatalf("generating rsa key: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		n := base64.RawURLEncoding.EncodeToString(priv.PublicKey.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(encodeExponent(priv.PublicKey.E))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{"kty": "RSA", "kid": kid, "alg": "RS256", "use": "sig", "n": n, "e": e}},
		})
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		sub := r.URL.Query().Get("sub")
		if sub == "" {
			sub = "dev-user"
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"sub": sub,
			"iat": time.Now().Unix(),
			"exp": time.Now().Add(time.Hour).Unix(),
		})
		token.Header["kid"] = kid
		signed, err := token.SignedString(priv)
		if err != nil {
			http.Error(w, "failed to sign token", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"access_token": signed})
	})

	port := os.Getenv("MOCKJWKS_PORT")
	if port == "" {
		port = "9091"
	}
	log.Printf("mockjwks listening on :%s (dev-only, do not use in production)", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

func encodeExponent(v int) []byte {
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
