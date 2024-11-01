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
	"strings"
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
	// Check file size (5MB limit)
	if file.Size > 5*1024*1024 {
		return errors.New("file too large (max 5MB)")
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

	// Add multipart form parsing before CSRF check
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		h.logger.Printf("File size error: %v", err)
		http.Error(w, "File too large (max 5MB)", http.StatusBadRequest)
		return
	}
	defer r.MultipartForm.RemoveAll()

	// Validate file size
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		h.logger.Printf("File size error: %v", err)
		http.Error(w, "File too large (max 5MB)", http.StatusBadRequest)
		return
	}
	defer r.MultipartForm.RemoveAll()

	// Get the file from form
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

	// Save the file and get the filename
	filename, err := h.saveImage(file, header)
	if err != nil {
		h.logger.Printf("Error saving file: %v", err)
		http.Error(w, "Failed to save image", http.StatusInternalServerError)
		return
	}

	// Update settings in database
	_, err = h.db.Exec(
		"INSERT OR REPLACE INTO settings (key, value) VALUES ('footer_image_url', ?)",
		filename,
	)
	if err != nil {
		h.logger.Printf("Error updating settings: %v", err)
		http.Error(w, "Failed to update settings", http.StatusInternalServerError)
		return
	}

	// Return the filename
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, filename)
}

func (h *ImageHandler) saveImage(file multipart.File, header *multipart.FileHeader) (string, error) {
	// Read file content
	content, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("error reading file: %w", err)
	}

	// Create unique filename
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
