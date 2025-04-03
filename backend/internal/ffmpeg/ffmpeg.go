package ffmpeg

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/golang/glog"
)

// FFmpeg represents an FFmpeg processor
type FFmpeg struct {
	BinaryPath string
	ThreadCount int
	Preset string
}

// NewFFmpeg creates a new FFmpeg processor
func NewFFmpeg(binaryPath string, threadCount int, preset string) *FFmpeg {
	return &FFmpeg{
		BinaryPath: binaryPath,
		ThreadCount: threadCount,
		Preset: preset,
	}
}

// TranscodeToHLS transcodes a video file to HLS format with multiple resolutions
func (f *FFmpeg) TranscodeToHLS(inputFile, outputDir, segmentFilename string, resolutions []Resolution) error {
	args := []string{
		"-i", inputFile,
		"-threads", fmt.Sprintf("%d", f.ThreadCount),
	}

	var variantArgs []string
	var masterPlaylistContent strings.Builder
	masterPlaylistContent.WriteString("#EXTM3U\n")
	masterPlaylistContent.WriteString("#EXT-X-VERSION:3\n")

	// Add each resolution variant
	for i, res := range resolutions {
		variantName := fmt.Sprintf("v%d", i)
		playlistFile := fmt.Sprintf("%s_%s.m3u8", segmentFilename, variantName)
		segmentFile := fmt.Sprintf("%s_%s_%%03d.ts", segmentFilename, variantName)
		
		masterPlaylistContent.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d\n", 
			res.Bitrate, res.Width, res.Height))
		masterPlaylistContent.WriteString(fmt.Sprintf("%s\n", playlistFile))
		
		// Add variant arguments
		variantArgs = append(variantArgs,
			"-map", "0:v:0",
			"-map", "0:a:0",
			"-c:v", "libx264",
			"-preset", f.Preset,
			"-b:v", res.Bitrate,
			"-s", fmt.Sprintf("%dx%d", res.Width, res.Height),
			"-c:a", "aac",
			"-b:a", "128k",
			"-hls_time", "10",
			"-hls_list_size", "0",
			"-hls_segment_filename", fmt.Sprintf("%s/%s", outputDir, segmentFile),
			fmt.Sprintf("%s/%s", outputDir, playlistFile),
		)
	}

	// Execute the FFmpeg command
	args = append(args, variantArgs...)
	
	cmd := exec.Command(f.BinaryPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	
	glog.Infof("Executing FFmpeg command: %s %s", f.BinaryPath, strings.Join(args, " "))
	
	err := cmd.Run()
	if err != nil {
		glog.Errorf("FFmpeg error: %v\nStderr: %s", err, stderr.String())
		return fmt.Errorf("failed to transcode: %v - %s", err, stderr.String())
	}
	
	glog.Infof("FFmpeg output: %s", stdout.String())
	
	// Create master playlist file
	// In a real implementation, you would write the masterPlaylistContent to a file
	
	return nil
}

// Resolution represents a video resolution and bitrate
type Resolution struct {
	Width  int
	Height int
	Bitrate string // e.g., "2500k"
}

// GetMediaInfo returns information about a media file
func (f *FFmpeg) GetMediaInfo(inputFile string) (map[string]string, error) {
	cmd := exec.Command(f.BinaryPath, 
		"-i", inputFile,
		"-hide_banner",
	)
	
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	
	_ = cmd.Run() // FFmpeg will return an error code, but we still want the output
	
	info := make(map[string]string)
	output := stderr.String()
	
	// Parse the output
	// In a real implementation, you would properly parse the FFmpeg output
	
	glog.Infof("Media info for %s: %v", inputFile, info)
	
	return info, nil
} 