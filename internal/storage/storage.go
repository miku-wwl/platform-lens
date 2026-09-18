package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type ArtifactStorage interface {
	Put(context.Context, string, []byte) (string, error)
	Get(context.Context, string) ([]byte, error)
	Exists(context.Context, string) (bool, error)
	Ready(context.Context) error
}

type FileSystem struct{ Root string }

func NewFileSystem(root string) (*FileSystem, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &FileSystem{Root: root}, nil
}

func (s *FileSystem) path(uri string) (string, error) {
	if filepath.IsAbs(uri) {
		return "", errors.New("absolute artifact path")
	}
	path := filepath.Join(s.Root, filepath.Clean(uri))
	root, err := filepath.EvalSymlinks(s.Root)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(path)
	if resolved, err := filepath.EvalSymlinks(parent); err == nil {
		path = filepath.Join(resolved, filepath.Base(path))
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("artifact path outside root")
	}
	return path, nil
}

func (s *FileSystem) Put(_ context.Context, uri string, data []byte) (string, error) {
	path, err := s.path(uri)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return uri, nil
}
func (s *FileSystem) Get(_ context.Context, uri string) ([]byte, error) {
	path, err := s.path(uri)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}
func (s *FileSystem) Exists(_ context.Context, uri string) (bool, error) {
	path, err := s.path(uri)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
func (s *FileSystem) Ready(_ context.Context) error {
	probe := filepath.Join(s.Root, ".ready")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return err
	}
	return os.Remove(probe)
}

type S3 struct {
	Client *s3.Client
	Bucket string
}

func NewS3(ctx context.Context, endpoint, region, bucket string) (*S3, error) {
	options := []func(*config.LoadOptions) error{config.WithRegion(region)}
	if endpoint != "" {
		options = append(options, config.WithBaseEndpoint(endpoint), config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")))
	}
	cfg, err := config.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(cfg, func(options *s3.Options) { options.UsePathStyle = true })
	result := &S3{Client: client, Bucket: bucket}
	if err := result.ensureBucket(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *S3) ensureBucket(ctx context.Context) error {
	_, err := s.Client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.Bucket)})
	if err == nil {
		return nil
	}
	_, err = s.Client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(s.Bucket)})
	return err
}
func (s *S3) Put(ctx context.Context, uri string, data []byte) (string, error) {
	_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(uri), Body: strings.NewReader(string(data))})
	if err != nil {
		return "", err
	}
	return "s3://" + s.Bucket + "/" + uri, nil
}
func (s *S3) Get(ctx context.Context, uri string) ([]byte, error) {
	result, err := s.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(strings.TrimPrefix(uri, "s3://"+s.Bucket+"/"))})
	if err != nil {
		return nil, err
	}
	defer result.Body.Close()
	return io.ReadAll(result.Body)
}
func (s *S3) Exists(ctx context.Context, uri string) (bool, error) {
	_, err := s.Client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(strings.TrimPrefix(uri, "s3://"+s.Bucket+"/"))})
	if err != nil {
		return false, nil
	}
	return true, nil
}
func (s *S3) Ready(ctx context.Context) error {
	_, err := s.Client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.Bucket)})
	return err
}

func Hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func ArtifactURI(runID string, attempt int, name string) string {
	return fmt.Sprintf("runs/%s/attempts/%d/%s", runID, attempt, filepath.ToSlash(name))
}
