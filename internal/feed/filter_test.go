// internal/feed/filter_test.go
package feed

import (
	"context"
	"testing"
	"time"

	"infoscope/internal/database"
)

func TestFilterEngine_KeywordFilter(t *testing.T) {
	env := setupTestDB(t)
	defer env.db.Close()

	// Add filter tables to the test database
	_, err := env.db.Exec(database.Schema)
	if err != nil {
		t.Fatalf("Failed to create filter tables: %v", err)
	}

	fe := NewFilterEngine(env.db)

	// Test case-sensitive keyword filter
	filter := &database.EntryFilter{
		Pattern:       "Go",
		PatternType:   "keyword",
		TargetType:    "title",
		CaseSensitive: true,
	}

	// Should match
	result, err := fe.TestFilter(filter, "Learning Go Programming")
	if err != nil {
		t.Fatalf("Error testing filter: %v", err)
	}
	if !result {
		t.Error("Expected filter to match 'Learning Go Programming'")
	}

	// Should not match (case sensitive)
	result, err = fe.TestFilter(filter, "Learning go programming")
	if err != nil {
		t.Fatalf("Error testing filter: %v", err)
	}
	if result {
		t.Error("Expected filter not to match 'Learning go programming' (case sensitive)")
	}

	// Test case-insensitive keyword filter
	filter.CaseSensitive = false
	result, err = fe.TestFilter(filter, "Learning go programming")
	if err != nil {
		t.Fatalf("Error testing filter: %v", err)
	}
	if !result {
		t.Error("Expected case-insensitive filter to match 'Learning go programming'")
	}
}

func TestFilterEngine_RegexFilter(t *testing.T) {
	env := setupTestDB(t)
	defer env.db.Close()

	// Add filter tables to the test database
	_, err := env.db.Exec(database.Schema)
	if err != nil {
		t.Fatalf("Failed to create filter tables: %v", err)
	}

	fe := NewFilterEngine(env.db)

	// Test regex filter
	filter := &database.EntryFilter{
		Pattern:       `^Go\s+\d+\.\d+`,
		PatternType:   "regex",
		TargetType:    "title",
		CaseSensitive: true,
	}

	// Should match
	result, err := fe.TestFilter(filter, "Go 1.21 Released")
	if err != nil {
		t.Fatalf("Error testing filter: %v", err)
	}
	if !result {
		t.Error("Expected regex filter to match 'Go 1.21 Released'")
	}

	// Should not match
	result, err = fe.TestFilter(filter, "New Go features")
	if err != nil {
		t.Fatalf("Error testing filter: %v", err)
	}
	if result {
		t.Error("Expected regex filter not to match 'New Go features'")
	}
}

func TestFilterEngine_InvalidRegex(t *testing.T) {
	env := setupTestDB(t)
	defer env.db.Close()

	// Add filter tables to the test database
	_, err := env.db.Exec(database.Schema)
	if err != nil {
		t.Fatalf("Failed to create filter tables: %v", err)
	}

	fe := NewFilterEngine(env.db)

	// Test invalid regex
	filter := &database.EntryFilter{
		Pattern:       `[invalid regex`,
		PatternType:   "regex",
		TargetType:    "title",
		CaseSensitive: true,
	}

	_, err = fe.TestFilter(filter, "Test text")
	if err == nil {
		t.Error("Expected error for invalid regex pattern")
	}
}

func TestFilterEngine_FilterGroup(t *testing.T) {
	env := setupTestDB(t)
	defer env.db.Close()

	// Add filter tables to the test database
	_, err := env.db.Exec(database.Schema)
	if err != nil {
		t.Fatalf("Failed to create filter tables: %v", err)
	}

	fe := NewFilterEngine(env.db)

	// Create test filters
	filter1 := &database.EntryFilter{
		ID:            1,
		Pattern:       "Go",
		PatternType:   "keyword",
		TargetType:    "title",
		CaseSensitive: false,
	}

	filter2 := &database.EntryFilter{
		ID:            2,
		Pattern:       "Rust",
		PatternType:   "keyword",
		TargetType:    "title",
		CaseSensitive: false,
	}

	// Test OR logic
	group := database.FilterGroup{
		ID:     1,
		Name:   "Languages",
		Action: "keep",
		Rules: []database.FilterGroupRule{
			{
				ID:       1,
				FilterID: 1,
				Operator: "OR",
				Position: 0,
				Filter:   filter1,
			},
			{
				ID:       2,
				FilterID: 2,
				Operator: "OR",
				Position: 1,
				Filter:   filter2,
			},
		},
	}

	// Should match first filter
	result, err := fe.TestFilterGroup(group, "Learning Go Programming")
	if err != nil {
		t.Fatalf("Error testing filter group: %v", err)
	}
	if !result {
		t.Error("Expected OR group to match 'Learning Go Programming'")
	}

	// Should match second filter
	result, err = fe.TestFilterGroup(group, "Rust Programming Guide")
	if err != nil {
		t.Fatalf("Error testing filter group: %v", err)
	}
	if !result {
		t.Error("Expected OR group to match 'Rust Programming Guide'")
	}

	// Should not match
	result, err = fe.TestFilterGroup(group, "Python Tutorial")
	if err != nil {
		t.Fatalf("Error testing filter group: %v", err)
	}
	if result {
		t.Error("Expected OR group not to match 'Python Tutorial'")
	}
}

func TestFilterEngine_FilterGroupAND(t *testing.T) {
	env := setupTestDB(t)
	defer env.db.Close()

	// Add filter tables to the test database
	_, err := env.db.Exec(database.Schema)
	if err != nil {
		t.Fatalf("Failed to create filter tables: %v", err)
	}

	fe := NewFilterEngine(env.db)

	// Create test filters
	filter1 := &database.EntryFilter{
		ID:            1,
		Pattern:       "Go",
		PatternType:   "keyword",
		TargetType:    "title",
		CaseSensitive: false,
	}

	filter2 := &database.EntryFilter{
		ID:            2,
		Pattern:       "tutorial",
		PatternType:   "keyword",
		TargetType:    "title",
		CaseSensitive: false,
	}

	// Test AND logic
	group := database.FilterGroup{
		ID:     1,
		Name:   "Go Tutorials",
		Action: "keep",
		Rules: []database.FilterGroupRule{
			{
				ID:       1,
				FilterID: 1,
				Operator: "AND",
				Position: 0,
				Filter:   filter1,
			},
			{
				ID:       2,
				FilterID: 2,
				Operator: "AND",
				Position: 1,
				Filter:   filter2,
			},
		},
	}

	// Should match (contains both)
	result, err := fe.TestFilterGroup(group, "Go Programming Tutorial")
	if err != nil {
		t.Fatalf("Error testing filter group: %v", err)
	}
	if !result {
		t.Error("Expected AND group to match 'Go Programming Tutorial'")
	}

	// Should not match (missing second keyword)
	result, err = fe.TestFilterGroup(group, "Go Programming Guide")
	if err != nil {
		t.Fatalf("Error testing filter group: %v", err)
	}
	if result {
		t.Error("Expected AND group not to match 'Go Programming Guide'")
	}
}

func TestValidateRegexPattern(t *testing.T) {
	// Valid patterns
	validPatterns := []string{
		`^Go\s+\d+\.\d+`,
		`[Tt]utorial`,
		`\b\w+\b`,
	}

	for _, pattern := range validPatterns {
		if err := ValidateRegexPattern(pattern, true); err != nil {
			t.Errorf("Expected pattern '%s' to be valid, got error: %v", pattern, err)
		}
	}

	// Invalid patterns
	invalidPatterns := []string{
		`[invalid`,
		`*invalid`,
		`(?P<invalid`,
	}

	for _, pattern := range invalidPatterns {
		if err := ValidateRegexPattern(pattern, true); err == nil {
			t.Errorf("Expected pattern '%s' to be invalid", pattern)
		}
	}
}

func TestFilterEngine_CacheClearing(t *testing.T) {
	env := setupTestDB(t)
	defer env.db.Close()

	// Add filter tables to the test database
	_, err := env.db.Exec(database.Schema)
	if err != nil {
		t.Fatalf("Failed to create filter tables: %v", err)
	}

	fe := NewFilterEngine(env.db)

	// Add a regex to cache
	filter := &database.EntryFilter{
		Pattern:       `test`,
		PatternType:   "regex",
		TargetType:    "title",
		CaseSensitive: true,
	}

	_, err = fe.TestFilter(filter, "test")
	if err != nil {
		t.Fatalf("Error testing filter: %v", err)
	}

	// Check cache has content
	if len(fe.regexCache) == 0 {
		t.Error("Expected regex cache to have content")
	}

	// Clear cache
	fe.ClearCache()

	// Check cache is cleared
	if len(fe.regexCache) != 0 {
		t.Error("Expected regex cache to be cleared")
	}
}

// TestFilterEngine_KeepFilterBehavior tests that "keep" filters work as whitelists
func TestFilterEngine_KeepFilterBehavior(t *testing.T) {
	env := setupTestDB(t)
	defer env.db.Close()

	// Add filter tables to the test database
	_, err := env.db.Exec(database.Schema)
	if err != nil {
		t.Fatalf("Failed to create filter tables: %v", err)
	}

	fe := NewFilterEngine(env.db)

	// Mock active filter groups with a "keep" filter
	fe.cacheMutex.Lock()
	fe.cachedGroups = []database.FilterGroup{
		{
			ID:       1,
			Name:     "Keep Go Posts",
			Action:   "keep",
			Priority: 1,
			IsActive: true,
			Rules: []database.FilterGroupRule{
				{
					ID:       1,
					FilterID: 1,
					Operator: "OR",
					Position: 0,
					Filter: &database.EntryFilter{
						ID:            1,
						Pattern:       "Go",
						PatternType:   "keyword",
						TargetType:    "title",
						CaseSensitive: false,
					},
				},
			},
		},
	}
	fe.lastUpdated = time.Now()
	fe.cacheMutex.Unlock()

	// Test that entries matching the "keep" filter are kept
	testEntry := &database.Entry{
		ID: 1,
		FeedID: 1,
		Title: "Learning Go Programming",
		URL: "http://example.com/go",
		Content: "",
	}
	decision, err := fe.FilterEntry(context.Background(), testEntry, "", []string{})
	if err != nil {
		t.Fatalf("Error filtering entry: %v", err)
	}
	if decision != FilterKeep {
		t.Error("Expected entry matching 'keep' filter to be kept")
	}

	// Test that entries NOT matching the "keep" filter are discarded (whitelist behavior)
	testEntry.Title = "Python Tutorial"
	decision, err = fe.FilterEntry(context.Background(), testEntry, "", []string{})
	if err != nil {
		t.Fatalf("Error filtering entry: %v", err)
	}
	if decision != FilterDiscard {
		t.Error("Expected entry NOT matching 'keep' filter to be discarded (this was the bug)")
	}
}

// TestFilterEngine_DiscardFilterBehavior tests that "discard" filters work as blacklists
func TestFilterEngine_DiscardFilterBehavior(t *testing.T) {
	env := setupTestDB(t)
	defer env.db.Close()

	// Add filter tables to the test database
	_, err := env.db.Exec(database.Schema)
	if err != nil {
		t.Fatalf("Failed to create filter tables: %v", err)
	}

	fe := NewFilterEngine(env.db)

	// Mock active filter groups with a "discard" filter
	fe.cacheMutex.Lock()
	fe.cachedGroups = []database.FilterGroup{
		{
			ID:       1,
			Name:     "Discard Spam",
			Action:   "discard",
			Priority: 1,
			IsActive: true,
			Rules: []database.FilterGroupRule{
				{
					ID:       1,
					FilterID: 1,
					Operator: "OR",
					Position: 0,
					Filter: &database.EntryFilter{
						ID:            1,
						Pattern:       "spam",
						PatternType:   "keyword",
						TargetType:    "title",
						CaseSensitive: false,
					},
				},
			},
		},
	}
	fe.lastUpdated = time.Now()
	fe.cacheMutex.Unlock()

	// Test that entries matching the "discard" filter are discarded  
	testEntry := &database.Entry{
		ID: 1,
		FeedID: 1,
		Title: "This is spam content",
		URL: "http://example.com/spam",
		Content: "",
	}
	decision, err := fe.FilterEntry(context.Background(), testEntry, "", []string{})
	if err != nil {
		t.Fatalf("Error filtering entry: %v", err)
	}
	if decision != FilterDiscard {
		t.Error("Expected entry matching 'discard' filter to be discarded")
	}

	// Test that entries NOT matching the "discard" filter are kept (blacklist behavior)
	testEntry.Title = "Legitimate news article"
	decision, err = fe.FilterEntry(context.Background(), testEntry, "", []string{})
	if err != nil {
		t.Fatalf("Error filtering entry: %v", err)
	}
	if decision != FilterKeep {
		t.Error("Expected entry NOT matching 'discard' filter to be kept")
	}
}

// TestFilterEngine_MixedKeepDiscardFilters tests behavior when both keep and discard filters are present
func TestFilterEngine_MixedKeepDiscardFilters(t *testing.T) {
	env := setupTestDB(t)
	defer env.db.Close()

	// Add filter tables to the test database
	_, err := env.db.Exec(database.Schema)
	if err != nil {
		t.Fatalf("Failed to create filter tables: %v", err)
	}

	fe := NewFilterEngine(env.db)

	// Mock active filter groups with both keep and discard filters
	fe.cacheMutex.Lock()
	fe.cachedGroups = []database.FilterGroup{
		{
			ID:       1,
			Name:     "Keep Go Posts",
			Action:   "keep",
			Priority: 1,
			IsActive: true,
			Rules: []database.FilterGroupRule{
				{
					ID:       1,
					FilterID: 1,
					Operator: "OR",
					Position: 0,
					Filter: &database.EntryFilter{
						ID:            1,
						Pattern:       "Go",
						PatternType:   "keyword",
						TargetType:    "title",
						CaseSensitive: false,
					},
				},
			},
		},
		{
			ID:       2,
			Name:     "Discard Spam",
			Action:   "discard",
			Priority: 2,
			IsActive: true,
			Rules: []database.FilterGroupRule{
				{
					ID:       2,
					FilterID: 2,
					Operator: "OR",
					Position: 0,
					Filter: &database.EntryFilter{
						ID:            2,
						Pattern:       "spam",
						PatternType:   "keyword",
						TargetType:    "title",
						CaseSensitive: false,
					},
				},
			},
		},
	}
	fe.lastUpdated = time.Now()
	fe.cacheMutex.Unlock()

	// When keep filters are present, only keep filters should matter
	// Test: Entry matches keep filter → should be kept
	testEntry := &database.Entry{
		ID: 1,
		FeedID: 1,
		Title: "Learning Go Programming",
		URL: "http://example.com/go",
		Content: "",
	}
	decision, err := fe.FilterEntry(context.Background(), testEntry, "", []string{})
	if err != nil {
		t.Fatalf("Error filtering entry: %v", err)
	}
	if decision != FilterKeep {
		t.Error("Expected entry matching 'keep' filter to be kept even with discard filters present")
	}

	// Test: Entry matches discard filter but no keep filter → should be discarded (because keep filters are present)
	testEntry.Title = "This is spam content"
	decision, err = fe.FilterEntry(context.Background(), testEntry, "", []string{})
	if err != nil {
		t.Fatalf("Error filtering entry: %v", err)
	}
	if decision != FilterDiscard {
		t.Error("Expected entry not matching any 'keep' filter to be discarded when keep filters are present")
	}

	// Test: Entry matches neither keep nor discard filter → should be discarded (whitelist behavior)
	testEntry.Title = "Python Tutorial"
	decision, err = fe.FilterEntry(context.Background(), testEntry, "", []string{})
	if err != nil {
		t.Fatalf("Error filtering entry: %v", err)
	}
	if decision != FilterDiscard {
		t.Error("Expected entry matching no 'keep' filters to be discarded (whitelist behavior)")
	}
}

// TestFilterEngine_RealDatabaseKeepFilters tests keep filters with actual database operations
func TestFilterEngine_RealDatabaseKeepFilters(t *testing.T) {
	env := setupTestDB(t)
	defer env.db.Close()

	// Add filter tables to the test database
	_, err := env.db.Exec(database.Schema)
	if err != nil {
		t.Fatalf("Failed to create filter tables: %v", err)
	}

	db := &database.DB{DB: env.db}
	fe := NewFilterEngine(env.db)

	// Create a keep filter in the database
	filter, err := db.CreateEntryFilter(context.Background(), "Keep Go Articles", "Go", "keyword", "title", false)
	if err != nil {
		t.Fatalf("Failed to create filter: %v", err)
	}

	// Create a keep filter group
	group, err := db.CreateFilterGroup(context.Background(), "Keep Technical", "keep", 1, "")
	if err != nil {
		t.Fatalf("Failed to create filter group: %v", err)
	}

	// Add the filter to the group
	err = db.AddFilterToGroup(context.Background(), group.ID, filter.ID, "OR", 0)
	if err != nil {
		t.Fatalf("Failed to add filter to group: %v", err)
	}

	// Clear any cached groups to force fresh database read
	fe.InvalidateCache()

	// Test with an entry that matches the keep filter
	testEntry := &database.Entry{
		ID: 1,
		FeedID: 1,
		Title: "Learning Go Programming",
		URL: "http://example.com/go",
		Content: "",
	}
	decision, err := fe.FilterEntry(context.Background(), testEntry, "", []string{})
	if err != nil {
		t.Fatalf("Error filtering matching entry: %v", err)
	}
	if decision != FilterKeep {
		t.Errorf("Expected keep filter to keep matching entry, got decision: %v", decision)
	}

	// Test with an entry that does NOT match the keep filter
	testEntry.Title = "Python Tutorial"
	decision, err = fe.FilterEntry(context.Background(), testEntry, "", []string{})
	if err != nil {
		t.Fatalf("Error filtering non-matching entry: %v", err)
	}
	if decision != FilterDiscard {
		t.Errorf("Expected keep filter to discard non-matching entry (whitelist mode), got decision: %v", decision)
		
		// Debug: let's see what groups we actually got
		groups, err := fe.getActiveFilterGroups(context.Background())
		if err != nil {
			t.Fatalf("Error getting filter groups: %v", err)
		}
		t.Logf("Found %d filter groups:", len(groups))
		for i, group := range groups {
			t.Logf("  Group %d: ID=%d, Name=%s, Action=%s, Active=%t, Rules=%d", 
				i, group.ID, group.Name, group.Action, group.IsActive, len(group.Rules))
			for j, rule := range group.Rules {
				if rule.Filter != nil {
					t.Logf("    Rule %d: Pattern=%s, Type=%s, CaseSensitive=%t", 
						j, rule.Filter.Pattern, rule.Filter.PatternType, rule.Filter.CaseSensitive)
				}
			}
		}
	}
}
