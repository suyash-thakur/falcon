package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/golang/glog"
)

// Config holds storage configuration
type Config struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	UseSSL    bool
}

// StorageService provides methods to interact with S3-compatible storage
type StorageService struct {
	s3Client *s3.S3
	bucket   string
}

// NewStorageService creates a new storage service
func NewStorageService(config Config) (*StorageService, error) {
	// Create custom S3 session
	s3Config := &aws.Config{
		Credentials:      credentials.NewStaticCredentials(config.AccessKey, config.SecretKey, ""),
		Endpoint:         aws.String(config.Endpoint),
		Region:           aws.String(config.Region),
		DisableSSL:       aws.Bool(!config.UseSSL),
		S3ForcePathStyle: aws.Bool(true), // Required for MinIO
	}

	sess, err := session.NewSession(s3Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 session: %v", err)
	}

	// Create S3 client
	s3Client := s3.New(sess)

	// Create bucket if it doesn't exist
	_, err = s3Client.HeadBucket(&s3.HeadBucketInput{
		Bucket: aws.String(config.Bucket),
	})

	if err != nil {
		glog.Infof("Bucket %s does not exist, creating it...", config.Bucket)
		_, err = s3Client.CreateBucket(&s3.CreateBucketInput{
			Bucket: aws.String(config.Bucket),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create bucket: %v", err)
		}
	}

	return &StorageService{
		s3Client: s3Client,
		bucket:   config.Bucket,
	}, nil
}

// UploadFile uploads a file to S3 storage
func (s *StorageService) UploadFile(ctx context.Context, localFilePath, objectKey string) (string, error) {
	// Read file content
	fileContent, err := ioutil.ReadFile(localFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %v", err)
	}

	// Upload to S3
	_, err = s.s3Client.PutObjectWithContext(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(objectKey),
		Body:        bytes.NewReader(fileContent),
		ContentType: aws.String(getContentType(localFilePath)),
	})

	if err != nil {
		return "", fmt.Errorf("failed to upload file to S3: %v", err)
	}

	// Return object URL
	return fmt.Sprintf("s3://%s/%s", s.bucket, objectKey), nil
}

// DownloadFile downloads a file from S3 storage
func (s *StorageService) DownloadFile(ctx context.Context, objectKey, localFilePath string) error {
	// Get object from S3
	resp, err := s.s3Client.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey),
	})

	if err != nil {
		return fmt.Errorf("failed to get object from S3: %v", err)
	}
	defer resp.Body.Close()

	// Create local file
	localFile, err := ioutil.TempFile(filepath.Dir(localFilePath), "download-*")
	if err != nil {
		return fmt.Errorf("failed to create local file: %v", err)
	}
	defer localFile.Close()

	// Copy object content to local file
	_, err = io.Copy(localFile, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to copy content to local file: %v", err)
	}

	// Rename temp file to target path
	err = os.Rename(localFile.Name(), localFilePath)
	if err != nil {
		return fmt.Errorf("failed to rename temp file: %v", err)
	}

	return nil
}

// GetSignedURL generates a pre-signed URL for accessing an object
func (s *StorageService) GetSignedURL(ctx context.Context, objectKey string, expiration time.Duration) (string, error) {
	req, _ := s.s3Client.GetObjectRequest(&s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey),
	})

	// Generate signed URL
	url, err := req.Presign(expiration)
	if err != nil {
		return "", fmt.Errorf("failed to sign request: %v", err)
	}

	return url, nil
}

// Helper function to determine content type
func getContentType(filePath string) string {
	ext := filepath.Ext(filePath)
	switch ext {
	case ".mp4":
		return "video/mp4"
	case ".ts":
		return "video/MP2T"
	case ".m3u8":
		return "application/x-mpegURL"
	case ".mpd":
		return "application/dash+xml"
	default:
		return "application/octet-stream"
	}
} 