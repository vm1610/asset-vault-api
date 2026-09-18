package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const claimsContextKey = "auth.claims"

// Middleware returns a Gin handler that requires a valid RS256 bearer token
// signed by a key published in the configured JWKS. When audience/issuer are
// non-empty, tokens are additionally required to match them.
func Middleware(keySet *KeySet, audience, issuer string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString, err := extractBearerToken(c.GetHeader("Authorization"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}

		claims := jwt.MapClaims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
			if t.Method.Alg() != jwt.SigningMethodRS256.Alg() {
				return nil, fmt.Errorf("auth: unsupported signing method %q", t.Method.Alg())
			}
			kid, ok := t.Header["kid"].(string)
			if !ok || kid == "" {
				return nil, errMissingKid
			}
			return keySet.Key(kid)
		}, jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}))

		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}

		if audience != "" {
			aud, _ := claims.GetAudience()
			if !containsString(aud, audience) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token audience mismatch"})
				return
			}
		}
		if issuer != "" {
			iss, _ := claims.GetIssuer()
			if iss != issuer {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token issuer mismatch"})
				return
			}
		}

		c.Set(claimsContextKey, claims)
		c.Next()
	}
}

// Claims returns the validated claims stored on the request context by Middleware.
func Claims(c *gin.Context) (jwt.MapClaims, bool) {
	v, ok := c.Get(claimsContextKey)
	if !ok {
		return nil, false
	}
	claims, ok := v.(jwt.MapClaims)
	return claims, ok
}

var errMissingKid = errors.New("auth: token header missing kid")

func containsString(list []string, target string) bool {
	for _, v := range list {
		if v == target {
			return true
		}
	}
	return false
}

func extractBearerToken(header string) (string, error) {
	if header == "" {
		return "", errors.New("missing authorization header")
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", errors.New("authorization header must be a bearer token")
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", errors.New("empty bearer token")
	}
	return token, nil
}
