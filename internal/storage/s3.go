package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// s3API is the subset of the AWS SDK S3 client that S3Storage depends on.
// Narrowing to an interface lets tests substitute a fake client instead of
// making real network calls to AWS.
type s3API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// s3Presigner is the subset of the AWS SDK presign client S3Storage depends
// on for generating presigned download URLs.
type s3Presigner interface {
	PresignGetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*v4PresignedHTTPRequest, error)
}

// v4PresignedHTTPRequest mirrors the fields of
// github.com/aws/aws-sdk-go-v2/aws/signer/v4.PresignedHTTPRequest that this
// package uses, so the presigner interface above does not need to import
// the signer package directly.
type v4PresignedHTTPRequest struct {
	URL string
}

// S3Storage stores objects in an S3 (or S3-compatible) bucket.
type S3Storage struct {
	client    s3API
	presigner s3Presigner
	bucket    string
}

// NewS3Storage builds an S3Storage backed by the real AWS SDK v2 client,
// loading credentials from the standard AWS environment/config chain. It
// optionally targets a custom endpoint for S3-compatible services.
func NewS3Storage(ctx context.Context, bucket, region, endpoint string) (*S3Storage, error) {
	if bucket == "" {
		return nil, errors.New("storage: S3_BUCKET must be set to use the s3 backend")
	}

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("storage: loading aws config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})
	presignClient := s3.NewPresignClient(client)

	return &S3Storage{
		client:    client,
		presigner: &realPresigner{presignClient},
		bucket:    bucket,
	}, nil
}

// realPresigner adapts *s3.PresignClient to the s3Presigner interface.
type realPresigner struct {
	client *s3.PresignClient
}

func (p *realPresigner) PresignGetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*v4PresignedHTTPRequest, error) {
	req, err := p.client.PresignGetObject(ctx, params, optFns...)
	if err != nil {
		return nil, err
	}
	return &v4PresignedHTTPRequest{URL: req.URL}, nil
}

func (s *S3Storage) Put(ctx context.Context, key string, src io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          src,
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("storage: s3 put object: %w", err)
	}
	return nil
}

func (s *S3Storage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NoSuchKey" || apiErr.ErrorCode() == "NotFound") {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: s3 get object: %w", err)
	}
	return out.Body, nil
}

func (s *S3Storage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("storage: s3 delete object: %w", err)
	}
	return nil
}

func (s *S3Storage) PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	req, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, func(po *s3.PresignOptions) {
		po.Expires = expiry
	})
	if err != nil {
		return "", fmt.Errorf("storage: presigning s3 url: %w", err)
	}
	return req.URL, nil
}
