package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrAssetNotFound = errors.New("db: asset not found")

type AssetRepo struct {
	conn *sql.DB
}

func NewAssetRepo(conn *sql.DB) *AssetRepo {
	return &AssetRepo{conn: conn}
}

func (r *AssetRepo) Create(ctx context.Context, a Asset) error {
	_, err := r.conn.ExecContext(ctx, `
		INSERT INTO assets (id, owner_sub, filename, content_type, size_bytes, storage_key, thumbnail_key)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.OwnerSub, a.Filename, a.ContentType, a.SizeBytes, a.StorageKey, a.ThumbnailKey,
	)
	if err != nil {
		return fmt.Errorf("db: inserting asset: %w", err)
	}
	return nil
}

func (r *AssetRepo) Get(ctx context.Context, id string) (Asset, error) {
	row := r.conn.QueryRowContext(ctx, `
		SELECT id, owner_sub, filename, content_type, size_bytes, storage_key, thumbnail_key, created_at
		FROM assets WHERE id = ?`, id)

	var a Asset
	if err := row.Scan(&a.ID, &a.OwnerSub, &a.Filename, &a.ContentType, &a.SizeBytes, &a.StorageKey, &a.ThumbnailKey, &a.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Asset{}, ErrAssetNotFound
		}
		return Asset{}, fmt.Errorf("db: fetching asset: %w", err)
	}
	return a, nil
}

func (r *AssetRepo) ListByOwner(ctx context.Context, ownerSub string) ([]Asset, error) {
	rows, err := r.conn.QueryContext(ctx, `
		SELECT id, owner_sub, filename, content_type, size_bytes, storage_key, thumbnail_key, created_at
		FROM assets WHERE owner_sub = ? ORDER BY created_at DESC`, ownerSub)
	if err != nil {
		return nil, fmt.Errorf("db: listing assets: %w", err)
	}
	defer rows.Close()

	var assets []Asset
	for rows.Next() {
		var a Asset
		if err := rows.Scan(&a.ID, &a.OwnerSub, &a.Filename, &a.ContentType, &a.SizeBytes, &a.StorageKey, &a.ThumbnailKey, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("db: scanning asset row: %w", err)
		}
		assets = append(assets, a)
	}
	return assets, rows.Err()
}

func (r *AssetRepo) SetThumbnailKey(ctx context.Context, id, thumbnailKey string) error {
	res, err := r.conn.ExecContext(ctx, `UPDATE assets SET thumbnail_key = ? WHERE id = ?`, thumbnailKey, id)
	if err != nil {
		return fmt.Errorf("db: setting thumbnail key: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("db: checking update result: %w", err)
	}
	if n == 0 {
		return ErrAssetNotFound
	}
	return nil
}

func (r *AssetRepo) Delete(ctx context.Context, id string) error {
	res, err := r.conn.ExecContext(ctx, `DELETE FROM assets WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("db: deleting asset: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("db: checking delete result: %w", err)
	}
	if n == 0 {
		return ErrAssetNotFound
	}
	return nil
}
