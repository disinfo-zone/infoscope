// internal/feed/filter_test.go
package feed

import (
	"testing"

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
		CaseSensitive: false,
	}

	filter2 := &database.EntryFilter{
		ID:            2,
		Pattern:       "Rust",
		PatternType:   "keyword",
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
		CaseSensitive: false,
	}

	filter2 := &database.EntryFilter{
		ID:            2,
		Pattern:       "tutorial",
		PatternType:   "keyword",
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
