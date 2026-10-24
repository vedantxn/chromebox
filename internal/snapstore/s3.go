package snapstore

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3 is a Backend on an S3 bucket. Credentials come from the node's instance
// role (scripts/aws/storage.sh); the role allows this one bucket and nothing else.
type S3 struct {
	c      *s3.Client
	bucket string
}

func NewS3(ctx context.Context, bucket, region string) (*S3, error) {
	// Many parallel transfers to one host: the default of 2 idle connections
	// per host would reconnect for most of them.
	hc := awshttp.NewBuildableClient().WithTransportOptions(func(t *http.Transport) {
		t.MaxIdleConnsPerHost = 32
	})
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithHTTPClient(hc))
	if err != nil {
		return nil, err
	}
	return &S3{c: s3.NewFromConfig(cfg), bucket: bucket}, nil
}

func (s *S3) Put(ctx context.Context, key string, data []byte) error {
	_, err := s.c.PutObject(ctx, &s3.PutObjectInput{Bucket: &s.bucket, Key: &key, Body: bytes.NewReader(data)})
	return err
}

func (s *S3) Get(ctx context.Context, key string) ([]byte, error) {
	r, err := s.c.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

func (s *S3) DeletePrefix(ctx context.Context, prefix string) (int, error) {
	n := 0
	p := s3.NewListObjectsV2Paginator(s.c, &s3.ListObjectsV2Input{Bucket: &s.bucket, Prefix: aws.String(prefix)})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return n, err
		}
		if len(page.Contents) == 0 {
			continue
		}
		ids := make([]types.ObjectIdentifier, len(page.Contents))
		for i, o := range page.Contents {
			ids[i] = types.ObjectIdentifier{Key: o.Key}
		}
		if _, err := s.c.DeleteObjects(ctx, &s3.DeleteObjectsInput{Bucket: &s.bucket, Delete: &types.Delete{Objects: ids, Quiet: aws.Bool(true)}}); err != nil {
			return n, err
		}
		n += len(ids)
	}
	return n, nil
}
