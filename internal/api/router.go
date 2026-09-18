// Package api wires Gin HTTP handlers to the auth, storage, db, and worker
// packages, exposing the asset upload/browse/download surface consumed by
// the frontend.
package api

import (
	"time"

	"github.com/gin-gonic/gin"

	"asset-vault-api/internal/auth"
	"asset-vault-api/internal/db"
	"asset-vault-api/internal/storage"
	"asset-vault-api/internal/worker"
)

type Deps struct {
	Assets      *db.AssetRepo
	Jobs        *db.JobRepo
	Storage     storage.Storage
	Pool        *worker.Pool
	KeySet      *auth.KeySet
	JWTAudience string
	JWTIssuer   string
	// TempDir stages multipart uploads on disk before streaming them to the
	// configured storage backend, so the full file is never held in memory.
	TempDir string
	// LocalStorage is non-nil only when Storage is backed by the local
	// filesystem, so the download handler can verify signed URLs.
	LocalStorage *storage.LocalStorage
	// PresignExpiry controls how long generated download URLs remain valid.
	PresignExpiry time.Duration
}

func NewRouter(deps Deps) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	h := &handlers{deps: deps}

	if deps.LocalStorage != nil {
		r.GET("/download/*key", h.downloadLocal)
	}

	protected := r.Group("/")
	protected.Use(auth.Middleware(deps.KeySet, deps.JWTAudience, deps.JWTIssuer))
	{
		protected.POST("/assets", h.uploadAsset)
		protected.GET("/assets", h.listAssets)
		protected.GET("/assets/:id", h.getAsset)
		protected.DELETE("/assets/:id", h.deleteAsset)
		protected.GET("/jobs/:id", h.getJob)
	}

	return r
}
