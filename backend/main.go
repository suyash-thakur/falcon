package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/falcon/backend/internal/database"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
	"github.com/spf13/viper"
)

// Service represents a backend service
type Service struct {
	Name         string
	Command      string
	Args         []string
	Cmd          *exec.Cmd
	StdoutLogger *log.Logger
	StderrLogger *log.Logger
}

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
	// Set up router for API
	router := mux.NewRouter()

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

	// Define API routes
	router.HandleFunc("/health", healthCheckHandler).Methods("GET")
	router.HandleFunc("/info", infoHandler).Methods("GET")

	// Define services to start
	services := []*Service{
		{
			Name:         "uploader",
			Command:      "go",
			Args:         []string{"run", "cmd/uploader/main.go"},
			StdoutLogger: log.New(os.Stdout, "UPLOADER: ", log.LstdFlags),
			StderrLogger: log.New(os.Stderr, "UPLOADER ERR: ", log.LstdFlags),
		},
		{
			Name:         "transcoder",
			Command:      "go",
			Args:         []string{"run", "cmd/transcoder/main.go"},
			StdoutLogger: log.New(os.Stdout, "TRANSCODER: ", log.LstdFlags),
			StderrLogger: log.New(os.Stderr, "TRANSCODER ERR: ", log.LstdFlags),
		},
		{
			Name:         "streamer",
			Command:      "go",
			Args:         []string{"run", "cmd/streamer/main.go"},
			StdoutLogger: log.New(os.Stdout, "STREAMER: ", log.LstdFlags),
			StderrLogger: log.New(os.Stderr, "STREAMER ERR: ", log.LstdFlags),
		},
	}

	// Start services
	startServices(services)
	defer stopServices(services)

	// Set up server
	port := viper.GetString("server.port")
	if port == "" {
		port = "8000"
	}

	srv := &http.Server{
		Handler:      router,
		Addr:         ":" + port,
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	// Run server in a goroutine
	go func() {
		glog.Infof("Main server starting on port %s", port)
		if err := srv.ListenAndServe(); err != nil {
			glog.Errorf("Server error: %v", err)
		}
	}()

	// Set up graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	// Block until signal is received
	<-c
	glog.Info("Shutting down...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		glog.Errorf("Server shutdown failed: %v", err)
	}

	glog.Info("Server stopped")
}

// startServices starts all services in goroutines
func startServices(services []*Service) {
	for _, service := range services {
		go startService(service)
	}
}

// startService starts a single service and handles its output
func startService(service *Service) {
	glog.Infof("Starting %s service", service.Name)

	service.Cmd = exec.Command(service.Command, service.Args...)

	// Set up pipes for stdout and stderr
	stdout, err := service.Cmd.StdoutPipe()
	if err != nil {
		glog.Errorf("Failed to get stdout pipe for %s: %v", service.Name, err)
		return
	}

	stderr, err := service.Cmd.StderrPipe()
	if err != nil {
		glog.Errorf("Failed to get stderr pipe for %s: %v", service.Name, err)
		return
	}

	// Start the service
	if err := service.Cmd.Start(); err != nil {
		glog.Errorf("Failed to start %s: %v", service.Name, err)
		return
	}

	// Process output in goroutines
	go processOutput(stdout, service.StdoutLogger)
	go processOutput(stderr, service.StderrLogger)

	// Wait for the service to complete
	if err := service.Cmd.Wait(); err != nil {
		if strings.Contains(err.Error(), "killed") {
			glog.Infof("%s service was stopped", service.Name)
		} else {
			glog.Errorf("%s service exited with error: %v", service.Name, err)
		}
	} else {
		glog.Infof("%s service completed", service.Name)
	}
}

// processOutput reads from a pipe and writes to a logger
func processOutput(pipe io.ReadCloser, logger *log.Logger) {
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		logger.Println(scanner.Text())
	}
}

// stopServices stops all services
func stopServices(services []*Service) {
	var wg sync.WaitGroup

	for _, service := range services {
		if service.Cmd != nil && service.Cmd.Process != nil {
			wg.Add(1)
			go func(s *Service) {
				defer wg.Done()
				glog.Infof("Stopping %s service", s.Name)
				if err := s.Cmd.Process.Kill(); err != nil {
					glog.Errorf("Failed to stop %s: %v", s.Name, err)
				}
			}(service)
		}
	}

	wg.Wait()
	glog.Info("All services stopped")
}

// Health check handler
func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","message":"Service is healthy"}`))
}

// Info handler returns information about the service
func infoHandler(w http.ResponseWriter, r *http.Request) {
	info := map[string]interface{}{
		"name":        "Falcon Video Platform",
		"version":     "1.0.0",
		"description": "Open Source Video on Demand Platform",
		"endpoints": []string{
			"/health",
			"/info",
			"/upload",
			"/videos",
			"/videos/{id}",
			"/videos/{id}/hls/{filename}",
			"/videos/{id}/dash/{filename}",
		},
		"services": []string{
			"uploader",
			"transcoder",
			"streamer",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(info)
}
