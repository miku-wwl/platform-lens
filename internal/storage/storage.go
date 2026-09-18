package storage

import (
	"bytes"
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
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/miku-wwl/platform-lens/internal/cloudaws"
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

func NewS3(config cloudaws.Config, bucket string) (*S3, error) {
	client := s3.NewFromConfig(config.AWS, func(options *s3.Options) {
		options.UsePathStyle = config.Endpoint != ""
	})
	return &S3{Client: client, Bucket: bucket}, nil
}
func (s *S3) Put(ctx context.Context, uri string, data []byte) (string, error) {
	key, err := s.key(uri)
	if err != nil {
		return "", err
	}
	_, err = s.Client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key), Body: bytes.NewReader(data)})
	if err != nil {
		return "", err
	}
	return s.uri(key), nil
}
func (s *S3) Get(ctx context.Context, uri string) ([]byte, error) {
	key, err := s.key(uri)
	if err != nil {
		return nil, err
	}
	result, err := s.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key)})
	if err != nil {
		return nil, err
	}
	defer result.Body.Close()
	return io.ReadAll(result.Body)
}
func (s *S3) Exists(ctx context.Context, uri string) (bool, error) {
	key, err := s.key(uri)
	if err != nil {
		return false, err
	}
	_, err = s.Client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key)})
	return classifyExistsError(err)
}
func (s *S3) Ready(ctx context.Context) error {
	_, err := s.Client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.Bucket)})
	return err
}

func Hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func ArtifactURI(runID string, attempt int, name string) string {
	return fmt.Sprintf("runs/%s/attempts/%d/%s", runID, attempt, filepath.ToSlash(name))
}

func (s *S3) key(uri string) (string, error) {
	prefix := "s3://" + s.Bucket + "/"
	if strings.HasPrefix(uri, "s3://") {
		if !strings.HasPrefix(uri, prefix) {
			return "", fmt.Errorf("artifact URI bucket mismatch: %s", uri)
		}
		uri = strings.TrimPrefix(uri, prefix)
	}
	uri = filepath.ToSlash(strings.TrimPrefix(uri, "/"))
	if uri == "" || strings.Contains(uri, "..") {
		return "", errors.New("invalid S3 artifact key")
	}
	return uri, nil
}

func (s *S3) uri(key string) string { return "s3://" + s.Bucket + "/" + key }

func classifyExistsError(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	if cloudaws.IsNotFound(err) {
		return false, nil
	}
	return false, err
}
