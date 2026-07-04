package storage

import (
	"context"
	"errors"
	"io"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3Options struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Bucket          string
	Region          string
	Secure          bool
	PathStyle       bool
}

type S3Storage struct {
	client *minio.Client
	bucket string
}

func NewS3Storage(opts S3Options) (*S3Storage, error) {
	if strings.TrimSpace(opts.Endpoint) == "" {
		return nil, errors.New("s3 endpoint is required")
	}
	if strings.TrimSpace(opts.Bucket) == "" {
		return nil, errors.New("s3 bucket is required")
	}

	endpoint, secure, err := normalizeEndpoint(opts.Endpoint, opts.Secure)
	if err != nil {
		return nil, err
	}

	options := &minio.Options{
		Creds:  credentials.NewStaticV4(opts.AccessKeyID, opts.SecretAccessKey, opts.SessionToken),
		Secure: secure,
		Region: opts.Region,
	}
	if opts.PathStyle {
		options.BucketLookup = minio.BucketLookupPath
	}
	client, err := minio.New(endpoint, options)
	if err != nil {
		return nil, err
	}

	return &S3Storage{client: client, bucket: opts.Bucket}, nil
}

func normalizeEndpoint(endpoint string, secure bool) (string, bool, error) {
	endpoint = strings.TrimSpace(endpoint)
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", false, err
	}
	if parsed.Scheme == "" {
		return endpoint, secure, nil
	}
	switch parsed.Scheme {
	case "http":
		return parsed.Host, false, nil
	case "https":
		return parsed.Host, true, nil
	default:
		return "", false, errors.New("unsupported s3 endpoint scheme")
	}
}

func NewS3StorageWithClient(client *minio.Client, bucket string) (*S3Storage, error) {
	if client == nil {
		return nil, errors.New("s3 client is required")
	}
	if strings.TrimSpace(bucket) == "" {
		return nil, errors.New("s3 bucket is required")
	}
	return &S3Storage{client: client, bucket: bucket}, nil
}

func (s *S3Storage) Put(ctx context.Context, object Object) (ObjectInfo, error) {
	if strings.TrimSpace(object.Key) == "" {
		return ObjectInfo{}, errors.New("object key is required")
	}
	if object.Body == nil {
		return ObjectInfo{}, errors.New("object body is required")
	}

	contentType := object.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	info, err := s.client.PutObject(ctx, s.bucket, object.Key, object.Body, object.Size, minio.PutObjectOptions{
		ContentType:  contentType,
		UserMetadata: object.Metadata,
	})
	if err != nil {
		return ObjectInfo{}, err
	}

	return ObjectInfo{
		Key:         object.Key,
		ContentType: contentType,
		Size:        info.Size,
		Metadata:    object.Metadata,
		UpdatedAt:   info.LastModified,
	}, nil
}

func (s *S3Storage) Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, ObjectInfo{}, err
	}

	stat, err := object.Stat()
	if err != nil {
		_ = object.Close()
		return nil, ObjectInfo{}, err
	}

	return object, objectInfoFromMinIO(key, stat), nil
}

func (s *S3Storage) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

func (s *S3Storage) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, err
	}
	return objectInfoFromMinIO(key, info), nil
}

func objectInfoFromMinIO(key string, info minio.ObjectInfo) ObjectInfo {
	return ObjectInfo{
		Key:         key,
		ContentType: info.ContentType,
		Size:        info.Size,
		Metadata:    info.UserMetadata,
		UpdatedAt:   info.LastModified,
	}
}
