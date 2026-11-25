package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
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
	// url.Parse treats "minio:9000" as scheme "minio", not as a host:port.
	// Validated bare host-and-port values (DNS, IPv4, bracketed IPv6) keep the caller's Secure flag.
	if isBareHostPort(endpoint) {
		return endpoint, secure, nil
	}
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

// isBareHostPort reports whether endpoint is a host:port value without a scheme,
// where host is a DNS name, an IPv4 address or a bracketed IPv6 address and port
// is in the range 1-65535.
func isBareHostPort(endpoint string) bool {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil || host == "" {
		return false
	}
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 || strconv.Itoa(portNum) != port {
		return false
	}
	if strings.HasPrefix(endpoint, "[") {
		return net.ParseIP(host) != nil && strings.Contains(host, ":")
	}
	if net.ParseIP(host) != nil {
		return true
	}
	return isDNSName(host)
}

func isDNSName(host string) bool {
	if len(host) > 253 {
		return false
	}
	labels := strings.Split(strings.TrimSuffix(host, "."), ".")
	// A numeric final label is a malformed IPv4 address, not a DNS name.
	if _, err := strconv.Atoi(labels[len(labels)-1]); err == nil {
		return false
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			default:
				return false
			}
		}
	}
	return true
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

func (s *S3Storage) Ready(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("s3 bucket %s does not exist", s.bucket)
	}
	return err
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
