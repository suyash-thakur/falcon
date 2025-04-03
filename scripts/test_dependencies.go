package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/go-redis/redis/v8"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/spf13/viper"
	"go.temporal.io/sdk/client"
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
	fmt.Println("Testing backend dependencies...")

	// Test FFmpeg availability
	testFFmpeg()

	// Test PostgreSQL connection
	testPostgreSQL()

	// Test Redis connection
	testRedis()

	// Test MinIO connection
	testMinIO()

	// Test Temporal connection if not skipped
	if os.Getenv("SKIP_TEMPORAL") != "true" {
		testTemporal()
	} else {
		fmt.Println("\n5. Skipping Temporal test (SKIP_TEMPORAL=true)")
	}

	fmt.Println("\nAll dependencies are working correctly!")
}

// Test if FFmpeg is installed and working
func testFFmpeg() {
	fmt.Println("\n1. Testing FFmpeg...")
	
	cmd := exec.Command("ffmpeg", "-version")
	output, err := cmd.CombinedOutput()
	
	if err != nil {
		log.Fatalf("FFmpeg test failed: %v", err)
	}
	
	fmt.Printf("✅ FFmpeg is installed: %s\n", output[:50])
}

// Test PostgreSQL connection
func testPostgreSQL() {
	fmt.Println("\n2. Testing PostgreSQL connection...")

	connString := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		viper.GetString("database.user"),
		viper.GetString("database.password"),
		viper.GetString("database.host"),
		viper.GetInt("database.port"),
		viper.GetString("database.dbname"),
		viper.GetString("database.sslmode"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.Connect(ctx, connString)
	if err != nil {
		log.Fatalf("PostgreSQL connection failed: %v", err)
	}
	defer pool.Close()

	// Test query
	var version string
	err = pool.QueryRow(ctx, "SELECT version()").Scan(&version)
	if err != nil {
		log.Fatalf("PostgreSQL query failed: %v", err)
	}

	fmt.Printf("✅ PostgreSQL is connected: %s\n", version[:50])
}

// Test Redis connection
func testRedis() {
	fmt.Println("\n3. Testing Redis connection...")

	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", viper.GetString("redis.host"), viper.GetInt("redis.port")),
		Password: viper.GetString("redis.password"),
		DB:       viper.GetInt("redis.db"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pong, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Redis connection failed: %v", err)
	}

	fmt.Printf("✅ Redis is connected: %s\n", pong)
}

// Test MinIO connection
func testMinIO() {
	fmt.Println("\n4. Testing MinIO connection...")

	// Create custom S3 session
	s3Config := &aws.Config{
		Credentials:      credentials.NewStaticCredentials(
			viper.GetString("storage.access_key"),
			viper.GetString("storage.secret_key"),
			"",
		),
		Endpoint:         aws.String(viper.GetString("storage.endpoint")),
		Region:           aws.String(viper.GetString("storage.region")),
		DisableSSL:       aws.Bool(!viper.GetBool("storage.use_ssl")),
		S3ForcePathStyle: aws.Bool(true), // Required for MinIO
	}

	sess, err := session.NewSession(s3Config)
	if err != nil {
		log.Fatalf("MinIO session creation failed: %v", err)
	}

	// Create S3 client
	s3Client := s3.New(sess)

	// Test listing buckets
	result, err := s3Client.ListBuckets(&s3.ListBucketsInput{})
	if err != nil {
		log.Fatalf("MinIO bucket listing failed: %v", err)
	}

	// Ensure the videos bucket exists, create it if not
	bucket := viper.GetString("storage.bucket")
	bucketExists := false
	
	for _, b := range result.Buckets {
		if *b.Name == bucket {
			bucketExists = true
			break
		}
	}

	if !bucketExists {
		fmt.Printf("Creating bucket '%s'...\n", bucket)
		_, err = s3Client.CreateBucket(&s3.CreateBucketInput{
			Bucket: aws.String(bucket),
		})
		if err != nil {
			log.Fatalf("MinIO bucket creation failed: %v", err)
		}
	}

	fmt.Printf("✅ MinIO is connected, found %d buckets\n", len(result.Buckets))
}

// Test Temporal connection
func testTemporal() {
	fmt.Println("\n5. Testing Temporal connection...")

	// Create Temporal client
	c, err := client.NewClient(client.Options{
		HostPort: viper.GetString("temporal.host") + ":" + viper.GetString("temporal.port"),
	})
	
	if err != nil {
		log.Fatalf("Temporal client creation failed: %v", err)
	}
	defer c.Close()

	// Just verify we can connect to Temporal
	fmt.Printf("✅ Temporal client connected successfully to %s:%s\n", 
		viper.GetString("temporal.host"), 
		viper.GetString("temporal.port"))
} 