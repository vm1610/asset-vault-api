package api

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// downloadLocal serves objects for the LocalStorage backend via signed
// URLs produced by storage.PresignGet, verifying the HMAC signature and
// expiry before streaming the object back. This endpoint intentionally
// sits outside the JWT-protected group: the signature in the URL is itself
// the credential, the same trust model a real S3 presigned URL uses.
func (h *handlers) downloadLocal(c *gin.Context) {
	key := strings.TrimPrefix(c.Param("key"), "/")
	expStr := c.Query("exp")
	sig := c.Query("sig")

	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || sig == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing or malformed signature"})
		return
	}

	if !h.deps.LocalStorage.VerifySignedURL(key, exp, sig) {
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid or expired download link"})
		return
	}

	rc, err := h.deps.Storage.Get(c.Request.Context(), key)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "object not found"})
		return
	}
	defer rc.Close()

	c.Status(http.StatusOK)
	io.Copy(c.Writer, rc)
}
