package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	maxBytes := 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxBytes))

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading video for video", videoID, "by user", userID)

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get video", err)
		return
	}

	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "User is not the owner", err)
		return
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable parse media type", err)
		return
	}

	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Wrong file format (needs mp4)", err)
		return
	}

	outputFile, err := os.CreateTemp("", "tubely-upload.mp4")
	defer os.Remove(outputFile.Name())
	defer outputFile.Close()
	_, err = io.Copy(outputFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to save file", err)
		return
	}

	outputFile.Seek(0, io.SeekStart)

	videoRatio, err := getVideoAspectRatio(outputFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get aspect ratio", err)
		return
	}
	var ratioPrefix string
	switch videoRatio {
	case "16:9":
		ratioPrefix = "landscape"
	case "9:16":
		ratioPrefix = "portrait"
	default:
		ratioPrefix = "other"
	}

	fileFormat := strings.Split(mediaType, "/")[1]
	bytes := make([]byte, 32)
	rand.Read(bytes)
	fileName := base64.RawURLEncoding.EncodeToString(bytes)
	fileNameWithExt := fmt.Sprintf("%s/%s.%s", ratioPrefix, fileName, fileFormat)

	cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &fileNameWithExt,
		Body:        outputFile,
		ContentType: &mediaType,
	})

	newURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, fileNameWithExt)
	video.VideoURL = &newURL

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video", err)
		return
	}

}

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
