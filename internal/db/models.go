package db

import "time"

type Asset struct {
	ID           string
	OwnerSub     string
	Filename     string
	ContentType  string
	SizeBytes    int64
	StorageKey   string
	ThumbnailKey *string
	CreatedAt    time.Time
}

type JobStatus string

const (
	JobStatusPending    JobStatus = "pending"
	JobStatusProcessing JobStatus = "processing"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
)

type Job struct {
	ID        string
	AssetID   string
	Status    JobStatus
	Error     *string
	CreatedAt time.Time
	UpdatedAt time.Time
}
