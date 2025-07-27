// internal/feed/filter.go
package feed

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"infoscope/internal/database"
)

// FilterDecision represents the result of filter evaluation
type FilterDecision int

const (
	FilterKeep FilterDecision = iota
	FilterDiscard
)

// FilterEngine manages entry filtering logic
type FilterEngine struct {
	db           *sql.DB
	regexCache   map[string]*regexp.Regexp
	regexMutex   sync.RWMutex
	lastUpdated  time.Time
	cachedGroups []database.FilterGroup
	cacheMutex   sync.RWMutex
}

// NewFilterEngine creates a new filter engine instance
func NewFilterEngine(db *sql.DB) *FilterEngine {
	return &FilterEngine{
		db:         db,
		regexCache: make(map[string]*regexp.Regexp),
	}
}

// FilterEntry evaluates an entry title against all active filter groups
func (fe *FilterEngine) FilterEntry(ctx context.Context, title string) (FilterDecision, error) {
	groups, err := fe.getActiveFilterGroups(ctx)
	if err != nil {
		return FilterKeep, fmt.Errorf("failed to get active filter groups: %w", err)
	}

	// If no filters are active, keep the entry by default
	if len(groups) == 0 {
		return FilterKeep, nil
	}

	// Evaluate filter groups in priority order (ascending)
	for _, group := range groups {
		matches, err := fe.evaluateFilterGroup(group, title)
		if err != nil {
			// Log error but continue with other groups
			// In a production system, you might want to use structured logging
			continue
		}

		if matches {
			// First matching group determines the action
			if group.Action == "discard" {
				return FilterDiscard, nil
			}
			return FilterKeep, nil
		}
	}

	// If no groups match, keep the entry by default
	return FilterKeep, nil
}

// getActiveFilterGroups retrieves and caches active filter groups
func (fe *FilterEngine) getActiveFilterGroups(ctx context.Context) ([]database.FilterGroup, error) {
	fe.cacheMutex.RLock()
	// Check if cache is still valid (5 minutes)
	if time.Since(fe.lastUpdated) < 5*time.Minute && len(fe.cachedGroups) >= 0 {
		groups := make([]database.FilterGroup, len(fe.cachedGroups))
		copy(groups, fe.cachedGroups)
		fe.cacheMutex.RUnlock()
		return groups, nil
	}
	fe.cacheMutex.RUnlock()

	// Need to refresh cache
	fe.cacheMutex.Lock()
	defer fe.cacheMutex.Unlock()

	// Double-check pattern - another goroutine might have updated while we were waiting
	if time.Since(fe.lastUpdated) < 5*time.Minute && len(fe.cachedGroups) >= 0 {
		groups := make([]database.FilterGroup, len(fe.cachedGroups))
		copy(groups, fe.cachedGroups)
		return groups, nil
	}

	// Fetch fresh data
	db := &database.DB{DB: fe.db}
	groups, err := db.GetActiveFilterGroups(ctx)
	if err != nil {
		return nil, err
	}

	fe.cachedGroups = groups
	fe.lastUpdated = time.Now()

	return groups, nil
}

// evaluateFilterGroup evaluates a single filter group against a title
func (fe *FilterEngine) evaluateFilterGroup(group database.FilterGroup, title string) (bool, error) {
	if len(group.Rules) == 0 {
		return false, nil
	}

	// Handle single rule case (most common scenario)
	if len(group.Rules) == 1 {
		return fe.evaluateFilter(group.Rules[0].Filter, title)
	}

	// Handle multiple rules with boolean logic
	result := false

	for i, rule := range group.Rules {
		filterMatches, err := fe.evaluateFilter(rule.Filter, title)
		if err != nil {
			return false, fmt.Errorf("filter evaluation failed for rule %d (%s): %w", i, rule.Filter.Name, err)
		}

		if i == 0 {
			// First rule sets the initial result (operator is ignored for first rule)
			result = filterMatches
		} else {
			// Apply the operator from the current rule to combine with previous result
			switch rule.Operator {
			case "AND":
				result = result && filterMatches
				// Short-circuit: if AND operation fails, no need to continue
				if !result {
					return false, nil
				}
			case "OR":
				result = result || filterMatches
				// Short-circuit: if OR operation succeeds, no need to continue
				if result {
					return true, nil
				}
			default:
				// Default to AND for invalid operators (defensive programming)
				result = result && filterMatches
				if !result {
					return false, nil
				}
			}
		}
	}

	return result, nil
}

// evaluateFilter evaluates a single filter against a title
func (fe *FilterEngine) evaluateFilter(filter *database.EntryFilter, title string) (bool, error) {
	if filter == nil {
		return false, nil
	}

	switch filter.PatternType {
	case "keyword":
		return fe.evaluateKeywordFilter(filter, title), nil
	case "regex":
		return fe.evaluateRegexFilter(filter, title)
	default:
		return false, fmt.Errorf("unknown filter pattern type: %s", filter.PatternType)
	}
}

// evaluateKeywordFilter evaluates a keyword filter
func (fe *FilterEngine) evaluateKeywordFilter(filter *database.EntryFilter, title string) bool {
	searchTitle := title
	searchPattern := filter.Pattern

	if !filter.CaseSensitive {
		searchTitle = strings.ToLower(title)
		searchPattern = strings.ToLower(filter.Pattern)
	}

	return strings.Contains(searchTitle, searchPattern)
}

// evaluateRegexFilter evaluates a regex filter with caching
func (fe *FilterEngine) evaluateRegexFilter(filter *database.EntryFilter, title string) (bool, error) {
	regex, err := fe.getCompiledRegex(filter)
	if err != nil {
		return false, err
	}

	return regex.MatchString(title), nil
}

// getCompiledRegex retrieves or compiles a regex pattern with caching
func (fe *FilterEngine) getCompiledRegex(filter *database.EntryFilter) (*regexp.Regexp, error) {
	// Create a cache key that includes case sensitivity
	cacheKey := fmt.Sprintf("%s:%t", filter.Pattern, filter.CaseSensitive)

	fe.regexMutex.RLock()
	if regex, exists := fe.regexCache[cacheKey]; exists {
		fe.regexMutex.RUnlock()
		return regex, nil
	}
	fe.regexMutex.RUnlock()

	// Compile regex with appropriate flags
	fe.regexMutex.Lock()
	defer fe.regexMutex.Unlock()

	// Double-check pattern
	if regex, exists := fe.regexCache[cacheKey]; exists {
		return regex, nil
	}

	pattern := filter.Pattern
	if !filter.CaseSensitive {
		pattern = "(?i)" + pattern
	}

	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern '%s': %w", filter.Pattern, err)
	}

	// Cache the compiled regex
	fe.regexCache[cacheKey] = regex

	return regex, nil
}

// ClearCache clears the internal caches (useful for testing or manual refresh)
func (fe *FilterEngine) ClearCache() {
	fe.cacheMutex.Lock()
	fe.cachedGroups = nil
	fe.lastUpdated = time.Time{}
	fe.cacheMutex.Unlock()

	fe.regexMutex.Lock()
	fe.regexCache = make(map[string]*regexp.Regexp)
	fe.regexMutex.Unlock()
}

// ValidateRegexPattern validates a regex pattern without caching
func ValidateRegexPattern(pattern string, caseSensitive bool) error {
	testPattern := pattern
	if !caseSensitive {
		testPattern = "(?i)" + pattern
	}

	_, err := regexp.Compile(testPattern)
	if err != nil {
		return fmt.Errorf("invalid regex pattern: %w", err)
	}

	return nil
}

// TestFilter tests a filter against sample text
func (fe *FilterEngine) TestFilter(filter *database.EntryFilter, testText string) (bool, error) {
	return fe.evaluateFilter(filter, testText)
}

// TestFilterGroup tests a filter group against sample text
func (fe *FilterEngine) TestFilterGroup(group database.FilterGroup, testText string) (bool, error) {
	return fe.evaluateFilterGroup(group, testText)
}

// InvalidateCache forces the filter engine to refresh its cache on next request
func (fe *FilterEngine) InvalidateCache() {
	fe.cacheMutex.Lock()
	defer fe.cacheMutex.Unlock()

	fe.cachedGroups = nil
	fe.lastUpdated = time.Time{} // Zero time to force refresh
}
