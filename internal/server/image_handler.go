// internal/server/image_handler.go
package server

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var ErrInvalidFileType = errors.New("invalid file type")

const (
	maxUploadSize = 5 << 20 // 5 MB
	imagesDir     = "web/static/images"
)

type ImageHandler struct {
	db        *sql.DB
	logger    *log.Logger
	uploadDir string
}

func NewImageHandler(db *sql.DB, logger *log.Logger) (*ImageHandler, error) {
	// Ensure upload directory exists
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create image directory: %w", err)
	}

	return &ImageHandler{
		db:        db,
		logger:    logger,
		uploadDir: imagesDir,
	}, nil
}

// ValidateFile checks if an uploaded file is allowed
func (h *ImageHandler) validateFile(file *multipart.FileHeader) error {
	// Check file size
	if file.Size > maxUploadSize {
		return fmt.Errorf("file too large (max %d MB)", maxUploadSize/(1<<20))
	}

	// Check extension
	ext := strings.ToLower(filepath.Ext(file.Filename))
	allowedExts := map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".gif":  true,
		".ico":  true,
	}
	if !allowedExts[ext] {
		return ErrInvalidFileType
	}

	// Check MIME type
	contentType := file.Header.Get("Content-Type")
	allowedTypes := map[string]bool{
		"image/jpeg":               true,
		"image/png":                true,
		"image/gif":                true,
		"image/x-icon":             true,
		"image/vnd.microsoft.icon": true,
	}
	if !allowedTypes[contentType] {
		return ErrInvalidFileType
	}

	return nil
}

func (h *ImageHandler) HandleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse multipart form with size limit
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		h.logger.Printf("File size error: %v", err)
		http.Error(w, fmt.Sprintf("File too large (max %d MB)", maxUploadSize/(1<<20)),
			http.StatusBadRequest)
		return
	}
	defer r.MultipartForm.RemoveAll()

	// Get the file
	file, header, err := r.FormFile("image")
	if err != nil {
		h.logger.Printf("Error getting file: %v", err)
		http.Error(w, "Invalid file upload", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate the file
	if err := h.validateFile(header); err != nil {
		h.logger.Printf("File validation error: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Save the file
	filename, err := h.saveImage(file, header)
	if err != nil {
		h.logger.Printf("Error saving file: %v", err)
		http.Error(w, "Failed to save image", http.StatusInternalServerError)
		return
	}

	// Start transaction to update settings
	tx, err := h.db.Begin()
	if err != nil {
		h.logger.Printf("Error starting transaction: %v", err)
		http.Error(w, "Failed to update settings", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Update settings
	_, err = tx.Exec(
		"INSERT OR REPLACE INTO settings (key, value, type) VALUES ('footer_image_url', ?, 'string')",
		filename,
	)
	if err != nil {
		h.logger.Printf("Error updating settings: %v", err)
		http.Error(w, "Failed to update settings", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		h.logger.Printf("Error committing transaction: %v", err)
		http.Error(w, "Failed to update settings", http.StatusInternalServerError)
		return
	}

	// Clean up old images (keep last 10)
	go h.cleanupOldImages()

	// Return the filename
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprint(w, filename)
}

func (h *ImageHandler) saveImage(file multipart.File, header *multipart.FileHeader) (string, error) {
	// Read file content
	content, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("error reading file: %w", err)
	}

	// Create unique filename based on content hash
	hash := sha256.Sum256(content)
	ext := strings.ToLower(filepath.Ext(header.Filename))
	filename := hex.EncodeToString(hash[:8]) + ext

	// Save file
	path := filepath.Join(h.uploadDir, filename)
	if err := os.WriteFile(path, content, 0644); err != nil {
		return "", fmt.Errorf("error writing file: %w", err)
	}

	return filename, nil
}

func (h *ImageHandler) cleanupOldImages() {
	// Get list of images ordered by modification time
	files, err := filepath.Glob(filepath.Join(h.uploadDir, "*"))
	if err != nil {
		h.logger.Printf("Error listing images during cleanup: %v", err)
		return
	}

	// Skip if we don't have enough files to clean up
	if len(files) <= 10 {
		return
	}

	// Sort files by modification time
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	fileInfos := make([]fileInfo, 0, len(files))
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		// Skip default.ico
		if filepath.Base(file) == "default.ico" {
			continue
		}
		fileInfos = append(fileInfos, fileInfo{file, info.ModTime()})
	}

	sort.Slice(fileInfos, func(i, j int) bool {
		return fileInfos[i].modTime.After(fileInfos[j].modTime)
	})

	// Remove old files
	for _, fi := range fileInfos[10:] {
		if err := os.Remove(fi.path); err != nil {
			h.logger.Printf("Error removing old image %s: %v", fi.path, err)
		}
	}
}
