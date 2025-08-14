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

// FilterEntry evaluates an entry against all active filter groups  
func (fe *FilterEngine) FilterEntry(ctx context.Context, entry *database.Entry, feedCategory string, feedTags []string) (FilterDecision, error) {
	groups, err := fe.getActiveFilterGroups(ctx)
	if err != nil {
		return FilterKeep, fmt.Errorf("failed to get active filter groups: %w", err)
	}

	// If no filters are active, keep the entry by default
	if len(groups) == 0 {
		return FilterKeep, nil
	}

	// Filter groups by category if specified
	var relevantGroups []database.FilterGroup
	for _, group := range groups {
		// If group has no category filter, it applies to all entries
		// If group has category filter, it only applies to entries from feeds in that category
		if group.ApplyToCategory == "" || group.ApplyToCategory == feedCategory {
			relevantGroups = append(relevantGroups, group)
		}
	}

	// Check if there are any "keep" filters among the relevant groups
	hasKeepFilters := false
	for _, group := range relevantGroups {
		if group.Action == "keep" {
			hasKeepFilters = true
			break
		}
	}

	// Handle "keep" filters (whitelist mode) - must be processed separately
	if hasKeepFilters {
		// For keep filters, we only care about keep filters, ignore discard filters
		for _, group := range relevantGroups {
			if group.Action == "keep" {
				matches, err := fe.evaluateFilterGroup(group, entry, feedCategory, feedTags)
				if err != nil {
					// Log error but continue with other groups
					continue
				}
				if matches {
					// At least one keep filter matches - keep the entry
					return FilterKeep, nil
				}
			}
		}
		// No keep filters matched - discard the entry (whitelist behavior)
		return FilterDiscard, nil
	}

	// Handle "discard" filters (blacklist mode)
	for _, group := range relevantGroups {
		if group.Action == "discard" {
			matches, err := fe.evaluateFilterGroup(group, entry, feedCategory, feedTags)
			if err != nil {
				// Log error but continue with other groups
				continue
			}
			if matches {
				// Discard filter matches - discard the entry
				return FilterDiscard, nil
			}
		}
	}

	// No discard filters matched - keep the entry (blacklist behavior)
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

// evaluateFilterGroup evaluates a single filter group against an entry
func (fe *FilterEngine) evaluateFilterGroup(group database.FilterGroup, entry *database.Entry, feedCategory string, feedTags []string) (bool, error) {
	if len(group.Rules) == 0 {
		return false, nil
	}

	// Handle single rule case (most common scenario)
	if len(group.Rules) == 1 {
		return fe.evaluateFilter(group.Rules[0].Filter, entry, feedCategory, feedTags)
	}

	// Handle multiple rules with boolean logic
	result := false

	for i, rule := range group.Rules {
		filterMatches, err := fe.evaluateFilter(rule.Filter, entry, feedCategory, feedTags)
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

// evaluateFilter evaluates a single filter against an entry
func (fe *FilterEngine) evaluateFilter(filter *database.EntryFilter, entry *database.Entry, feedCategory string, feedTags []string) (bool, error) {
	if filter == nil {
		return false, nil
	}

	// Get the target text based on filter target type
	var targetText string
	switch filter.TargetType {
	case "title":
		targetText = entry.Title
	case "content":
		targetText = entry.Content
	case "feed_category":
		targetText = feedCategory
	case "feed_tags":
		// For tags, we check if the pattern matches any of the feed's tags
		for _, tag := range feedTags {
			matches, err := fe.evaluateFilterPattern(filter, tag)
			if err != nil {
				return false, err
			}
			if matches {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, fmt.Errorf("unknown filter target type: %s", filter.TargetType)
	}

	return fe.evaluateFilterPattern(filter, targetText)
}

// evaluateFilterPattern evaluates a filter pattern against target text
func (fe *FilterEngine) evaluateFilterPattern(filter *database.EntryFilter, targetText string) (bool, error) {
	switch filter.PatternType {
	case "keyword":
		return fe.evaluateKeywordFilter(filter, targetText), nil
	case "regex":
		return fe.evaluateRegexFilter(filter, targetText)
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
	// Create a mock entry for testing
	mockEntry := &database.Entry{
		Title:   testText,
		Content: testText,
	}
	
	// For testing, we assume the test text represents the target content
	var feedCategory string
	var feedTags []string
	
	// If testing feed_category or feed_tags, use test text as those values
	if filter.TargetType == "feed_category" {
		feedCategory = testText
	} else if filter.TargetType == "feed_tags" {
		feedTags = []string{testText}
	}
	
	return fe.evaluateFilter(filter, mockEntry, feedCategory, feedTags)
}

// TestFilterGroup tests a filter group against sample text
func (fe *FilterEngine) TestFilterGroup(group database.FilterGroup, testText string) (bool, error) {
	// Create a mock entry for testing
	mockEntry := &database.Entry{
		Title:   testText,
		Content: testText,
	}
	
	return fe.evaluateFilterGroup(group, mockEntry, testText, []string{testText})
}

// InvalidateCache forces the filter engine to refresh its cache on next request
func (fe *FilterEngine) InvalidateCache() {
	fe.cacheMutex.Lock()
	defer fe.cacheMutex.Unlock()

	fe.cachedGroups = nil
	fe.lastUpdated = time.Time{} // Zero time to force refresh
}
