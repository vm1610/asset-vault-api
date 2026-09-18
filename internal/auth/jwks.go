// Package auth validates bearer tokens against a JSON Web Key Set (JWKS)
// and exposes a Gin middleware that enforces authentication on protected routes.
package auth

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"
)

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwkSet struct {
	Keys []jwk `json:"keys"`
}

// KeySet fetches and caches RSA public keys published at a JWKS endpoint,
// keyed by "kid" so tokens can be validated against the correct signing key.
type KeySet struct {
	url        string
	ttl        time.Duration
	httpClient *http.Client

	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

func NewKeySet(url string, ttl time.Duration) *KeySet {
	return &KeySet{
		url:        url,
		ttl:        ttl,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		keys:       make(map[string]*rsa.PublicKey),
	}
}

// Key returns the RSA public key for the given kid, refreshing the cached
// set from the JWKS endpoint if it is stale or the kid is unknown.
func (k *KeySet) Key(kid string) (*rsa.PublicKey, error) {
	k.mu.RLock()
	key, ok := k.keys[kid]
	stale := time.Since(k.fetchedAt) > k.ttl
	k.mu.RUnlock()

	if ok && !stale {
		return key, nil
	}

	if err := k.refresh(); err != nil {
		if ok {
			// Serve the stale key rather than fail a valid request outright
			// because the JWKS endpoint is temporarily unreachable.
			return key, nil
		}
		return nil, err
	}

	k.mu.RLock()
	defer k.mu.RUnlock()
	key, ok = k.keys[kid]
	if !ok {
		return nil, fmt.Errorf("auth: no key found for kid %q", kid)
	}
	return key, nil
}

func (k *KeySet) refresh() error {
	resp, err := k.httpClient.Get(k.url)
	if err != nil {
		return fmt.Errorf("auth: fetching jwks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth: jwks endpoint returned status %d", resp.StatusCode)
	}

	var set jwkSet
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return fmt.Errorf("auth: decoding jwks: %w", err)
	}

	parsed := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, key := range set.Keys {
		if key.Kty != "RSA" {
			continue
		}
		pub, err := parseRSAPublicKey(key.N, key.E)
		if err != nil {
			continue
		}
		parsed[key.Kid] = pub
	}

	k.mu.Lock()
	k.keys = parsed
	k.fetchedAt = time.Now()
	k.mu.Unlock()

	return nil
}

func parseRSAPublicKey(nEnc, eEnc string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nEnc)
	if err != nil {
		return nil, fmt.Errorf("decoding modulus: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eEnc)
	if err != nil {
		return nil, fmt.Errorf("decoding exponent: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	return &rsa.PublicKey{
		N: n,
		E: int(e.Int64()),
	}, nil
}
