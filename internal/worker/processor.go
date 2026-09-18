package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"

	"asset-vault-api/internal/db"
	"asset-vault-api/internal/storage"
)

// ThumbnailProcessor generates and stores a thumbnail for an uploaded asset,
// updating the corresponding job row as it progresses.
type ThumbnailProcessor struct {
	Storage storage.Storage
	Assets  *db.AssetRepo
	Jobs    *db.JobRepo
}

func (p *ThumbnailProcessor) Process(ctx context.Context, task Task) {
	if err := p.Jobs.UpdateStatus(ctx, task.JobID, db.JobStatusProcessing, nil); err != nil {
		log.Printf("worker: marking job %s processing: %v", task.JobID, err)
		return
	}

	if err := p.process(ctx, task); err != nil {
		msg := err.Error()
		if uerr := p.Jobs.UpdateStatus(ctx, task.JobID, db.JobStatusFailed, &msg); uerr != nil {
			log.Printf("worker: marking job %s failed: %v", task.JobID, uerr)
		}
		return
	}

	if err := p.Jobs.UpdateStatus(ctx, task.JobID, db.JobStatusCompleted, nil); err != nil {
		log.Printf("worker: marking job %s completed: %v", task.JobID, err)
	}
}

func (p *ThumbnailProcessor) process(ctx context.Context, task Task) error {
	asset, err := p.Assets.Get(ctx, task.AssetID)
	if err != nil {
		return fmt.Errorf("loading asset: %w", err)
	}

	src, err := p.Storage.Get(ctx, asset.StorageKey)
	if err != nil {
		return fmt.Errorf("fetching original object: %w", err)
	}
	defer src.Close()

	thumb, err := GenerateThumbnail(src, asset.ContentType)
	if err != nil {
		if errors.Is(err, ErrUnsupportedFormat) {
			// Not every asset is thumbnailable (e.g. raw video uploads);
			// this is an expected outcome, not a failure of the job itself.
			return nil
		}
		return fmt.Errorf("generating thumbnail: %w", err)
	}

	thumbKey := asset.StorageKey + ".thumb.jpg"
	if err := p.Storage.Put(ctx, thumbKey, bytes.NewReader(thumb), int64(len(thumb)), "image/jpeg"); err != nil {
		return fmt.Errorf("storing thumbnail: %w", err)
	}

	if err := p.Assets.SetThumbnailKey(ctx, task.AssetID, thumbKey); err != nil {
		return fmt.Errorf("recording thumbnail key: %w", err)
	}

	return nil
}
