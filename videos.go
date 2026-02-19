package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
)

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	buffer := &bytes.Buffer{}
	cmd.Stdout = buffer
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("Could not run command: %v", err)
	}

	type videoData struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}

	data := videoData{}
	err = json.Unmarshal(buffer.Bytes(), &data)
	if err != nil {
		return "", fmt.Errorf("Could not read video data: %v", err)
	}

	width := data.Streams[0].Width
	height := data.Streams[0].Height

	aspect := float32(width) / float32(height)
	var aspectString string
	if aspect > 1.5 && aspect < 2 {
		aspectString = "16:9"
	} else if aspect < 0.6 && aspect > 0.5 {
		aspectString = "9:16"
	} else {
		aspectString = "other"
	}

	return aspectString, nil
}

func processVideoForFastStart(filePath string) (string, error) {
	outputFilePath := filePath + ".processing"
	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputFilePath)
	buffer := &bytes.Buffer{}
	cmd.Stdout = buffer
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("Could not run command: %v", err)
	}

	return outputFilePath, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	if video.VideoURL == nil {
		return video, nil
	}
	videoURLParts := strings.Split(*video.VideoURL, ",")
	if len(videoURLParts) != 2 {
		return database.Video{}, fmt.Errorf("Video URL not containing bucket and key")
	}
	presignedURL, err := generatePresignedURL(cfg.s3Client, videoURLParts[0], videoURLParts[1], time.Hour)
	if err != nil {
		return database.Video{}, err
	}
	video.VideoURL = &presignedURL
	return video, nil
}
