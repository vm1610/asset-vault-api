package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// LocalStorage stores objects as files under a root directory and issues
// HMAC-signed, time-limited download URLs served by the API's own
// /download handler, mirroring the shape of an S3 presigned URL without
// requiring a real object store.
type LocalStorage struct {
	root    string
	baseURL string
	secret  []byte
}

func NewLocalStorage(root, baseURL, secret string) (*LocalStorage, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("storage: creating root dir: %w", err)
	}
	return &LocalStorage{root: root, baseURL: baseURL, secret: []byte(secret)}, nil
}

func (l *LocalStorage) path(key string) (string, error) {
	clean := filepath.Clean("/" + key)
	if clean == "/" {
		return "", fmt.Errorf("storage: invalid key %q", key)
	}
	return filepath.Join(l.root, clean), nil
}

func (l *LocalStorage) Put(ctx context.Context, key string, src io.Reader, size int64, contentType string) error {
	dest, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("storage: creating object dir: %w", err)
	}

	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("storage: creating object: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, src); err != nil {
		return fmt.Errorf("storage: writing object: %w", err)
	}
	return nil
}

func (l *LocalStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: opening object: %w", err)
	}
	return f, nil
}

func (l *LocalStorage) Delete(ctx context.Context, key string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage: deleting object: %w", err)
	}
	return nil
}

func (l *LocalStorage) PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	exp := time.Now().Add(expiry).Unix()
	sig := l.sign(key, exp)

	u, err := url.Parse(l.baseURL)
	if err != nil {
		return "", fmt.Errorf("storage: invalid base url: %w", err)
	}
	u.Path = filepath.Join(u.Path, "download", key)
	q := u.Query()
	q.Set("exp", strconv.FormatInt(exp, 10))
	q.Set("sig", sig)
	u.RawQuery = q.Encode()

	return u.String(), nil
}

// VerifySignedURL checks that a (key, exp, sig) tuple produced by
// PresignGet is authentic and has not expired. It is used by the download
// handler that serves LocalStorage-backed objects directly.
func (l *LocalStorage) VerifySignedURL(key string, exp int64, sig string) bool {
	if time.Now().Unix() > exp {
		return false
	}
	expected := l.sign(key, exp)
	return hmac.Equal([]byte(expected), []byte(sig))
}

func (l *LocalStorage) sign(key string, exp int64) string {
	mac := hmac.New(sha256.New, l.secret)
	mac.Write([]byte(fmt.Sprintf("%s:%d", key, exp)))
	return hex.EncodeToString(mac.Sum(nil))
}
