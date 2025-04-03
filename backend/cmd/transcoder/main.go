package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/falcon/backend/internal/database"
	"github.com/falcon/backend/internal/ffmpeg"
	"github.com/falcon/backend/internal/storage"
	"github.com/golang/glog"
	"github.com/spf13/viper"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// Define workflow constants
const (
	TaskQueue = "TRANSCODER_TASK_QUEUE"
)

// TranscodeParams contains parameters for the transcoding workflow
type TranscodeParams struct {
	VideoID     string `json:"videoID"`
	ObjectKey   string `json:"objectKey"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
}

// ResolutionConfig defines a resolution for transcoding
type ResolutionConfig struct {
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Bitrate string `json:"bitrate"`
}

// Initialize configuration
func init() {
	// Initialize configuration
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("Error reading config file: %s", err)
	}

	// Set up logging
	flag.Set("logtostderr", "true")
	flag.Set("stderrthreshold", "INFO")
	flag.Set("v", "2")
	flag.Parse()
}

func main() {
	// Set up storage service
	storageConfig := storage.Config{
		Endpoint:  viper.GetString("storage.endpoint"),
		Region:    viper.GetString("storage.region"),
		Bucket:    viper.GetString("storage.bucket"),
		AccessKey: viper.GetString("storage.access_key"),
		SecretKey: viper.GetString("storage.secret_key"),
		UseSSL:    viper.GetBool("storage.use_ssl"),
	}

	storageService, err := storage.NewStorageService(storageConfig)
	if err != nil {
		glog.Fatalf("Failed to initialize storage service: %v", err)
	}

	// Set up database
	db, err := database.NewDatabaseFromConfig()
	if err != nil {
		glog.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Create tables if they don't exist
	if err := db.CreateTables(context.Background()); err != nil {
		glog.Fatalf("Failed to create database tables: %v", err)
	}

	// Configure Temporal client
	temporalClient, err := client.NewClient(client.Options{
		HostPort: viper.GetString("temporal.host") + ":" + viper.GetString("temporal.port"),
	})

	if err != nil {
		glog.Fatalf("Unable to create Temporal client: %v", err)
	}
	defer temporalClient.Close()

	glog.Info("Temporal client connected successfully")

	// Create worker
	w := worker.New(temporalClient, TaskQueue, worker.Options{})

	// Create activity dependencies
	deps := &ActivityDependencies{
		Storage: storageService,
		DB:      db,
		FFmpeg: ffmpeg.NewFFmpeg(
			viper.GetString("ffmpeg.path"),
			viper.GetInt("ffmpeg.thread_count"),
			viper.GetString("ffmpeg.preset"),
		),
	}

	// Register workflows and activities
	w.RegisterWorkflow(TranscodeWorkflow)
	w.RegisterActivity(DownloadVideoActivity)
	w.RegisterActivity(ExtractMetadataActivity)
	w.RegisterActivity(TranscodeVideoActivity)
	w.RegisterActivity(CleanupActivity)

	// Set activity context
	w.Options.BackgroundActivityContext = context.WithValue(
		context.Background(),
		"dependencies",
		deps,
	)

	// Start worker
	glog.Info("Starting Transcoder worker")
	err = w.Run(worker.InterruptCh())
	if err != nil {
		glog.Fatalf("Unable to start worker: %v", err)
	}
}

// ActivityDependencies holds references to services needed by activities
type ActivityDependencies struct {
	Storage *storage.StorageService
	DB      *database.Database
	FFmpeg  *ffmpeg.FFmpeg
}

// GetDependencies extracts dependencies from the context
func GetDependencies(ctx context.Context) *ActivityDependencies {
	return ctx.Value("dependencies").(*ActivityDependencies)
}

// TranscodeWorkflow defines the workflow for video transcoding
func TranscodeWorkflow(ctx workflow.Context, params TranscodeParams) (string, error) {
	glog.Infof("Starting transcoding workflow for video: %s", params.VideoID)

	// Update video status to "processing"
	if err := updateVideoStatus(ctx, params.VideoID, "processing"); err != nil {
		return "", err
	}

	// Activity options with retry policy
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute, // Video transcoding can take a while
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Minute,
			BackoffCoefficient: 2.0,
			MaximumInterval:    10 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// 1. Download video
	var downloadResult DownloadResult
	if err := workflow.ExecuteActivity(ctx, DownloadVideoActivity, params).Get(ctx, &downloadResult); err != nil {
		updateVideoStatus(ctx, params.VideoID, "error")
		return "", err
	}

	// 2. Extract metadata
	var metadataResult MetadataResult
	if err := workflow.ExecuteActivity(ctx, ExtractMetadataActivity, downloadResult).Get(ctx, &metadataResult); err != nil {
		updateVideoStatus(ctx, params.VideoID, "error")
		return "", err
	}

	// 3. Transcode video
	var transcodeResult TranscodeResult
	if err := workflow.ExecuteActivity(ctx, TranscodeVideoActivity, TranscodeParams{
		VideoID:     params.VideoID,
		ObjectKey:   params.ObjectKey,
		Filename:    params.Filename,
		ContentType: params.ContentType,
	}).Get(ctx, &transcodeResult); err != nil {
		updateVideoStatus(ctx, params.VideoID, "error")
		return "", err
	}

	// 4. Cleanup temporary files
	if err := workflow.ExecuteActivity(ctx, CleanupActivity, downloadResult.LocalPath).Get(ctx, nil); err != nil {
		glog.Warningf("Cleanup failed: %v", err)
		// Non-critical error, continue
	}

	// Update video status to "completed"
	if err := updateVideoStatus(ctx, params.VideoID, "completed"); err != nil {
		return "", err
	}

	glog.Infof("Transcoding workflow completed for video: %s", params.VideoID)
	return "Transcoding completed for " + params.VideoID, nil
}

// Helper function to update video status
func updateVideoStatus(ctx workflow.Context, videoID, status string) error {
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    10 * time.Second,
			MaximumAttempts:    5,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	return workflow.ExecuteActivity(ctx, func(ctx context.Context, videoID, status string) error {
		deps := GetDependencies(ctx)
		return deps.DB.UpdateVideoStatus(ctx, videoID, status)
	}, videoID, status).Get(ctx, nil)
}

// DownloadResult stores the result of downloading a video
type DownloadResult struct {
	VideoID      string
	LocalPath    string
	OriginalPath string
}

// DownloadVideoActivity downloads the video from storage
func DownloadVideoActivity(ctx context.Context, params TranscodeParams) (DownloadResult, error) {
	deps := GetDependencies(ctx)

	// Create a temporary directory for processing
	tempDir, err := os.MkdirTemp("", "transcode-"+params.VideoID)
	if err != nil {
		return DownloadResult{}, err
	}

	// Determine local path
	filename := filepath.Base(params.ObjectKey)
	localPath := filepath.Join(tempDir, filename)

	// Download the file
	err = deps.Storage.DownloadFile(ctx, params.ObjectKey, localPath)
	if err != nil {
		return DownloadResult{}, err
	}

	// Create or update video in database
	now := time.Now()
	video := &database.Video{
		ID:              params.VideoID,
		Title:           strings.TrimSuffix(params.Filename, filepath.Ext(params.Filename)),
		OriginalName:    params.Filename,
		OriginalPath:    params.ObjectKey,
		ProcessingState: "downloading",
		Size:            getFileSize(localPath),
		ContentType:     params.ContentType,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	err = deps.DB.CreateVideo(ctx, video)
	if err != nil {
		return DownloadResult{}, err
	}

	return DownloadResult{
		VideoID:      params.VideoID,
		LocalPath:    localPath,
		OriginalPath: params.ObjectKey,
	}, nil
}

// MetadataResult stores the result of metadata extraction
type MetadataResult struct {
	VideoID  string
	Duration float64
}

// ExtractMetadataActivity extracts metadata from the video
func ExtractMetadataActivity(ctx context.Context, download DownloadResult) (MetadataResult, error) {
	deps := GetDependencies(ctx)

	// Use FFmpeg to get media info
	info, err := deps.FFmpeg.GetMediaInfo(download.LocalPath)
	if err != nil {
		return MetadataResult{}, err
	}

	// Extract duration
	var duration float64
	if durationStr, ok := info["duration"]; ok {
		duration, _ = strconv.ParseFloat(durationStr, 64)
	}

	// Update video status
	err = deps.DB.UpdateVideoStatus(ctx, download.VideoID, "analyzing")
	if err != nil {
		return MetadataResult{}, err
	}

	return MetadataResult{
		VideoID:  download.VideoID,
		Duration: duration,
	}, nil
}

// TranscodeResult stores the result of transcoding
type TranscodeResult struct {
	VideoID         string
	OutputDirectory string
	Streams         []StreamInfo
}

// StreamInfo contains information about a transcoded stream
type StreamInfo struct {
	Resolution  string
	Bitrate     string
	Format      string
	Path        string
	Size        int64
	SegmentSize int
}

// TranscodeVideoActivity transcodes the video to multiple formats and resolutions
func TranscodeVideoActivity(ctx context.Context, params TranscodeParams) (TranscodeResult, error) {
	deps := GetDependencies(ctx)

	// Get video from database
	video, err := deps.DB.GetVideo(ctx, params.VideoID)
	if err != nil {
		return TranscodeResult{}, err
	}

	// Update video status
	err = deps.DB.UpdateVideoStatus(ctx, params.VideoID, "transcoding")
	if err != nil {
		return TranscodeResult{}, err
	}

	// Create a temporary directory for transcoded files
	outputDir, err := os.MkdirTemp("", "output-"+params.VideoID)
	if err != nil {
		return TranscodeResult{}, err
	}

	// Get input file path
	tempDir := filepath.Dir(filepath.Join(os.TempDir(), "transcode-"+params.VideoID))
	inputFile := filepath.Join(tempDir, filepath.Base(params.ObjectKey))

	// Get resolution configs from viper
	var resolutions []ffmpeg.Resolution
	if err := viper.UnmarshalKey("ffmpeg.resolutions", &resolutions); err != nil {
		return TranscodeResult{}, err
	}

	// Transcode to HLS
	segmentFilename := params.VideoID
	err = deps.FFmpeg.TranscodeToHLS(inputFile, outputDir, segmentFilename, resolutions)
	if err != nil {
		return TranscodeResult{}, err
	}

	// Upload transcoded files to storage
	// In a real implementation, we would upload all the files

	// Record streams in database
	var streams []StreamInfo
	for i, res := range resolutions {
		variantName := fmt.Sprintf("v%d", i)
		playlistFile := fmt.Sprintf("%s_%s.m3u8", segmentFilename, variantName)
		streamPath := fmt.Sprintf("videos/%s/hls/%s", params.VideoID, playlistFile)
		
		stream := &database.VideoStream{
			ID:          params.VideoID + "-" + variantName,
			VideoID:     params.VideoID,
			Resolution:  fmt.Sprintf("%dx%d", res.Width, res.Height),
			Bitrate:     res.Bitrate,
			Format:      "hls",
			Path:        streamPath,
			Size:        0, // In a real implementation, we would calculate the total size
			SegmentSize: 10, // In a real implementation, this would be the segment size in seconds
			CreatedAt:   time.Now(),
		}
		
		if err := deps.DB.AddVideoStream(ctx, stream); err != nil {
			return TranscodeResult{}, err
		}
		
		streams = append(streams, StreamInfo{
			Resolution:  fmt.Sprintf("%dx%d", res.Width, res.Height),
			Bitrate:     res.Bitrate,
			Format:      "hls",
			Path:        streamPath,
			Size:        0,
			SegmentSize: 10,
		})
	}

	return TranscodeResult{
		VideoID:         params.VideoID,
		OutputDirectory: outputDir,
		Streams:         streams,
	}, nil
}

// CleanupActivity cleans up temporary files
func CleanupActivity(ctx context.Context, localPath string) error {
	// Remove the directory containing the temporary files
	dir := filepath.Dir(localPath)
	return os.RemoveAll(dir)
}

// Helper function to get file size
func getFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
} 