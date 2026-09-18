package storage

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func newTestLocalStorage(t *testing.T) *LocalStorage {
	t.Helper()
	dir := t.TempDir()
	s, err := NewLocalStorage(dir, "http://localhost:8080", "test-secret")
	if err != nil {
		t.Fatalf("creating local storage: %v", err)
	}
	return s
}

func TestLocalStorage_PutGetRoundTrip(t *testing.T) {
	s := newTestLocalStorage(t)
	ctx := context.Background()
	data := []byte("hello asset vault")

	if err := s.Put(ctx, "assets/1/original.txt", bytes.NewReader(data), int64(len(data)), "text/plain"); err != nil {
		t.Fatalf("put: %v", err)
	}

	rc, err := s.Get(ctx, "assets/1/original.txt")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading object: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("round trip mismatch: got %q want %q", got, data)
	}
}

func TestLocalStorage_GetMissingReturnsErrNotFound(t *testing.T) {
	s := newTestLocalStorage(t)
	_, err := s.Get(context.Background(), "does/not/exist.txt")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLocalStorage_Delete(t *testing.T) {
	s := newTestLocalStorage(t)
	ctx := context.Background()
	data := []byte("to be deleted")

	if err := s.Put(ctx, "assets/2/original.txt", bytes.NewReader(data), int64(len(data)), "text/plain"); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := s.Delete(ctx, "assets/2/original.txt"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(ctx, "assets/2/original.txt"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestLocalStorage_PresignGetSignatureValidAndExpires(t *testing.T) {
	s := newTestLocalStorage(t)
	ctx := context.Background()

	signed, err := s.PresignGet(ctx, "assets/3/original.txt", time.Minute)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}

	u, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("parsing presigned url: %v", err)
	}
	exp, err := strconv.ParseInt(u.Query().Get("exp"), 10, 64)
	if err != nil {
		t.Fatalf("parsing exp: %v", err)
	}
	sig := u.Query().Get("sig")

	if !s.VerifySignedURL("assets/3/original.txt", exp, sig) {
		t.Fatal("expected valid signed url to verify")
	}

	if s.VerifySignedURL("assets/3/original.txt", exp, "tampered-signature") {
		t.Fatal("expected tampered signature to fail verification")
	}

	if s.VerifySignedURL("assets/other/original.txt", exp, sig) {
		t.Fatal("expected signature for a different key to fail verification")
	}

	expiredSigned, _ := s.PresignGet(ctx, "assets/3/original.txt", -time.Minute)
	eu, _ := url.Parse(expiredSigned)
	eExp, _ := strconv.ParseInt(eu.Query().Get("exp"), 10, 64)
	eSig := eu.Query().Get("sig")
	if s.VerifySignedURL("assets/3/original.txt", eExp, eSig) {
		t.Fatal("expected expired signed url to fail verification")
	}
}
