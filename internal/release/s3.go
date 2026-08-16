package release

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/url"
	"os"
	"path"
	"strings"
	"unicode"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

const checksumMetadataKey = "adenosine-sha256"

// S3Config describes one S3-compatible release asset bucket.
type S3Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	PathStyle       bool
}

type s3Client interface {
	HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// S3 stores immutable release asset objects in one shared bucket.
type S3 struct {
	client s3Client
	bucket string
}

// NewS3 constructs an S3-compatible backend and verifies bucket access.
func NewS3(ctx context.Context, cfg S3Config) (*S3, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	sdkConfig, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken)),
		awsconfig.WithRetryMode(aws.RetryModeStandard),
		awsconfig.WithRetryMaxAttempts(3),
	)
	if err != nil {
		return nil, fmt.Errorf("load S3 client configuration: %w", err)
	}
	client := s3.NewFromConfig(sdkConfig, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(cfg.Endpoint)
		options.UsePathStyle = cfg.PathStyle
	})
	storage := &S3{client: client, bucket: cfg.Bucket}
	if _, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(cfg.Bucket)}); err != nil {
		return nil, fmt.Errorf("verify S3 bucket access: %w", err)
	}
	return storage, nil
}

// Validate checks S3 configuration without performing network I/O.
func (cfg S3Config) Validate() error {
	endpoint, err := url.ParseRequestURI(cfg.Endpoint)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.ForceQuery || endpoint.RawPath != "" {
		return fmt.Errorf("release asset S3 endpoint must be an absolute HTTP or HTTPS URL without userinfo, query, or fragment")
	}
	if cfg.Region == "" || strings.TrimSpace(cfg.Region) != cfg.Region {
		return fmt.Errorf("release asset S3 region must not be empty or contain surrounding whitespace")
	}
	if !validBucket(cfg.Bucket) {
		return fmt.Errorf("release asset S3 bucket is invalid")
	}
	if cfg.AccessKeyID == "" || strings.TrimSpace(cfg.AccessKeyID) != cfg.AccessKeyID {
		return fmt.Errorf("release asset S3 access key ID must not be empty or contain surrounding whitespace")
	}
	if cfg.SecretAccessKey == "" || strings.TrimSpace(cfg.SecretAccessKey) != cfg.SecretAccessKey {
		return fmt.Errorf("release asset S3 secret access key must not be empty or contain surrounding whitespace")
	}
	if strings.TrimSpace(cfg.SessionToken) != cfg.SessionToken {
		return fmt.Errorf("release asset S3 session token must not contain surrounding whitespace")
	}
	return nil
}

func (storage *S3) Put(ctx context.Context, key string, source io.Reader, expectedSize int64) (string, error) {
	if expectedSize < 0 || source == nil {
		return "", ErrSizeMismatch
	}
	if err := validateStorageKey(key); err != nil {
		return "", err
	}
	temporary, err := os.CreateTemp("", "adenosine-release-asset-*")
	if err != nil {
		return "", fmt.Errorf("create release asset staging file: %w", err)
	}
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporary.Name())
	}()

	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, digest), &contextReader{ctx: ctx, reader: io.LimitReader(source, expectedSize+1)})
	if err != nil {
		return "", fmt.Errorf("stage release asset: %w", err)
	}
	if written != expectedSize {
		return "", ErrSizeMismatch
	}
	if err := temporary.Sync(); err != nil {
		return "", fmt.Errorf("sync release asset staging file: %w", err)
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind release asset staging file: %w", err)
	}

	digestBytes := digest.Sum(nil)
	checksumHex := hex.EncodeToString(digestBytes)
	checksumBase64 := base64.StdEncoding.EncodeToString(digestBytes)
	_, err = storage.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:            aws.String(storage.bucket),
		Key:               aws.String(key),
		Body:              temporary,
		ContentLength:     aws.Int64(expectedSize),
		ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
		ChecksumSHA256:    aws.String(checksumBase64),
		IfNoneMatch:       aws.String("*"),
		Metadata:          map[string]string{checksumMetadataKey: checksumHex},
	})
	if err == nil {
		return checksumHex, nil
	}
	if !s3ErrorCode(err, "PreconditionFailed", "ConditionalRequestConflict") {
		return "", fmt.Errorf("put release asset object: %w", err)
	}
	matches, inspectErr := storage.matchingObject(ctx, key, expectedSize, checksumHex)
	if inspectErr != nil {
		return "", inspectErr
	}
	if !matches {
		return "", ErrConflict
	}
	return checksumHex, nil
}

func (storage *S3) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := validateStorageKey(key); err != nil {
		return nil, err
	}
	object, err := storage.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(storage.bucket), Key: aws.String(key),
	})
	if s3ErrorCode(err, "NoSuchKey", "NotFound") {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get release asset object: %w", err)
	}
	expected, err := objectChecksum(object.Metadata, object.ChecksumSHA256)
	if err != nil {
		_ = object.Body.Close()
		return nil, err
	}
	return &checksumReadCloser{body: object.Body, digest: sha256.New(), expected: expected}, nil
}

func (storage *S3) Delete(ctx context.Context, key string) error {
	if err := validateStorageKey(key); err != nil {
		return err
	}
	_, err := storage.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(storage.bucket), Key: aws.String(key)})
	if s3ErrorCode(err, "NoSuchKey", "NotFound") {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete release asset object: %w", err)
	}
	return nil
}

func (storage *S3) matchingObject(ctx context.Context, key string, expectedSize int64, expectedChecksum string) (bool, error) {
	object, err := storage.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(storage.bucket), Key: aws.String(key),
	})
	if s3ErrorCode(err, "NoSuchKey", "NotFound") {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect existing release asset object: %w", err)
	}
	checksum, err := objectChecksum(object.Metadata, object.ChecksumSHA256)
	if err != nil {
		return false, nil
	}
	return object.ContentLength != nil && *object.ContentLength == expectedSize && checksum == expectedChecksum, nil
}

func objectChecksum(metadata map[string]string, encoded *string) (string, error) {
	if value := strings.ToLower(metadata[checksumMetadataKey]); validSHA256(value) {
		return value, nil
	}
	if encoded != nil {
		value, err := base64.StdEncoding.DecodeString(*encoded)
		if err == nil && len(value) == sha256.Size {
			return hex.EncodeToString(value), nil
		}
	}
	return "", fmt.Errorf("release asset object is missing a valid SHA-256 checksum")
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func validBucket(value string) bool {
	if value == "" || len(value) > 255 || strings.TrimSpace(value) != value || strings.ContainsAny(value, `/\\`) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validateStorageKey(key string) error {
	clean := path.Clean(key)
	if key == "" || clean != key || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(key, "/") || strings.ContainsRune(key, 0) {
		return fmt.Errorf("invalid release asset key")
	}
	return nil
}

func s3ErrorCode(err error, codes ...string) bool {
	if err == nil {
		return false
	}
	var apiError smithy.APIError
	if !errors.As(err, &apiError) {
		return false
	}
	for _, code := range codes {
		if apiError.ErrorCode() == code {
			return true
		}
	}
	return false
}

type checksumReadCloser struct {
	body     io.ReadCloser
	digest   hash.Hash
	expected string
	finished bool
	result   error
}

func (reader *checksumReadCloser) Read(buffer []byte) (int, error) {
	if reader.finished {
		return 0, reader.result
	}
	read, err := reader.body.Read(buffer)
	if read > 0 {
		_, _ = reader.digest.Write(buffer[:read])
	}
	if errors.Is(err, io.EOF) {
		reader.finished = true
		reader.result = io.EOF
		if hex.EncodeToString(reader.digest.Sum(nil)) != reader.expected {
			reader.result = ErrChecksumMismatch
		}
	}
	return read, reader.resultOr(err)
}

func (reader *checksumReadCloser) resultOr(err error) error {
	if reader.finished {
		return reader.result
	}
	return err
}

func (reader *checksumReadCloser) Close() error { return reader.body.Close() }
