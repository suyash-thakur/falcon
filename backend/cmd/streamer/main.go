package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/falcon/backend/internal/database"
	"github.com/falcon/backend/internal/storage"
	"github.com/go-redis/redis/v8"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
	"github.com/spf13/viper"
)

// VideoStreamInfo represents information about a video stream
type VideoStreamInfo struct {
	VideoID     string              `json:"videoId"`
	Title       string              `json:"title"`
	Duration    float64             `json:"duration"`
	Status      string              `json:"status"`
	Formats     []string            `json:"formats"`
	HLSMaster   string              `json:"hlsMaster,omitempty"`
	DASHMaster  string              `json:"dashMaster,omitempty"`
	Streams     []database.VideoStream `json:"streams"`
	CreatedAt   time.Time           `json:"createdAt"`
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

	// Set up database
	db, err := database.NewDatabaseFromConfig()
	if err != nil {
		glog.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Set up Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", viper.GetString("redis.host"), viper.GetInt("redis.port")),
		Password: viper.GetString("redis.password"),
		DB:       viper.GetInt("redis.db"),
	})
	defer redisClient.Close()

	// Ping Redis to verify connection
	_, err = redisClient.Ping(context.Background()).Result()
	if err != nil {
		glog.Fatalf("Failed to connect to Redis: %v", err)
	}

	// Create handlers with dependencies
	streamerHandler := &StreamerHandler{
		DB:      db,
		Storage: storageService,
		Redis:   redisClient,
	}

	// Define routes
	router.HandleFunc("/health", healthCheckHandler).Methods("GET")
	router.HandleFunc("/videos/{videoId}", streamerHandler.GetVideoInfo).Methods("GET")
	router.HandleFunc("/videos/{videoId}/hls/{filename}", streamerHandler.ServeHLSFile).Methods("GET")
	router.HandleFunc("/videos/{videoId}/dash/{filename}", streamerHandler.ServeDASHFile).Methods("GET")
	router.HandleFunc("/videos", streamerHandler.ListVideos).Methods("GET")

	// Add CORS middleware
	router.Use(corsMiddleware)

	// Set up server
	port := viper.GetString("server.port")
	if port == "" {
		port = "8002" // Different port from main and uploader services
	}

	srv := &http.Server{
		Handler:      router,
		Addr:         ":" + port,
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	// Run server
	glog.Infof("Streamer service starting on port %s", port)
	if err := srv.ListenAndServe(); err != nil {
		glog.Fatalf("Failed to start server: %v", err)
	}
}

// StreamerHandler handles video streaming requests
type StreamerHandler struct {
	DB      *database.Database
	Storage *storage.StorageService
	Redis   *redis.Client
}

// GetVideoInfo returns metadata about a video
func (h *StreamerHandler) GetVideoInfo(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	videoID := vars["videoId"]

	// Set response headers
	w.Header().Set("Content-Type", "application/json")

	// Get video from database
	video, err := h.DB.GetVideo(r.Context(), videoID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error retrieving video: %v", err), http.StatusNotFound)
		return
	}

	// Get video streams
	streams, err := h.DB.GetVideoStreams(r.Context(), videoID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error retrieving video streams: %v", err), http.StatusInternalServerError)
		return
	}

	// Build response
	var formats []string
	var hlsMaster, dashMaster string

	// Determine available formats and master playlist URLs
	for _, stream := range streams {
		if !contains(formats, stream.Format) {
			formats = append(formats, stream.Format)
		}

		if stream.Format == "hls" && stream.Resolution == "1920x1080" {
			// Generate signed URL for HLS master playlist
			url, err := h.Storage.GetSignedURL(r.Context(), fmt.Sprintf("videos/%s/hls/master.m3u8", videoID), 24*time.Hour)
			if err == nil {
				hlsMaster = url
			}
		} else if stream.Format == "dash" && stream.Resolution == "1920x1080" {
			// Generate signed URL for DASH manifest
			url, err := h.Storage.GetSignedURL(r.Context(), fmt.Sprintf("videos/%s/dash/manifest.mpd", videoID), 24*time.Hour)
			if err == nil {
				dashMaster = url
			}
		}
	}

	// Create response
	response := VideoStreamInfo{
		VideoID:    videoID,
		Title:      video.Title,
		Duration:   video.Duration,
		Status:     video.ProcessingState,
		Formats:    formats,
		HLSMaster:  hlsMaster,
		DASHMaster: dashMaster,
		Streams:    streams,
		CreatedAt:  video.CreatedAt,
	}

	// Return response
	json.NewEncoder(w).Encode(response)
}

// ServeHLSFile serves an HLS file (playlist or segment)
func (h *StreamerHandler) ServeHLSFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	videoID := vars["videoId"]
	filename := vars["filename"]

	// Determine the object key in storage
	objectKey := fmt.Sprintf("videos/%s/hls/%s", videoID, filename)

	// Check if the file is cached in Redis
	cacheKey := "hls:" + objectKey
	cachedContent, err := h.Redis.Get(r.Context(), cacheKey).Result()
	
	if err == nil && cachedContent != "" {
		// Serve from cache
		if isM3U8File(filename) {
			w.Header().Set("Content-Type", "application/x-mpegURL")
		} else {
			w.Header().Set("Content-Type", "video/MP2T")
		}
		w.Write([]byte(cachedContent))
		return
	}

	// Generate a signed URL for the file
	signedURL, err := h.Storage.GetSignedURL(r.Context(), objectKey, 1*time.Hour)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error generating URL: %v", err), http.StatusInternalServerError)
		return
	}

	// Redirect to the signed URL
	http.Redirect(w, r, signedURL, http.StatusTemporaryRedirect)
}

// ServeDASHFile serves a DASH file (manifest or segment)
func (h *StreamerHandler) ServeDASHFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	videoID := vars["videoId"]
	filename := vars["filename"]

	// Determine the object key in storage
	objectKey := fmt.Sprintf("videos/%s/dash/%s", videoID, filename)

	// Check if the file is cached in Redis
	cacheKey := "dash:" + objectKey
	cachedContent, err := h.Redis.Get(r.Context(), cacheKey).Result()
	
	if err == nil && cachedContent != "" {
		// Serve from cache
		if filename == "manifest.mpd" {
			w.Header().Set("Content-Type", "application/dash+xml")
		} else {
			w.Header().Set("Content-Type", "video/mp4")
		}
		w.Write([]byte(cachedContent))
		return
	}

	// Generate a signed URL for the file
	signedURL, err := h.Storage.GetSignedURL(r.Context(), objectKey, 1*time.Hour)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error generating URL: %v", err), http.StatusInternalServerError)
		return
	}

	// Redirect to the signed URL
	http.Redirect(w, r, signedURL, http.StatusTemporaryRedirect)
}

// ListVideos returns a paginated list of videos
func (h *StreamerHandler) ListVideos(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	limit := 10
	offset := 0

	if r.URL.Query().Get("limit") != "" {
		fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
	}

	if r.URL.Query().Get("offset") != "" {
		fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")

	// Get videos from database
	videos, err := h.DB.ListVideos(r.Context(), limit, offset)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error retrieving videos: %v", err), http.StatusInternalServerError)
		return
	}

	// Return response
	json.NewEncoder(w).Encode(map[string]interface{}{
		"videos": videos,
		"pagination": map[string]int{
			"limit":  limit,
			"offset": offset,
		},
	})
}

// Health check handler
func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","message":"Streamer service is healthy"}`))
}

// CORS middleware
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Helper function to check if a value is in a slice
func contains(slice []string, value string) bool {
	for _, item := range slice {
		if item == value {
			return true
		}
	}
	return false
}

// Helper function to check if a file is an M3U8 playlist
func isM3U8File(filename string) bool {
	return len(filename) > 5 && filename[len(filename)-5:] == ".m3u8"
} 