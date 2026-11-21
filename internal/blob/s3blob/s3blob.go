// Package s3blob adapts any S3-compatible store to blob.Store. It is the only
// package that imports the AWS SDK (docs/adr/0013-pdf-import.md).
package s3blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/rokumate/ronote/internal/blob"
)

// Config names the store. Nothing is read from the environment or from
// ~/.aws: RoNote's config is the only source.
type Config struct {
	Endpoint  string // e.g. http://127.0.0.1:8333
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
}

// Store is a bucket of an S3-compatible store.
type Store struct {
	client *s3.Client
	bucket string
}

// New returns a store for cfg's bucket. It makes no request.
func New(cfg Config) *Store {
	client := s3.New(s3.Options{
		BaseEndpoint: aws.String(cfg.Endpoint),
		Region:       cfg.Region,
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		// Path-style addressing and checksums only where required: what
		// S3-compatible stores other than AWS all accept.
		UsePathStyle:               true,
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
	return &Store{client: client, bucket: cfg.Bucket}
}

// Put uploads body in one request.
func (s *Store) Put(ctx context.Context, key string, body io.ReadSeeker, size int64) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        &s.bucket,
		Key:           &key,
		Body:          body,
		ContentLength: &size,
	})
	if err != nil {
		return fmt.Errorf("s3blob: put %s: %w", key, err)
	}
	return nil
}

// Get opens the object for reading.
func (s *Store) Get(ctx context.Context, key string) (*blob.Object, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		if notFound(err) {
			return nil, blob.ErrNotFound
		}
		return nil, fmt.Errorf("s3blob: get %s: %w", key, err)
	}
	return &blob.Object{Body: out.Body, Size: aws.ToInt64(out.ContentLength)}, nil
}

// Delete removes the object. S3 answers a delete of a missing key with
// success; a store that answers 404 is treated the same.
func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil && !notFound(err) {
		return fmt.Errorf("s3blob: delete %s: %w", key, err)
	}
	return nil
}

// List pages through ListObjectsV2.
func (s *Store) List(ctx context.Context, prefix string) iter.Seq2[blob.Info, error] {
	return func(yield func(blob.Info, error) bool) {
		pages := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{Bucket: &s.bucket, Prefix: &prefix})
		for pages.HasMorePages() {
			page, err := pages.NextPage(ctx)
			if err != nil {
				yield(blob.Info{}, fmt.Errorf("s3blob: list %s: %w", prefix, err))
				return
			}
			for _, obj := range page.Contents {
				info := blob.Info{Key: aws.ToString(obj.Key), Size: aws.ToInt64(obj.Size), Modified: aws.ToTime(obj.LastModified)}
				if !yield(info, nil) {
					return
				}
			}
		}
	}
}

// Check fails unless the bucket exists and takes a write, naming it. A write
// alone can't tell: SeaweedFS makes a missing bucket for a key with Admin
// rights, where Oracle's store refuses one (docs/adr/0015-operations.md).
func (s *Store) Check(ctx context.Context) error {
	if _, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &s.bucket}); err != nil {
		if notFound(err) {
			return fmt.Errorf("blob store: bucket %s doesn't exist: make it, as docs/runbook.md says", s.bucket)
		}
		return fmt.Errorf("blob store: bucket %s: %w", s.bucket, err)
	}
	return blob.CheckWritable(ctx, s, s.bucket)
}

func notFound(err error) bool {
	var noKey *types.NoSuchKey
	var resp *smithyhttp.ResponseError
	return errors.As(err, &noKey) || errors.As(err, &resp) && resp.HTTPStatusCode() == http.StatusNotFound
}
