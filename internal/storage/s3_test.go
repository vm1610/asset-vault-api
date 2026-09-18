package storage

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// fakeS3Client is an in-memory stand-in for the AWS S3 client, used because
// this environment has no live AWS credentials or bucket to test against.
// It exercises S3Storage's request construction and error translation, not
// the real S3 wire protocol.
type fakeS3Client struct {
	objects map[string][]byte
}

func newFakeS3Client() *fakeS3Client {
	return &fakeS3Client{objects: make(map[string][]byte)}
}

func (f *fakeS3Client) PutObject(ctx context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	data, err := io.ReadAll(in.Body)
	if err != nil {
		return nil, err
	}
	f.objects[aws.ToString(in.Key)] = data
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3Client) GetObject(ctx context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	data, ok := f.objects[aws.ToString(in.Key)]
	if !ok {
		return nil, &smithy.GenericAPIError{Code: "NoSuchKey", Message: "not found"}
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(data))}, nil
}

func (f *fakeS3Client) DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	delete(f.objects, aws.ToString(in.Key))
	return &s3.DeleteObjectOutput{}, nil
}

type fakePresigner struct{}

func (fakePresigner) PresignGetObject(ctx context.Context, in *s3.GetObjectInput, _ ...func(*s3.PresignOptions)) (*v4PresignedHTTPRequest, error) {
	return &v4PresignedHTTPRequest{URL: "https://example-bucket.s3.amazonaws.com/" + aws.ToString(in.Key) + "?X-Amz-Signature=fake"}, nil
}

func newTestS3Storage() (*S3Storage, *fakeS3Client) {
	client := newFakeS3Client()
	return &S3Storage{client: client, presigner: fakePresigner{}, bucket: "test-bucket"}, client
}

func TestS3Storage_PutGetRoundTrip(t *testing.T) {
	s, _ := newTestS3Storage()
	ctx := context.Background()
	data := []byte("s3 object body")

	if err := s.Put(ctx, "assets/1/original.bin", bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
		t.Fatalf("put: %v", err)
	}

	rc, err := s.Get(ctx, "assets/1/original.bin")
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

func TestS3Storage_GetMissingReturnsErrNotFound(t *testing.T) {
	s, _ := newTestS3Storage()
	_, err := s.Get(context.Background(), "does/not/exist.bin")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestS3Storage_Delete(t *testing.T) {
	s, client := newTestS3Storage()
	ctx := context.Background()
	client.objects["assets/2/original.bin"] = []byte("data")

	if err := s.Delete(ctx, "assets/2/original.bin"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := client.objects["assets/2/original.bin"]; ok {
		t.Fatal("expected object to be removed from backing store")
	}
}

func TestS3Storage_PresignGet(t *testing.T) {
	s, _ := newTestS3Storage()
	url, err := s.PresignGet(context.Background(), "assets/3/original.bin", 15*time.Minute)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	if url == "" {
		t.Fatal("expected non-empty presigned url")
	}
}
