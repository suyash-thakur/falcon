package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/falcon/backend/internal/database"
	"github.com/spf13/viper"
)

func init() {
	// Initialize configuration
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("../")
	viper.AddConfigPath(".")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("Error reading config file: %s", err)
	}
}

func main() {
	// Parse command line flags
	reset := flag.Bool("reset", false, "Drop and recreate all tables")
	seed := flag.Bool("seed", false, "Seed the database with sample data")
	flag.Parse()

	// Set up database
	db, err := database.NewDatabaseFromConfig()
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Reset database if specified
	if *reset {
		log.Println("Dropping all tables...")
		if err := dropTables(ctx, db); err != nil {
			log.Fatalf("Failed to drop tables: %v", err)
		}
	}

	// Create tables
	log.Println("Creating tables...")
	if err := db.CreateTables(ctx); err != nil {
		log.Fatalf("Failed to create tables: %v", err)
	}

	// Seed database if specified
	if *seed {
		log.Println("Seeding database...")
		if err := seedDatabase(ctx, db); err != nil {
			log.Fatalf("Failed to seed database: %v", err)
		}
	}

	log.Println("Migration completed successfully")
}

// dropTables drops all tables in the database
func dropTables(ctx context.Context, db *database.Database) error {
	// This is a simplified implementation - in a real system, you would handle constraints and foreign keys
	_, err := db.Pool().Exec(ctx, `
		DROP TABLE IF EXISTS video_streams CASCADE;
		DROP TABLE IF EXISTS videos CASCADE;
	`)
	return err
}

// seedDatabase adds sample data to the database
func seedDatabase(ctx context.Context, db *database.Database) error {
	// Add a sample video
	now := time.Now()
	sampleVideo := &database.Video{
		ID:              "sample-video-1",
		Title:           "Sample Video",
		OriginalName:    "sample.mp4",
		OriginalPath:    "uploads/sample-video-1/sample.mp4",
		ProcessingState: "completed",
		Duration:        120.5,
		Size:            15728640, // 15MB
		ContentType:     "video/mp4",
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := db.CreateVideo(ctx, sampleVideo); err != nil {
		return err
	}

	// Add sample streams
	resolutions := []struct {
		width      int
		height     int
		bitrate    string
		resolution string
	}{
		{1920, 1080, "5000k", "1920x1080"},
		{1280, 720, "2500k", "1280x720"},
		{854, 480, "1000k", "854x480"},
		{640, 360, "500k", "640x360"},
	}

	for i, res := range resolutions {
		stream := &database.VideoStream{
			ID:          sampleVideo.ID + "-v" + string(i+'0'),
			VideoID:     sampleVideo.ID,
			Resolution:  res.resolution,
			Bitrate:     res.bitrate,
			Format:      "hls",
			Path:        "videos/sample-video-1/hls/sample-video-1_v" + string(i+'0') + ".m3u8",
			Size:        int64(5000000 - i*1000000), // Decreasing size for lower resolutions
			SegmentSize: 10,
			CreatedAt:   now,
		}

		if err := db.AddVideoStream(ctx, stream); err != nil {
			return err
		}
	}

	return nil
} 