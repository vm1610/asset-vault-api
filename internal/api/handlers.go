package api

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"asset-vault-api/internal/auth"
	"asset-vault-api/internal/db"
	"asset-vault-api/internal/worker"
)

type handlers struct {
	deps Deps
}

func subjectFromContext(c *gin.Context) (string, bool) {
	claims, ok := auth.Claims(c)
	if !ok {
		return "", false
	}
	sub, ok := claims["sub"].(string)
	return sub, ok && sub != ""
}

// uploadAsset accepts a multipart/form-data request with a single "file"
// part, stages it to a temp file (so the full upload is never buffered in
// memory), streams it into the configured storage backend, records the
// asset, and enqueues a background thumbnail job.
func (h *handlers) uploadAsset(c *gin.Context) {
	owner, ok := subjectFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing subject claim"})
		return
	}

	mr, err := c.Request.MultipartReader()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expected multipart/form-data request"})
		return
	}

	var part *multipart.Part
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "malformed multipart body"})
			return
		}
		if p.FormName() == "file" {
			part = p
			break
		}
		p.Close()
	}
	if part == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing \"file\" part"})
		return
	}
	defer part.Close()

	staged, size, err := stageUpload(h.deps.TempDir, part)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to stage upload"})
		return
	}
	defer os.Remove(staged)

	f, err := os.Open(staged)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read staged upload"})
		return
	}
	defer f.Close()

	assetID := uuid.NewString()
	contentType := part.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	storageKey := fmt.Sprintf("assets/%s/original", assetID)

	if err := h.deps.Storage.Put(c.Request.Context(), storageKey, f, size, contentType); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store asset"})
		return
	}

	asset := db.Asset{
		ID:          assetID,
		OwnerSub:    owner,
		Filename:    part.FileName(),
		ContentType: contentType,
		SizeBytes:   size,
		StorageKey:  storageKey,
	}
	if err := h.deps.Assets.Create(c.Request.Context(), asset); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record asset"})
		return
	}

	jobID := uuid.NewString()
	if err := h.deps.Jobs.Create(c.Request.Context(), db.Job{ID: jobID, AssetID: assetID, Status: db.JobStatusPending}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to schedule processing"})
		return
	}
	h.deps.Pool.Enqueue(worker.Task{AssetID: assetID, JobID: jobID})

	c.JSON(http.StatusCreated, gin.H{
		"asset_id": assetID,
		"job_id":   jobID,
	})
}

func stageUpload(dir string, r io.Reader) (path string, size int64, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, err
	}
	tmp, err := os.CreateTemp(dir, "upload-*")
	if err != nil {
		return "", 0, err
	}
	defer tmp.Close()

	n, err := io.Copy(tmp, r)
	if err != nil {
		os.Remove(tmp.Name())
		return "", 0, err
	}
	return tmp.Name(), n, nil
}

func (h *handlers) listAssets(c *gin.Context) {
	owner, ok := subjectFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing subject claim"})
		return
	}

	assets, err := h.deps.Assets.ListByOwner(c.Request.Context(), owner)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list assets"})
		return
	}

	out := make([]gin.H, 0, len(assets))
	for _, a := range assets {
		out = append(out, assetSummary(a))
	}
	c.JSON(http.StatusOK, gin.H{"assets": out})
}

func (h *handlers) getAsset(c *gin.Context) {
	owner, ok := subjectFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing subject claim"})
		return
	}

	asset, err := h.deps.Assets.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "asset not found"})
		return
	}
	if asset.OwnerSub != owner {
		c.JSON(http.StatusNotFound, gin.H{"error": "asset not found"})
		return
	}

	body := assetSummary(asset)

	downloadURL, err := h.deps.Storage.PresignGet(c.Request.Context(), asset.StorageKey, h.deps.PresignExpiry)
	if err == nil {
		body["download_url"] = downloadURL
	}
	if asset.ThumbnailKey != nil {
		if thumbURL, err := h.deps.Storage.PresignGet(c.Request.Context(), *asset.ThumbnailKey, h.deps.PresignExpiry); err == nil {
			body["thumbnail_url"] = thumbURL
		}
	}

	c.JSON(http.StatusOK, body)
}

func (h *handlers) deleteAsset(c *gin.Context) {
	owner, ok := subjectFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing subject claim"})
		return
	}

	asset, err := h.deps.Assets.Get(c.Request.Context(), c.Param("id"))
	if err != nil || asset.OwnerSub != owner {
		c.JSON(http.StatusNotFound, gin.H{"error": "asset not found"})
		return
	}

	_ = h.deps.Storage.Delete(c.Request.Context(), asset.StorageKey)
	if asset.ThumbnailKey != nil {
		_ = h.deps.Storage.Delete(c.Request.Context(), *asset.ThumbnailKey)
	}
	if err := h.deps.Assets.Delete(c.Request.Context(), asset.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete asset"})
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *handlers) getJob(c *gin.Context) {
	owner, ok := subjectFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing subject claim"})
		return
	}

	job, err := h.deps.Jobs.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	asset, err := h.deps.Assets.Get(c.Request.Context(), job.AssetID)
	if err != nil || asset.OwnerSub != owner {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"job_id":   job.ID,
		"asset_id": job.AssetID,
		"status":   job.Status,
		"error":    job.Error,
	})
}

func assetSummary(a db.Asset) gin.H {
	return gin.H{
		"id":            a.ID,
		"filename":      a.Filename,
		"content_type":  a.ContentType,
		"size_bytes":    a.SizeBytes,
		"has_thumbnail": a.ThumbnailKey != nil,
		"created_at":    a.CreatedAt,
	}
}
