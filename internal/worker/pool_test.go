package worker

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"asset-vault-api/internal/db"
	"asset-vault-api/internal/storage"
)

func setupProcessorEnv(t *testing.T) (*db.AssetRepo, *db.JobRepo, *storage.LocalStorage) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("opening db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	store, err := storage.NewLocalStorage(t.TempDir(), "http://localhost:8080", "secret")
	if err != nil {
		t.Fatalf("creating storage: %v", err)
	}

	return db.NewAssetRepo(conn), db.NewJobRepo(conn), store
}

func TestThumbnailProcessor_GeneratesThumbnailAndCompletesJob(t *testing.T) {
	assets, jobs, store := setupProcessorEnv(t)
	ctx := context.Background()

	src := makeTestJPEG(t, 640, 480)
	if err := store.Put(ctx, "assets/a1/original.jpg", bytes.NewReader(src), int64(len(src)), "image/jpeg"); err != nil {
		t.Fatalf("seeding original object: %v", err)
	}
	if err := assets.Create(ctx, db.Asset{
		ID: "a1", OwnerSub: "user-1", Filename: "photo.jpg",
		ContentType: "image/jpeg", SizeBytes: int64(len(src)), StorageKey: "assets/a1/original.jpg",
	}); err != nil {
		t.Fatalf("seeding asset: %v", err)
	}
	if err := jobs.Create(ctx, db.Job{ID: "job-1", AssetID: "a1", Status: db.JobStatusPending}); err != nil {
		t.Fatalf("seeding job: %v", err)
	}

	processor := &ThumbnailProcessor{Storage: store, Assets: assets, Jobs: jobs}
	pool := NewPool(processor)
	pool.Start(ctx, 2)
	pool.Enqueue(Task{AssetID: "a1", JobID: "job-1"})
	pool.Stop()

	job, err := jobs.Get(ctx, "job-1")
	if err != nil {
		t.Fatalf("fetching job: %v", err)
	}
	if job.Status != db.JobStatusCompleted {
		t.Fatalf("expected job completed, got %s (error: %v)", job.Status, job.Error)
	}

	asset, err := assets.Get(ctx, "a1")
	if err != nil {
		t.Fatalf("fetching asset: %v", err)
	}
	if asset.ThumbnailKey == nil {
		t.Fatal("expected thumbnail key to be set")
	}

	rc, err := store.Get(ctx, *asset.ThumbnailKey)
	if err != nil {
		t.Fatalf("fetching stored thumbnail: %v", err)
	}
	defer rc.Close()
}

func TestThumbnailProcessor_MissingAssetMarksJobFailed(t *testing.T) {
	assets, jobs, store := setupProcessorEnv(t)
	ctx := context.Background()

	if err := jobs.Create(ctx, db.Job{ID: "job-2", AssetID: "does-not-exist", Status: db.JobStatusPending}); err != nil {
		t.Fatalf("seeding job: %v", err)
	}

	processor := &ThumbnailProcessor{Storage: store, Assets: assets, Jobs: jobs}
	pool := NewPool(processor)
	pool.Start(ctx, 1)
	pool.Enqueue(Task{AssetID: "does-not-exist", JobID: "job-2"})
	pool.Stop()

	job, err := jobs.Get(ctx, "job-2")
	if err != nil {
		t.Fatalf("fetching job: %v", err)
	}
	if job.Status != db.JobStatusFailed {
		t.Fatalf("expected job failed, got %s", job.Status)
	}
	if job.Error == nil {
		t.Fatal("expected job error message to be set")
	}
}

func TestPool_StopWaitsForInFlightWork(t *testing.T) {
	done := make(chan struct{})
	proc := processorFunc(func(ctx context.Context, task Task) {
		time.Sleep(20 * time.Millisecond)
		close(done)
	})

	pool := NewPool(proc)
	pool.Start(context.Background(), 1)
	pool.Enqueue(Task{AssetID: "x", JobID: "y"})
	pool.Stop()

	select {
	case <-done:
	default:
		t.Fatal("expected task to complete before Stop returned")
	}
}

type processorFunc func(ctx context.Context, task Task)

func (f processorFunc) Process(ctx context.Context, task Task) { f(ctx, task) }
