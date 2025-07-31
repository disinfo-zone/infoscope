// internal/server/taxonomy_handlers.go
package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"infoscope/internal/database"
)

// FeedUpdateRequest represents the JSON payload for updating a feed
type FeedUpdateRequest struct {
	Title    string   `json:"title"`
	URL      string   `json:"url"`
	Category string   `json:"category"`
	Tags     []string `json:"tags"`
}

// FeedResponse represents the JSON response for feed data
type FeedResponse struct {
	ID          int64    `json:"id"`
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Status      string   `json:"status"`
	ErrorCount  int      `json:"errorCount"`
	LastError   string   `json:"lastError,omitempty"`
	LastFetched string   `json:"lastFetched"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
}

// handleFeedAPI handles REST API requests for individual feeds
func (s *Server) handleFeedAPI(w http.ResponseWriter, r *http.Request) {
	// Set JSON content type
	w.Header().Set("Content-Type", "application/json")

	// Extract feed ID from URL
	feedIDStr := strings.TrimPrefix(r.URL.Path, "/admin/api/feeds/")
	feedIDStr = strings.TrimSuffix(feedIDStr, "/")
	
	if feedIDStr == "" {
		s.writeJSONError(w, "Feed ID is required", http.StatusBadRequest)
		return
	}

	feedID, err := strconv.ParseInt(feedIDStr, 10, 64)
	if err != nil {
		s.writeJSONError(w, "Invalid feed ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetFeed(w, r, feedID)
	case http.MethodPut:
		s.handleUpdateFeed(w, r, feedID)
	case http.MethodDelete:
		s.handleDeleteFeed(w, r, feedID)
	default:
		s.writeJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetFeed retrieves a single feed by ID
func (s *Server) handleGetFeed(w http.ResponseWriter, r *http.Request, feedID int64) {
	feed, err := s.dbWrapper().GetFeedByID(r.Context(), feedID)
	if err == database.ErrNotFound {
		s.writeJSONError(w, "Feed not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.logger.Printf("Error retrieving feed %d: %v", feedID, err)
		s.writeJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Convert to response format
	response := FeedResponse{
		ID:          feed.ID,
		Title:       feed.Title,
		URL:         feed.URL,
		Category:    feed.Category,
		Tags:        feed.Tags,
		Status:      feed.Status,
		ErrorCount:  feed.ErrorCount,
		LastError:   feed.LastError,
		LastFetched: feed.LastFetched.Format("2006-01-02T15:04:05Z"),
		CreatedAt:   feed.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   feed.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Printf("Error encoding feed response: %v", err)
	}
}

// handleUpdateFeed updates a feed's taxonomy information
func (s *Server) handleUpdateFeed(w http.ResponseWriter, r *http.Request, feedID int64) {
	// Verify CSRF token
	if !s.csrf.Validate(w, r) {
		s.writeJSONError(w, "Invalid CSRF token", http.StatusForbidden)
		return
	}

	var req FeedUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSONError(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if strings.TrimSpace(req.Title) == "" {
		s.writeJSONError(w, "Title is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		s.writeJSONError(w, "URL is required", http.StatusBadRequest)
		return
	}

	// Clean up the data
	req.Title = strings.TrimSpace(req.Title)
	req.URL = strings.TrimSpace(req.URL)
	req.Category = strings.TrimSpace(req.Category)
	
	// Clean up tags (remove empty entries)
	var cleanTags []string
	for _, tag := range req.Tags {
		if cleaned := strings.TrimSpace(tag); cleaned != "" {
			cleanTags = append(cleanTags, cleaned)
		}
	}

	// Update the feed
	err := s.dbWrapper().UpdateFeedWithTaxonomy(r.Context(), feedID, req.Title, req.URL, req.Category, cleanTags)
	if err == database.ErrNotFound {
		s.writeJSONError(w, "Feed not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.logger.Printf("Error updating feed %d: %v", feedID, err)
		s.writeJSONError(w, "Failed to update feed", http.StatusInternalServerError)
		return
	}

	// Return the updated feed
	s.handleGetFeed(w, r, feedID)
}

// handleDeleteFeed deletes a feed (existing functionality, kept for completeness)
func (s *Server) handleDeleteFeed(w http.ResponseWriter, r *http.Request, feedID int64) {
	// Verify CSRF token
	if !s.csrf.Validate(w, r) {
		s.writeJSONError(w, "Invalid CSRF token", http.StatusForbidden)
		return
	}

	// Delete the feed
	_, err := s.db.ExecContext(r.Context(), "DELETE FROM feeds WHERE id = ?", feedID)
	if err != nil {
		s.logger.Printf("Error deleting feed %d: %v", feedID, err)
		s.writeJSONError(w, "Failed to delete feed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleTagsAPI handles requests for tag data
func (s *Server) handleTagsAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tags, err := s.dbWrapper().GetAllTagNames(r.Context())
	if err != nil {
		s.logger.Printf("Error retrieving tags: %v", err)
		s.writeJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(map[string][]string{"tags": tags}); err != nil {
		s.logger.Printf("Error encoding tags response: %v", err)
	}
}

// handleCategoriesAPI handles requests for category data
func (s *Server) handleCategoriesAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	categories, err := s.dbWrapper().GetAllCategories(r.Context())
	if err != nil {
		s.logger.Printf("Error retrieving categories: %v", err)
		s.writeJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(map[string][]string{"categories": categories}); err != nil {
		s.logger.Printf("Error encoding categories response: %v", err)
	}
}

// writeJSONError writes a JSON error response
func (s *Server) writeJSONError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	
	errorResponse := map[string]string{
		"error": message,
	}
	
	if err := json.NewEncoder(w).Encode(errorResponse); err != nil {
		s.logger.Printf("Error encoding error response: %v", err)
	}
}

// dbWrapper returns a wrapped database instance with taxonomy methods
func (s *Server) dbWrapper() *database.DB {
	return &database.DB{DB: s.db}
}