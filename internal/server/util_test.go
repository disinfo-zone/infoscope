package server

import (
	"testing"
)

func TestStripHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Basic HTML removal",
			input:    "<p>Hello <strong>world</strong>!</p>",
			expected: "Hello world!",
		},
		{
			name:     "Multiple tags and entities",
			input:    "<div><p>Test &amp; example with <a href='#'>link</a></p></div>",
			expected: "Test & example with link",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "No HTML",
			input:    "Plain text",
			expected: "Plain text",
		},
		{
			name:     "Whitespace normalization",
			input:    "<p>Text\n\nwith\t\tmultiple\r\nwhitespace</p>",
			expected: "Text with multiple whitespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripHTML(tt.input)
			if result != tt.expected {
				t.Errorf("stripHTML(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestTruncateText(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		maxLength int
		expected  string
	}{
		{
			name:      "Short text unchanged",
			input:     "Short text",
			maxLength: 20,
			expected:  "Short text",
		},
		{
			name:      "Long text truncated at word boundary",
			input:     "This is a very long sentence that should be truncated at a good word boundary",
			maxLength: 30,
			expected:  "This is a very long...",
		},
		{
			name:      "Very short maxLength",
			input:     "Hello world",
			maxLength: 5,
			expected:  "He...",
		},
		{
			name:      "Empty string",
			input:     "",
			maxLength: 10,
			expected:  "",
		},
		{
			name:      "Zero maxLength",
			input:     "Some text",
			maxLength: 0,
			expected:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateText(tt.input, tt.maxLength)
			if result != tt.expected {
				t.Errorf("truncateText(%q, %d) = %q, want %q", tt.input, tt.maxLength, result, tt.expected)
			}
		})
	}
}

func TestProcessBodyText(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		maxLength int
		expected  string
	}{
		{
			name:      "HTML content processing",
			input:     "<p>This is a <strong>test</strong> article with <em>formatting</em>.</p>",
			maxLength: 30,
			expected:  "This is a test article...",
		},
		{
			name:      "Empty content",
			input:     "",
			maxLength: 100,
			expected:  "",
		},
		{
			name:      "Plain text",
			input:     "Simple plain text content",
			maxLength: 50,
			expected:  "Simple plain text content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ProcessBodyText(tt.input, tt.maxLength)
			if result != tt.expected {
				t.Errorf("processBodyText(%q, %d) = %q, want %q", tt.input, tt.maxLength, result, tt.expected)
			}
		})
	}
}
