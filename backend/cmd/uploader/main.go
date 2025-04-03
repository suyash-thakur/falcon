package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/falcon/backend/internal/storage"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
	"github.com/spf13/viper"
	"go.temporal.io/sdk/client"
)

// VideoUploadResponse represents the response to a video upload request
type VideoUploadResponse struct {
	VideoID     string `json:"video_id"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	Status      string `json:"status"`
	Message     string `json:"message"`
	Timestamp   string `json:"timestamp"`
}

// Initialize configuration and set up dependencies
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
	// Create a new router
	router := mux.NewRouter()

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

	// Set up Temporal client
	temporalClient, err := client.NewClient(client.Options{
		HostPort: viper.GetString("temporal.host") + ":" + viper.GetString("temporal.port"),
	})
	if err != nil {
		glog.Fatalf("Unable to create Temporal client: %v", err)
	}
	defer temporalClient.Close()

	// Create upload handler with dependencies
	uploadHandler := NewUploadHandler(storageService, temporalClient)

	// Define routes
	router.HandleFunc("/health", healthCheckHandler).Methods("GET")
	router.HandleFunc("/upload", uploadHandler.UploadVideo).Methods("POST")

	// Set up server
	port := viper.GetString("server.port")
	if port == "" {
		port = "8001" // Different port from main service
	}

	srv := &http.Server{
		Handler:      router,
		Addr:         ":" + port,
		WriteTimeout: 15 * time.Minute, // Longer timeout for video uploads
		ReadTimeout:  15 * time.Minute,
	}

	// Run server
	glog.Infof("Uploader service starting on port %s", port)
	if err := srv.ListenAndServe(); err != nil {
		glog.Fatalf("Failed to start server: %v", err)
	}
}

// UploadHandler handles video upload requests
type UploadHandler struct {
	storageService *storage.StorageService
	temporalClient client.Client
}

// NewUploadHandler creates a new upload handler
func NewUploadHandler(storageService *storage.StorageService, temporalClient client.Client) *UploadHandler {
	return &UploadHandler{
		storageService: storageService,
		temporalClient: temporalClient,
	}
}

// UploadVideo handles video file uploads
func (h *UploadHandler) UploadVideo(w http.ResponseWriter, r *http.Request) {
	// Set response headers
	w.Header().Set("Content-Type", "application/json")

	// Parse multipart form (max 500MB)
	if err := r.ParseMultipartForm(500 << 20); err != nil {
		handleError(w, "Failed to parse form", err, http.StatusBadRequest)
		return
	}

	// Get the file from the form
	file, header, err := r.FormFile("video")
	if err != nil {
		handleError(w, "Failed to get video file", err, http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate file type
	contentType := header.Header.Get("Content-Type")
	if !isValidVideoType(contentType) {
		handleError(w, "Invalid video format", nil, http.StatusBadRequest)
		return
	}

	// Generate a unique filename
	videoID := generateUniqueID()
	ext := filepath.Ext(header.Filename)
	filename := videoID + ext
	tempFile := filepath.Join(os.TempDir(), filename)

	// Save to temporary file
	out, err := os.Create(tempFile)
	if err != nil {
		handleError(w, "Failed to create temporary file", err, http.StatusInternalServerError)
		return
	}
	defer out.Close()
	defer os.Remove(tempFile) // Clean up temp file when done

	// Copy file content to temporary file
	_, err = io.Copy(out, file)
	if err != nil {
		handleError(w, "Failed to save file", err, http.StatusInternalServerError)
		return
	}
	out.Close() // Close now to ensure file is fully written

	// Upload to storage
	objectKey := fmt.Sprintf("uploads/%s/%s", videoID, filename)
	_, err = h.storageService.UploadFile(r.Context(), tempFile, objectKey)
	if err != nil {
		handleError(w, "Failed to upload to storage", err, http.StatusInternalServerError)
		return
	}

	// Start transcoding workflow
	workflowOptions := client.StartWorkflowOptions{
		ID:        "transcode-" + videoID,
		TaskQueue: "TRANSCODER_TASK_QUEUE",
	}

	workflowParams := map[string]interface{}{
		"videoID":     videoID,
		"objectKey":   objectKey,
		"filename":    header.Filename,
		"contentType": contentType,
	}

	_, err = h.temporalClient.ExecuteWorkflow(r.Context(), workflowOptions, "TranscodeWorkflow", workflowParams)
	if err != nil {
		glog.Errorf("Failed to start transcoding workflow: %v", err)
		// Continue anyway - we'll handle failed transcoding later
	}

	// Create response
	response := VideoUploadResponse{
		VideoID:     videoID,
		Filename:    header.Filename,
		Size:        header.Size,
		ContentType: contentType,
		Status:      "uploaded",
		Message:     "Video uploaded successfully and scheduled for transcoding",
		Timestamp:   time.Now().Format(time.RFC3339),
	}

	// Return success response
	json.NewEncoder(w).Encode(response)
}

// Health check handler
func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","message":"Uploader service is healthy"}`))
}

// Helper functions
func handleError(w http.ResponseWriter, message string, err error, statusCode int) {
	errMsg := message
	if err != nil {
		errMsg = fmt.Sprintf("%s: %v", message, err)
		glog.Error(errMsg)
	}

	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{
		"error":   message,
		"details": errMsg,
	})
}

func isValidVideoType(contentType string) bool {
	validTypes := []string{
		"video/mp4",
		"video/quicktime",
		"video/x-msvideo",
		"video/x-ms-wmv",
		"video/x-matroska",
	}

	for _, valid := range validTypes {
		if contentType == valid {
			return true
		}
	}

	return false
}

func generateUniqueID() string {
	return fmt.Sprintf("%d-%s", time.Now().Unix(), strings.ReplaceAll(filepath.Base(filepath.Clean(os.TempDir())), " ", ""))
} 