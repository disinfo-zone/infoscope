// internal/server/image_handler.go
package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
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
	maxUploadSize      = 5 << 20 // 5 MB
	maxFaviconSize     = 1 << 20 // 1 MB
	defaultFaviconName = "default.ico"
)

type ImageHandler struct {
	db             *sql.DB
	logger         *log.Logger
	csrf           *CSRF
	uploadDir      string
	productionMode bool
}

func NewImageHandler(db *sql.DB, logger *log.Logger, csrf *CSRF, baseUploadDir string, productionMode bool) (*ImageHandler, error) {
	if err := os.MkdirAll(baseUploadDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base image directory %s: %w", baseUploadDir, err)
	}
	faviconDir := filepath.Join(baseUploadDir, "favicon")
	if err := os.MkdirAll(faviconDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create favicon directory %s: %w", faviconDir, err)
	}
	return &ImageHandler{
		db:             db,
		logger:         logger,
		csrf:           csrf,
		uploadDir:      baseUploadDir,
		productionMode: productionMode,
	}, nil
}

func (h *ImageHandler) validateFile(file multipart.File, header *multipart.FileHeader, allowedMIMETypes map[string]bool) (string, error) {
	if header.Size > maxUploadSize {
		return "", fmt.Errorf("file too large (max %d MB)", maxUploadSize/(1<<20))
	}
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("failed to read file for content type detection: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("failed to rewind file after reading for content type detection: %w", err)
	}
	detectedContentType := http.DetectContentType(buffer[:n])
	if !allowedMIMETypes[detectedContentType] {
		h.logger.Printf("Invalid content type detected: '%s' for file '%s' (client-sent: '%s'). Allowed: %v",
			detectedContentType, header.Filename, header.Header.Get("Content-Type"), allowedMIMETypes)
		return "", ErrInvalidFileType
	}
	return detectedContentType, nil
}

func (h *ImageHandler) isValidFavicon(file multipart.File, header *multipart.FileHeader) (bool, error) {
	if header.Size > maxFaviconSize {
		return false, fmt.Errorf("favicon too large (max %d MB)", maxFaviconSize/(1<<20))
	}
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("failed to read favicon for content type detection: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return false, fmt.Errorf("failed to rewind favicon file: %w", err)
	}
	detectedContentType := http.DetectContentType(buffer[:n])
	allowedFaviconTypes := map[string]bool{
		"image/png":                true,
		"image/x-icon":             true,
		"image/vnd.microsoft.icon": true,
	}
	if !allowedFaviconTypes[detectedContentType] {
		// Some ICO files may be misdetected by sniffing (e.g., as image/gif).
		// Also, the client may send a correct favicon Content-Type in header.
		clientCT := header.Header.Get("Content-Type")
		if strings.EqualFold(filepath.Ext(header.Filename), ".ico") && (detectedContentType == "image/gif" || clientCT == "image/x-icon" || clientCT == "image/vnd.microsoft.icon") {
			if !h.productionMode {
				h.logger.Printf("Warning: favicon '%s' detected as %s but headers/ext indicate ICO. Allowing upload.", header.Filename, detectedContentType)
			}
			return true, nil
		}
		h.logger.Printf("Invalid favicon content type detected: '%s' for file '%s'. Allowed: image/png, image/x-icon, image/vnd.microsoft.icon",
			detectedContentType, header.Filename)
		return false, nil
	}
	return true, nil
}

func (h *ImageHandler) HandleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.csrf.Validate(w, r) {
		h.logger.Printf("CSRF validation failed for HandleUpload")
		RespondWithError(w, http.StatusForbidden, "Invalid CSRF token")
		return
	}
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		http.Error(w, fmt.Sprintf("File too large (max %d MB)", maxUploadSize/(1<<20)), http.StatusBadRequest)
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "Invalid file upload", http.StatusBadRequest)
		return
	}
	defer file.Close()
	allowedMetaTypes := map[string]bool{
		"image/jpeg": true,
		"image/png":  true,
		"image/gif":  true,
		"image/webp": true,
	}
	if _, err := h.validateFile(file, header, allowedMetaTypes); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "Failed to process image (rewind error)", http.StatusInternalServerError)
		return
	}
	savedFilename, err := h.saveImage(file, header, h.uploadDir)
	if err != nil {
		http.Error(w, "Failed to save image", http.StatusInternalServerError)
		return
	}
	if err := h.updateSettingKey(r.Context(), "footer_image_url", savedFilename); err != nil {
		http.Error(w, "Failed to update settings", http.StatusInternalServerError)
		return
	}
	go h.cleanupOldImages(h.uploadDir, filepath.Base(savedFilename), 10)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"filename": savedFilename})
}

func (h *ImageHandler) saveImage(file multipart.File, header *multipart.FileHeader, directory string) (string, error) {
	content, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("error reading file: %w", err)
	}
	hash := sha256.Sum256(content)
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		ext = ".png"
	}
	filename := hex.EncodeToString(hash[:16]) + ext
	path := filepath.Join(directory, filename)
	if err := os.WriteFile(path, content, 0644); err != nil {
		return "", fmt.Errorf("error writing file to %s: %w", path, err)
	}
	if !h.productionMode {
		h.logger.Printf("Saved image %s to %s", header.Filename, path)
	}
	return filename, nil
}

func (h *ImageHandler) cleanupOldImages(directory string, currentImageFilename string, numToKeep int) {
	files, err := os.ReadDir(directory)
	if err != nil {
		h.logger.Printf("Error listing images in %s during cleanup: %v", directory, err)
		return
	}
	if len(files) <= numToKeep {
		return
	}
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	var imageFiles []fileInfo
	for _, entry := range files {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || entry.Name() == defaultFaviconName || entry.Name() == currentImageFilename {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			h.logger.Printf("Error getting info for file %s in %s: %v", entry.Name(), directory, err)
			continue
		}
		imageFiles = append(imageFiles, fileInfo{filepath.Join(directory, entry.Name()), info.ModTime()})
	}
	if len(imageFiles) <= numToKeep {
		return
	}
	sort.Slice(imageFiles, func(i, j int) bool {
		return imageFiles[i].modTime.After(imageFiles[j].modTime)
	})
	for i := numToKeep; i < len(imageFiles); i++ {
		fi := imageFiles[i]
		if err := os.Remove(fi.path); err != nil {
			h.logger.Printf("Error removing old image %s: %v", fi.path, err)
		} else {
			if !h.productionMode {
				h.logger.Printf("Cleaned up old image: %s", fi.path)
			}
		}
	}
}

func (h *ImageHandler) HandleFaviconUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.csrf.Validate(w, r) {
		h.logger.Printf("CSRF validation failed for favicon upload")
		RespondWithError(w, http.StatusForbidden, "Invalid CSRF token")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFaviconSize)
	if err := r.ParseMultipartForm(maxFaviconSize); err != nil {
		http.Error(w, fmt.Sprintf("Favicon too large or form parsing error (max %d MB)", maxFaviconSize/(1<<20)), http.StatusBadRequest)
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("favicon")
	if err != nil {
		http.Error(w, "Invalid file upload for favicon", http.StatusBadRequest)
		return
	}
	defer file.Close()
	valid, err := h.isValidFavicon(file, header)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !valid {
		http.Error(w, "Invalid file type. Must be ICO or PNG.", http.StatusBadRequest)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "Failed to process image (rewind error)", http.StatusInternalServerError)
		return
	}
	faviconDir := filepath.Join(h.uploadDir, "favicon")
	if err := os.MkdirAll(faviconDir, 0755); err != nil {
		h.logger.Printf("Failed to create favicon directory %s: %v", faviconDir, err)
		http.Error(w, "Failed to save favicon (dir error)", http.StatusInternalServerError)
		return
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".ico" && ext != ".png" {
		ext = ".png"
	}
	savedFilename, err := h.saveImage(file, header, faviconDir)
	if err != nil {
		http.Error(w, "Failed to save favicon", http.StatusInternalServerError)
		return
	}
	faviconURLPath := "favicon/" + savedFilename
	if err := h.updateSettingKey(r.Context(), "favicon_url", faviconURLPath); err != nil {
		http.Error(w, "Failed to update settings for favicon", http.StatusInternalServerError)
		return
	}
	go h.cleanupOldImages(faviconDir, filepath.Base(savedFilename), 5)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"filename": faviconURLPath})
}

func (h *ImageHandler) HandleMetaImageUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.csrf.Validate(w, r) {
		h.logger.Printf("CSRF validation failed for meta image upload")
		RespondWithError(w, http.StatusForbidden, "Invalid CSRF token")
		return
	}
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		http.Error(w, fmt.Sprintf("File too large (max %d MB)", maxUploadSize/(1<<20)), http.StatusBadRequest)
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "Invalid file upload for meta image", http.StatusBadRequest)
		return
	}
	defer file.Close()
	allowedMetaTypes := map[string]bool{
		"image/jpeg": true,
		"image/png":  true,
		"image/gif":  true,
		"image/webp": true,
	}
	if _, err := h.validateFile(file, header, allowedMetaTypes); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "Failed to process image (rewind error)", http.StatusInternalServerError)
		return
	}
	savedFilename, err := h.saveImage(file, header, h.uploadDir)
	if err != nil {
		http.Error(w, "Failed to save meta image", http.StatusInternalServerError)
		return
	}
	if err := h.updateSettingKey(r.Context(), "meta_image_url", savedFilename); err != nil {
		http.Error(w, "Failed to update settings for meta image", http.StatusInternalServerError)
		return
	}
	go h.cleanupOldImages(h.uploadDir, filepath.Base(savedFilename), 10)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"filename": savedFilename})
}

// updateSettingKey is a helper to update a specific setting key in the DB.
func (h *ImageHandler) updateSettingKey(ctx context.Context, key, value string) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		// Log the error before returning it, ensuring context is captured.
		h.logger.Printf("Error starting transaction for setting key %s: %v", key, err)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Use defer for rollback

	_, err = tx.ExecContext(ctx,
		"INSERT OR REPLACE INTO settings (key, value, type) VALUES (?, ?, 'string')", // Corrected type to 'string'
		key, value,
	)
	if err != nil {
		// Log the error
		h.logger.Printf("Error updating setting %s to %s: %v", key, value, err)
		return fmt.Errorf("failed to update setting %s: %w", key, err)
	}

	if err := tx.Commit(); err != nil {
		// Log the error
		h.logger.Printf("Error committing transaction for setting %s: %v", key, err)
		return fmt.Errorf("failed to commit transaction for setting %s: %w", key, err)
	}

	if !h.productionMode {
		h.logger.Printf("Successfully updated setting %s to %s", key, value)
	}
	return nil
}
