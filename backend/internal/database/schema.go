package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/spf13/viper"
)

// DbConfig represents database connection configuration
type DbConfig struct {
	Host           string
	Port           int
	User           string
	Password       string
	DbName         string
	SSLMode        string
	MaxConnections int
}

// Database represents a database connection
type Database struct {
	pool *pgxpool.Pool
}

// Video represents a video in the database
type Video struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	OriginalName    string    `json:"original_name"`
	OriginalPath    string    `json:"original_path"`
	ProcessingState string    `json:"processing_state"`
	Duration        float64   `json:"duration"`
	Size            int64     `json:"size"`
	ContentType     string    `json:"content_type"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// VideoStream represents a transcoded video stream
type VideoStream struct {
	ID          string    `json:"id"`
	VideoID     string    `json:"video_id"`
	Resolution  string    `json:"resolution"`
	Bitrate     string    `json:"bitrate"`
	Format      string    `json:"format"`
	Path        string    `json:"path"`
	Size        int64     `json:"size"`
	SegmentSize int       `json:"segment_size"`
	CreatedAt   time.Time `json:"created_at"`
}

// NewDatabase creates a new database connection
func NewDatabase(config DbConfig) (*Database, error) {
	connString := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		config.User, config.Password, config.Host, config.Port, config.DbName, config.SSLMode,
	)

	poolConfig, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("unable to parse connection string: %v", err)
	}

	poolConfig.MaxConns = int32(config.MaxConnections)

	pool, err := pgxpool.ConnectConfig(context.Background(), poolConfig)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to database: %v", err)
	}

	return &Database{pool: pool}, nil
}

// NewDatabaseFromConfig creates a new database connection from Viper config
func NewDatabaseFromConfig() (*Database, error) {
	config := DbConfig{
		Host:           viper.GetString("database.host"),
		Port:           viper.GetInt("database.port"),
		User:           viper.GetString("database.user"),
		Password:       viper.GetString("database.password"),
		DbName:         viper.GetString("database.dbname"),
		SSLMode:        viper.GetString("database.sslmode"),
		MaxConnections: viper.GetInt("database.max_connections"),
	}

	return NewDatabase(config)
}

// Close closes the database connection
func (db *Database) Close() {
	db.pool.Close()
}

// Pool returns the underlying connection pool
func (db *Database) Pool() *pgxpool.Pool {
	return db.pool
}

// CreateTables creates all required database tables
func (db *Database) CreateTables(ctx context.Context) error {
	// Create videos table
	_, err := db.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS videos (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			original_name TEXT NOT NULL,
			original_path TEXT NOT NULL,
			processing_state TEXT NOT NULL,
			duration FLOAT DEFAULT 0,
			size BIGINT NOT NULL,
			content_type TEXT NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create videos table: %v", err)
	}

	// Create video_streams table
	_, err = db.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS video_streams (
			id TEXT PRIMARY KEY,
			video_id TEXT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
			resolution TEXT NOT NULL,
			bitrate TEXT NOT NULL,
			format TEXT NOT NULL,
			path TEXT NOT NULL,
			size BIGINT DEFAULT 0,
			segment_size INTEGER DEFAULT 0,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			UNIQUE(video_id, resolution, format)
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create video_streams table: %v", err)
	}

	return nil
}

// CreateVideo adds a new video to the database
func (db *Database) CreateVideo(ctx context.Context, video *Video) error {
	_, err := db.pool.Exec(ctx, `
		INSERT INTO videos (
			id, title, original_name, original_path, processing_state, 
			size, content_type, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`,
		video.ID,
		video.Title,
		video.OriginalName,
		video.OriginalPath,
		video.ProcessingState,
		video.Size,
		video.ContentType,
		video.CreatedAt,
		video.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to insert video: %v", err)
	}

	return nil
}

// UpdateVideoStatus updates the processing state of a video
func (db *Database) UpdateVideoStatus(ctx context.Context, videoID, status string) error {
	_, err := db.pool.Exec(ctx, `
		UPDATE videos 
		SET processing_state = $1, updated_at = NOW() 
		WHERE id = $2
	`, status, videoID)

	if err != nil {
		return fmt.Errorf("failed to update video status: %v", err)
	}

	return nil
}

// GetVideo retrieves a video by ID
func (db *Database) GetVideo(ctx context.Context, videoID string) (*Video, error) {
	video := &Video{}

	err := db.pool.QueryRow(ctx, `
		SELECT 
			id, title, original_name, original_path, processing_state,
			duration, size, content_type, created_at, updated_at
		FROM videos
		WHERE id = $1
	`, videoID).Scan(
		&video.ID,
		&video.Title,
		&video.OriginalName,
		&video.OriginalPath,
		&video.ProcessingState,
		&video.Duration,
		&video.Size,
		&video.ContentType,
		&video.CreatedAt,
		&video.UpdatedAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("video not found: %s", videoID)
		}
		return nil, fmt.Errorf("failed to get video: %v", err)
	}

	return video, nil
}

// AddVideoStream adds a transcoded stream for a video
func (db *Database) AddVideoStream(ctx context.Context, stream *VideoStream) error {
	_, err := db.pool.Exec(ctx, `
		INSERT INTO video_streams (
			id, video_id, resolution, bitrate, format, path, size, segment_size, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (video_id, resolution, format) 
		DO UPDATE SET 
			path = EXCLUDED.path,
			size = EXCLUDED.size,
			segment_size = EXCLUDED.segment_size
	`,
		stream.ID,
		stream.VideoID,
		stream.Resolution,
		stream.Bitrate,
		stream.Format,
		stream.Path,
		stream.Size,
		stream.SegmentSize,
		stream.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to insert video stream: %v", err)
	}

	return nil
}

// GetVideoStreams retrieves all streams for a video
func (db *Database) GetVideoStreams(ctx context.Context, videoID string) ([]*VideoStream, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT 
			id, video_id, resolution, bitrate, format, path, size, segment_size, created_at
		FROM video_streams
		WHERE video_id = $1
		ORDER BY resolution DESC
	`, videoID)
	if err != nil {
		return nil, fmt.Errorf("failed to query video streams: %v", err)
	}
	defer rows.Close()

	var streams []*VideoStream
	for rows.Next() {
		stream := &VideoStream{}
		err := rows.Scan(
			&stream.ID,
			&stream.VideoID,
			&stream.Resolution,
			&stream.Bitrate,
			&stream.Format,
			&stream.Path,
			&stream.Size,
			&stream.SegmentSize,
			&stream.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan video stream: %v", err)
		}
		streams = append(streams, stream)
	}

	return streams, nil
}

// ListVideos retrieves a list of videos with pagination
func (db *Database) ListVideos(ctx context.Context, limit, offset int) ([]*Video, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT 
			id, title, original_name, original_path, processing_state,
			duration, size, content_type, created_at, updated_at
		FROM videos
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query videos: %v", err)
	}
	defer rows.Close()

	var videos []*Video
	for rows.Next() {
		video := &Video{}
		err := rows.Scan(
			&video.ID,
			&video.Title,
			&video.OriginalName,
			&video.OriginalPath,
			&video.ProcessingState,
			&video.Duration,
			&video.Size,
			&video.ContentType,
			&video.CreatedAt,
			&video.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan video: %v", err)
		}
		videos = append(videos, video)
	}

	return videos, nil
} 