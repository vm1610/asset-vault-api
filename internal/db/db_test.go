package db

import (
	"context"
	"path/filepath"
	"testing"
)

func newTestDB(t *testing.T) (*AssetRepo, *JobRepo) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	conn, err := Open(path)
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return NewAssetRepo(conn), NewJobRepo(conn)
}

func TestAssetRepo_CreateGetListDelete(t *testing.T) {
	assets, _ := newTestDB(t)
	ctx := context.Background()

	a := Asset{
		ID:          "asset-1",
		OwnerSub:    "user-1",
		Filename:    "photo.png",
		ContentType: "image/png",
		SizeBytes:   1024,
		StorageKey:  "assets/asset-1/original.png",
	}
	if err := assets.Create(ctx, a); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := assets.Get(ctx, "asset-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Filename != "photo.png" || got.OwnerSub != "user-1" {
		t.Fatalf("unexpected asset: %+v", got)
	}

	if err := assets.SetThumbnailKey(ctx, "asset-1", "assets/asset-1/thumb.png"); err != nil {
		t.Fatalf("set thumbnail key: %v", err)
	}
	got, err = assets.Get(ctx, "asset-1")
	if err != nil {
		t.Fatalf("get after thumbnail update: %v", err)
	}
	if got.ThumbnailKey == nil || *got.ThumbnailKey != "assets/asset-1/thumb.png" {
		t.Fatalf("expected thumbnail key to be set, got %+v", got.ThumbnailKey)
	}

	list, err := assets.ListByOwner(ctx, "user-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 asset, got %d", len(list))
	}

	if err := assets.Delete(ctx, "asset-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := assets.Get(ctx, "asset-1"); err != ErrAssetNotFound {
		t.Fatalf("expected ErrAssetNotFound after delete, got %v", err)
	}
}

func TestAssetRepo_GetMissing(t *testing.T) {
	assets, _ := newTestDB(t)
	if _, err := assets.Get(context.Background(), "does-not-exist"); err != ErrAssetNotFound {
		t.Fatalf("expected ErrAssetNotFound, got %v", err)
	}
}

func TestJobRepo_CreateGetUpdateStatus(t *testing.T) {
	assets, jobs := newTestDB(t)
	ctx := context.Background()

	if err := assets.Create(ctx, Asset{ID: "asset-1", OwnerSub: "user-1", Filename: "a.png", ContentType: "image/png", SizeBytes: 10, StorageKey: "k"}); err != nil {
		t.Fatalf("seed asset: %v", err)
	}

	job := Job{ID: "job-1", AssetID: "asset-1", Status: JobStatusPending}
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	got, err := jobs.Get(ctx, "job-1")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.Status != JobStatusPending {
		t.Fatalf("expected pending status, got %s", got.Status)
	}

	if err := jobs.UpdateStatus(ctx, "job-1", JobStatusCompleted, nil); err != nil {
		t.Fatalf("update status: %v", err)
	}
	got, err = jobs.Get(ctx, "job-1")
	if err != nil {
		t.Fatalf("get job after update: %v", err)
	}
	if got.Status != JobStatusCompleted {
		t.Fatalf("expected completed status, got %s", got.Status)
	}

	errMsg := "thumbnail generation failed: unsupported format"
	if err := jobs.UpdateStatus(ctx, "job-1", JobStatusFailed, &errMsg); err != nil {
		t.Fatalf("update status to failed: %v", err)
	}
	got, err = jobs.Get(ctx, "job-1")
	if err != nil {
		t.Fatalf("get job after failure: %v", err)
	}
	if got.Error == nil || *got.Error != errMsg {
		t.Fatalf("expected error message to persist, got %+v", got.Error)
	}
}

func TestJobRepo_UpdateStatusMissing(t *testing.T) {
	_, jobs := newTestDB(t)
	if err := jobs.UpdateStatus(context.Background(), "no-such-job", JobStatusCompleted, nil); err != ErrJobNotFound {
		t.Fatalf("expected ErrJobNotFound, got %v", err)
	}
}
