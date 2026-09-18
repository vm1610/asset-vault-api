package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrJobNotFound = errors.New("db: job not found")

type JobRepo struct {
	conn *sql.DB
}

func NewJobRepo(conn *sql.DB) *JobRepo {
	return &JobRepo{conn: conn}
}

func (r *JobRepo) Create(ctx context.Context, j Job) error {
	_, err := r.conn.ExecContext(ctx, `
		INSERT INTO jobs (id, asset_id, status) VALUES (?, ?, ?)`,
		j.ID, j.AssetID, j.Status,
	)
	if err != nil {
		return fmt.Errorf("db: inserting job: %w", err)
	}
	return nil
}

func (r *JobRepo) Get(ctx context.Context, id string) (Job, error) {
	row := r.conn.QueryRowContext(ctx, `
		SELECT id, asset_id, status, error, created_at, updated_at FROM jobs WHERE id = ?`, id)

	var j Job
	if err := row.Scan(&j.ID, &j.AssetID, &j.Status, &j.Error, &j.CreatedAt, &j.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Job{}, ErrJobNotFound
		}
		return Job{}, fmt.Errorf("db: fetching job: %w", err)
	}
	return j, nil
}

func (r *JobRepo) UpdateStatus(ctx context.Context, id string, status JobStatus, jobErr *string) error {
	res, err := r.conn.ExecContext(ctx, `
		UPDATE jobs SET status = ?, error = ?, updated_at = ? WHERE id = ?`,
		status, jobErr, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("db: updating job status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("db: checking update result: %w", err)
	}
	if n == 0 {
		return ErrJobNotFound
	}
	return nil
}
