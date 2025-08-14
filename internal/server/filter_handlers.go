// internal/server/filter_handlers.go
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"infoscope/internal/database"
	"infoscope/internal/feed"
)

// Helper function to extract ID from URL path
func extractIDFromPath(path string, prefix string) (int64, error) {
	// Remove prefix and any trailing slash
	idStr := strings.TrimPrefix(path, prefix)
	idStr = strings.TrimSuffix(idStr, "/")

	// Handle paths like "/admin/filters/123/rules"
	parts := strings.Split(idStr, "/")
	if len(parts) > 0 && parts[0] != "" {
		return strconv.ParseInt(parts[0], 10, 64)
	}

	return 0, fmt.Errorf("invalid ID in path")
}

// FilterResponse represents the response format for filter operations
type FilterResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// CreateFilterRequest represents the request to create a new filter
type CreateFilterRequest struct {
	Name          string `json:"name"`
	Pattern       string `json:"pattern"`
	PatternType   string `json:"pattern_type"`
	TargetType    string `json:"target_type"`
	CaseSensitive bool   `json:"case_sensitive"`
}

// UpdateFilterRequest represents the request to update a filter
type UpdateFilterRequest struct {
	Name          string `json:"name"`
	Pattern       string `json:"pattern"`
	PatternType   string `json:"pattern_type"`
	TargetType    string `json:"target_type"`
	CaseSensitive bool   `json:"case_sensitive"`
}

// CreateFilterGroupRequest represents the request to create a new filter group
type CreateFilterGroupRequest struct {
	Name            string `json:"name"`
	Action          string `json:"action"`
	Priority        int    `json:"priority"`
	ApplyToCategory string `json:"apply_to_category,omitempty"`
}

// UpdateFilterGroupRequest represents the request to update a filter group
type UpdateFilterGroupRequest struct {
	Name            string `json:"name"`
	Action          string `json:"action"`
	IsActive        bool   `json:"is_active"`
	Priority        int    `json:"priority"`
	ApplyToCategory string `json:"apply_to_category,omitempty"`
}

// FilterGroupRulesRequest represents the request to update filter group rules
type FilterGroupRulesRequest struct {
	Rules []FilterGroupRuleRequest `json:"rules"`
}

// FilterGroupRuleRequest represents a single rule in a group
type FilterGroupRuleRequest struct {
	FilterID int64  `json:"filter_id"`
	Operator string `json:"operator"`
	Position int    `json:"position"`
}

// TestFilterRequest represents the request to test a filter
type TestFilterRequest struct {
	Pattern       string `json:"pattern"`
	PatternType   string `json:"pattern_type"`
	TargetType    string `json:"target_type"`
	CaseSensitive bool   `json:"case_sensitive"`
	TestText      string `json:"test_text"`
}

// GetFilters handles GET /admin/filters
func (s *Server) GetFilters(w http.ResponseWriter, r *http.Request) {
	db := &database.DB{DB: s.db}
	filters, err := db.GetAllEntryFilters(r.Context())
	if err != nil {
		s.jsonError(w, "Failed to get filters", http.StatusInternalServerError)
		return
	}

	s.jsonResponse(w, FilterResponse{
		Success: true,
		Data:    filters,
	})
}

// GetFilter handles GET /admin/filters/{id}
func (s *Server) GetFilter(w http.ResponseWriter, r *http.Request) {
	filterID, err := extractIDFromPath(r.URL.Path, "/admin/filters/")
	if err != nil {
		s.jsonError(w, "Invalid filter ID", http.StatusBadRequest)
		return
	}

	db := &database.DB{DB: s.db}
	filters, err := db.GetAllEntryFilters(r.Context())
	if err != nil {
		s.jsonError(w, "Failed to get filters", http.StatusInternalServerError)
		return
	}

	// Find the specific filter
	for _, filter := range filters {
		if filter.ID == filterID {
			s.jsonResponse(w, FilterResponse{
				Success: true,
				Data:    filter,
			})
			return
		}
	}

	s.jsonError(w, "Filter not found", http.StatusNotFound)
}

// CreateFilter handles POST /admin/filters
func (s *Server) CreateFilter(w http.ResponseWriter, r *http.Request) {
	var req CreateFilterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate input
	if req.Name == "" || req.Pattern == "" {
		s.jsonError(w, "Name and pattern are required", http.StatusBadRequest)
		return
	}

	if req.PatternType != "keyword" && req.PatternType != "regex" {
		s.jsonError(w, "Pattern type must be 'keyword' or 'regex'", http.StatusBadRequest)
		return
	}

	// Set default target type if not provided
	if req.TargetType == "" {
		req.TargetType = "title"
	}

	if req.TargetType != "title" && req.TargetType != "content" && req.TargetType != "feed_tags" && req.TargetType != "feed_category" {
		s.jsonError(w, "Target type must be 'title', 'content', 'feed_tags', or 'feed_category'", http.StatusBadRequest)
		return
	}

	// Validate regex patterns
	if req.PatternType == "regex" {
		if err := feed.ValidateRegexPattern(req.Pattern, req.CaseSensitive); err != nil {
			s.jsonError(w, fmt.Sprintf("Invalid regex pattern: %v", err), http.StatusBadRequest)
			return
		}
	}

	db := &database.DB{DB: s.db}
	filter, err := db.CreateEntryFilter(r.Context(), req.Name, req.Pattern, req.PatternType, req.TargetType, req.CaseSensitive)
	if err != nil {
		s.logger.Printf("Error creating filter: %v", err)
		s.jsonError(w, "Failed to create filter", http.StatusInternalServerError)
		return
	}

	// Invalidate filter cache in case this filter gets added to active groups
	s.feedService.InvalidateFilterCache()

	s.jsonResponse(w, FilterResponse{
		Success: true,
		Message: "Filter created successfully",
		Data:    filter,
	})
}

// UpdateFilter handles PUT /admin/filters/{id}
func (s *Server) UpdateFilter(w http.ResponseWriter, r *http.Request) {
	id, err := extractIDFromPath(r.URL.Path, "/admin/filters/")
	if err != nil {
		s.jsonError(w, "Invalid filter ID", http.StatusBadRequest)
		return
	}

	var req UpdateFilterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate input
	if req.Name == "" || req.Pattern == "" {
		s.jsonError(w, "Name and pattern are required", http.StatusBadRequest)
		return
	}

	if req.PatternType != "keyword" && req.PatternType != "regex" {
		s.jsonError(w, "Pattern type must be 'keyword' or 'regex'", http.StatusBadRequest)
		return
	}

	// Set default target type if not provided
	if req.TargetType == "" {
		req.TargetType = "title"
	}

	if req.TargetType != "title" && req.TargetType != "content" && req.TargetType != "feed_tags" && req.TargetType != "feed_category" {
		s.jsonError(w, "Target type must be 'title', 'content', 'feed_tags', or 'feed_category'", http.StatusBadRequest)
		return
	}

	// Validate regex patterns
	if req.PatternType == "regex" {
		if err := feed.ValidateRegexPattern(req.Pattern, req.CaseSensitive); err != nil {
			s.jsonError(w, fmt.Sprintf("Invalid regex pattern: %v", err), http.StatusBadRequest)
			return
		}
	}

	db := &database.DB{DB: s.db}
	err = db.UpdateEntryFilter(r.Context(), id, req.Name, req.Pattern, req.PatternType, req.TargetType, req.CaseSensitive)
	if err == database.ErrNotFound {
		s.jsonError(w, "Filter not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.logger.Printf("Error updating filter: %v", err)
		s.jsonError(w, "Failed to update filter", http.StatusInternalServerError)
		return
	}

	// Invalidate filter cache to ensure changes take effect immediately
	s.feedService.InvalidateFilterCache()

	s.jsonResponse(w, FilterResponse{
		Success: true,
		Message: "Filter updated successfully",
	})
}

// DeleteFilter handles DELETE /admin/filters/{id}
func (s *Server) DeleteFilter(w http.ResponseWriter, r *http.Request) {
	id, err := extractIDFromPath(r.URL.Path, "/admin/filters/")
	if err != nil {
		s.jsonError(w, "Invalid filter ID", http.StatusBadRequest)
		return
	}

	db := &database.DB{DB: s.db}
	err = db.DeleteEntryFilter(r.Context(), id)
	if err == database.ErrNotFound {
		s.jsonError(w, "Filter not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.logger.Printf("Error deleting filter: %v", err)
		s.jsonError(w, "Failed to delete filter", http.StatusInternalServerError)
		return
	}

	// Invalidate filter cache to ensure changes take effect immediately
	s.feedService.InvalidateFilterCache()

	s.jsonResponse(w, FilterResponse{
		Success: true,
		Message: "Filter deleted successfully",
	})
}

// GetFilterGroups handles GET /admin/filter-groups
func (s *Server) GetFilterGroups(w http.ResponseWriter, r *http.Request) {
	db := &database.DB{DB: s.db}
	groups, err := db.GetAllFilterGroups(r.Context())
	if err != nil {
		s.jsonError(w, "Failed to get filter groups", http.StatusInternalServerError)
		return
	}

	s.jsonResponse(w, FilterResponse{
		Success: true,
		Data:    groups,
	})
}

// GetFilterGroup handles GET /admin/filter-groups/{id}
func (s *Server) GetFilterGroup(w http.ResponseWriter, r *http.Request) {
	groupID, err := extractIDFromPath(r.URL.Path, "/admin/filter-groups/")
	if err != nil {
		s.jsonError(w, "Invalid filter group ID", http.StatusBadRequest)
		return
	}

	db := &database.DB{DB: s.db}
	groups, err := db.GetAllFilterGroups(r.Context())
	if err != nil {
		s.jsonError(w, "Failed to get filter groups", http.StatusInternalServerError)
		return
	}

	// Find the specific group
	for _, group := range groups {
		if group.ID == groupID {
			s.jsonResponse(w, FilterResponse{
				Success: true,
				Data:    group,
			})
			return
		}
	}

	s.jsonError(w, "Filter group not found", http.StatusNotFound)
}

// CreateFilterGroup handles POST /admin/filter-groups
func (s *Server) CreateFilterGroup(w http.ResponseWriter, r *http.Request) {
	var req CreateFilterGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate input
	if req.Name == "" {
		s.jsonError(w, "Name is required", http.StatusBadRequest)
		return
	}

	if req.Action != "keep" && req.Action != "discard" {
		s.jsonError(w, "Action must be 'keep' or 'discard'", http.StatusBadRequest)
		return
	}

	db := &database.DB{DB: s.db}
	group, err := db.CreateFilterGroup(r.Context(), req.Name, req.Action, req.Priority, req.ApplyToCategory)
	if err != nil {
		s.logger.Printf("Error creating filter group: %v", err)
		s.jsonError(w, "Failed to create filter group", http.StatusInternalServerError)
		return
	}

	// Invalidate filter cache to ensure changes take effect immediately
	s.feedService.InvalidateFilterCache()

	s.jsonResponse(w, FilterResponse{
		Success: true,
		Message: "Filter group created successfully",
		Data:    group,
	})
}

// UpdateFilterGroup handles PUT /admin/filter-groups/{id}
func (s *Server) UpdateFilterGroup(w http.ResponseWriter, r *http.Request) {
	id, err := extractIDFromPath(r.URL.Path, "/admin/filter-groups/")
	if err != nil {
		s.jsonError(w, "Invalid filter group ID", http.StatusBadRequest)
		return
	}

	var req UpdateFilterGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate input
	if req.Name == "" {
		s.jsonError(w, "Name is required", http.StatusBadRequest)
		return
	}

	if req.Action != "keep" && req.Action != "discard" {
		s.jsonError(w, "Action must be 'keep' or 'discard'", http.StatusBadRequest)
		return
	}

	db := &database.DB{DB: s.db}
	err = db.UpdateFilterGroup(r.Context(), id, req.Name, req.Action, req.IsActive, req.Priority, req.ApplyToCategory)
	if err == database.ErrNotFound {
		s.jsonError(w, "Filter group not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.logger.Printf("Error updating filter group: %v", err)
		s.jsonError(w, "Failed to update filter group", http.StatusInternalServerError)
		return
	}

	// Invalidate filter cache to ensure changes take effect immediately
	s.feedService.InvalidateFilterCache()

	s.jsonResponse(w, FilterResponse{
		Success: true,
		Message: "Filter group updated successfully",
	})
}

// DeleteFilterGroup handles DELETE /admin/filter-groups/{id}
func (s *Server) DeleteFilterGroup(w http.ResponseWriter, r *http.Request) {
	id, err := extractIDFromPath(r.URL.Path, "/admin/filter-groups/")
	if err != nil {
		s.jsonError(w, "Invalid filter group ID", http.StatusBadRequest)
		return
	}

	db := &database.DB{DB: s.db}
	err = db.DeleteFilterGroup(r.Context(), id)
	if err == database.ErrNotFound {
		s.jsonError(w, "Filter group not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.logger.Printf("Error deleting filter group: %v", err)
		s.jsonError(w, "Failed to delete filter group", http.StatusInternalServerError)
		return
	}

	// Invalidate filter cache to ensure changes take effect immediately
	s.feedService.InvalidateFilterCache()

	s.jsonResponse(w, FilterResponse{
		Success: true,
		Message: "Filter group deleted successfully",
	})
}

// GetFilterGroupRules handles GET /admin/filter-groups/{id}/rules
func (s *Server) GetFilterGroupRules(w http.ResponseWriter, r *http.Request) {
	groupID, err := extractIDFromPath(r.URL.Path, "/admin/filter-groups/")
	if err != nil {
		s.jsonError(w, "Invalid filter group ID", http.StatusBadRequest)
		return
	}

	db := &database.DB{DB: s.db}
	rules, err := db.GetFilterGroupRules(r.Context(), groupID)
	if err != nil {
		s.jsonError(w, "Failed to get filter group rules", http.StatusInternalServerError)
		return
	}

	s.jsonResponse(w, FilterResponse{
		Success: true,
		Data:    rules,
	})
}

// UpdateFilterGroupRules handles PUT /admin/filter-groups/{id}/rules
func (s *Server) UpdateFilterGroupRules(w http.ResponseWriter, r *http.Request) {
	groupID, err := extractIDFromPath(r.URL.Path, "/admin/filter-groups/")
	if err != nil {
		s.jsonError(w, "Invalid filter group ID", http.StatusBadRequest)
		return
	}

	var req FilterGroupRulesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Convert request rules to database rules with validation
	var dbRules []database.FilterGroupRule
	for i, rule := range req.Rules {
		// Validate operator
		if rule.Operator != "AND" && rule.Operator != "OR" {
			s.jsonError(w, fmt.Sprintf("Invalid operator '%s' at position %d. Must be 'AND' or 'OR'", rule.Operator, i), http.StatusBadRequest)
			return
		}

		// Validate filter ID
		if rule.FilterID <= 0 {
			s.jsonError(w, fmt.Sprintf("Invalid filter ID %d at position %d", rule.FilterID, i), http.StatusBadRequest)
			return
		}

		// Validate position
		if rule.Position < 0 {
			s.jsonError(w, fmt.Sprintf("Invalid position %d at position %d", rule.Position, i), http.StatusBadRequest)
			return
		}

		dbRules = append(dbRules, database.FilterGroupRule{
			GroupID:  groupID,
			FilterID: rule.FilterID,
			Operator: rule.Operator,
			Position: rule.Position,
		})
	}

	db := &database.DB{DB: s.db}
	// Persist new ordered rules. On implementations without the helper, fall back to replace-all.
	if err = db.UpdateFilterGroupRules(r.Context(), groupID, dbRules); err != nil {
		s.logger.Printf("Falling back to replace-all rules for group %d: %v", groupID, err)
		if derr := db.ReplaceFilterGroupRules(r.Context(), groupID, dbRules); derr != nil {
			s.logger.Printf("Error replacing filter group rules: %v", derr)
			s.jsonError(w, "Failed to update filter group rules", http.StatusInternalServerError)
			return
		}
	}
	if err != nil {
		s.logger.Printf("Error updating filter group rules: %v", err)
		s.jsonError(w, "Failed to update filter group rules", http.StatusInternalServerError)
		return
	}

	// Invalidate filter cache to ensure changes take effect immediately
	s.feedService.InvalidateFilterCache()

	s.jsonResponse(w, FilterResponse{
		Success: true,
		Message: "Filter group rules updated successfully",
	})
}

// TestFilter handles POST /admin/filter-test
func (s *Server) TestFilter(w http.ResponseWriter, r *http.Request) {
	var req TestFilterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate input
	if req.Pattern == "" || req.TestText == "" {
		s.jsonError(w, "Pattern and test text are required", http.StatusBadRequest)
		return
	}

	if req.PatternType != "keyword" && req.PatternType != "regex" {
		s.jsonError(w, "Pattern type must be 'keyword' or 'regex'", http.StatusBadRequest)
		return
	}

	// Set default target type if not provided
	if req.TargetType == "" {
		req.TargetType = "title"
	}

	if req.TargetType != "title" && req.TargetType != "content" && req.TargetType != "feed_tags" && req.TargetType != "feed_category" {
		s.jsonError(w, "Target type must be 'title', 'content', 'feed_tags', or 'feed_category'", http.StatusBadRequest)
		return
	}

	// Create a temporary filter for testing
	filter := &database.EntryFilter{
		Pattern:       req.Pattern,
		PatternType:   req.PatternType,
		TargetType:    req.TargetType,
		CaseSensitive: req.CaseSensitive,
	}

	// Create a filter engine for testing
	filterEngine := feed.NewFilterEngine(s.db)
	matches, err := filterEngine.TestFilter(filter, req.TestText)
	if err != nil {
		s.jsonError(w, fmt.Sprintf("Filter test failed: %v", err), http.StatusBadRequest)
		return
	}

	s.jsonResponse(w, FilterResponse{
		Success: true,
		Data: map[string]interface{}{
			"matches":   matches,
			"test_text": req.TestText,
			"pattern":   req.Pattern,
		},
	})
}

// jsonResponse sends a JSON response
func (s *Server) jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// jsonError sends a JSON error response
func (s *Server) jsonError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(FilterResponse{
		Success: false,
		Message: message,
	})
}

// Route handlers that dispatch based on HTTP method

// handleFilterRoutes handles routing for /admin/filters and /admin/filters/{id}
func (s *Server) handleFilterRoutes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Check if this is a request for a specific filter
		if path := strings.TrimPrefix(r.URL.Path, "/admin/filters/"); path != "" && path != r.URL.Path {
			s.GetFilter(w, r)
		} else {
			s.GetFilters(w, r)
		}
	case http.MethodPost:
		s.CreateFilter(w, r)
	case http.MethodPut:
		s.UpdateFilter(w, r)
	case http.MethodDelete:
		s.DeleteFilter(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleFilterGroupRoutes handles routing for /admin/filter-groups and /admin/filter-groups/{id}
func (s *Server) handleFilterGroupRoutes(w http.ResponseWriter, r *http.Request) {
	// Check if this is a rules request
	if strings.Contains(r.URL.Path, "/rules") {
		switch r.Method {
		case http.MethodGet:
			s.GetFilterGroupRules(w, r)
		case http.MethodPut:
			s.UpdateFilterGroupRules(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		// Check if this is a request for a specific group
		if path := strings.TrimPrefix(r.URL.Path, "/admin/filter-groups/"); path != "" && path != r.URL.Path {
			s.GetFilterGroup(w, r)
		} else {
			s.GetFilterGroups(w, r)
		}
	case http.MethodPost:
		s.CreateFilterGroup(w, r)
	case http.MethodPut:
		s.UpdateFilterGroup(w, r)
	case http.MethodDelete:
		s.DeleteFilterGroup(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
