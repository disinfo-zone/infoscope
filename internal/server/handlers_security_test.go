package server

import (
	"testing"
)

func TestValidateTrackingCode(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		description string
	}{
		{
			name:        "empty_code",
			input:       "",
			expectError: false,
			description: "Empty tracking code should be allowed",
		},
		{
			name:        "valid_google_analytics",
			input:       `<script async src="https://www.googletagmanager.com/gtag/js?id=GA_TRACKING_ID"></script>`,
			expectError: false,
			description: "Valid Google Analytics script should be allowed",
		},
		{
			name:        "valid_tracking_pixel",
			input:       `<img src="https://analytics.example.com/pixel.gif" width="1" height="1" alt="">`,
			expectError: false,
			description: "Valid tracking pixel should be allowed",
		},
		{
			name:        "malicious_script",
			input:       `<script>alert('XSS')</script>`,
			expectError: true, // This should be rejected because it's inline JS
			description: "Malicious inline script should be rejected",
		},
		{
			name:        "dangerous_iframe",
			input:       `<iframe src="javascript:alert('XSS')" width="100" height="100"></iframe>`,
			expectError: true, // This should be rejected because javascript: URLs are not allowed
			description: "Dangerous iframe with javascript: URL should be rejected",
		},
		{
			name:        "valid_meta_tag",
			input:       `<meta name="google-site-verification" content="abc123">`,
			expectError: false,
			description: "Valid meta tag should be allowed",
		},
		{
			name:        "script_with_onclick",
			input:       `<script onclick="alert('XSS')" src="https://example.com/script.js"></script>`,
			expectError: false, // onclick will be removed but script with src should remain
			description: "Script with onclick should be sanitized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := validateTrackingCode(tt.input)
			
			if tt.expectError && err == nil {
				t.Errorf("Expected error for %s but got none. Input: %s", tt.description, tt.input)
			}
			
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error for %s: %v. Input: %s", tt.description, err, tt.input)
			}
			
			// For non-error cases, ensure we get some output
			if !tt.expectError && tt.input != "" && result == "" {
				t.Errorf("Expected non-empty result for %s but got empty string", tt.description)
			}
			
			t.Logf("Test %s: Input length=%d, Output length=%d, Error=%v", 
				tt.name, len(tt.input), len(result), err)
		})
	}
}

func TestValidateTrackingCodeSanitization(t *testing.T) {
	// Test that dangerous content is properly sanitized
	input := `<script src="https://example.com/analytics.js"></script><script>alert('xss')</script>`
	_, err := validateTrackingCode(input)
	
	// This should error because it contains inline JavaScript without src
	if err == nil {
		t.Error("Expected error for mixed valid and invalid scripts but got none")
	}
	
	// Test a case that should work - external script only
	input2 := `<script src="https://example.com/analytics.js" async></script>`
	result2, err2 := validateTrackingCode(input2)
	
	if err2 != nil {
		t.Errorf("Unexpected error for valid external script: %v", err2)
	}
	
	if len(result2) == 0 {
		t.Error("Expected sanitized output but got empty string")
	}
	
	t.Logf("Valid script test - Input: %s, Output: %s", input2, result2)
}

func TestValidateTrackingCodeAdvancedSecurity(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		description string
	}{
		{
			name:        "script_with_data_uri",
			input:       `<script src="data:text/javascript,alert('xss')"></script>`,
			expectError: true,
			description: "Script with data: URI should be rejected",
		},
		{
			name:        "script_with_localhost",
			input:       `<script src="http://localhost:8080/malicious.js"></script>`,
			expectError: true,
			description: "Script pointing to localhost on standard ports should be rejected",
		},
		{
			name:        "valid_multiple_scripts",
			input:       `<script src="https://www.googletagmanager.com/gtag/js"></script><script src="https://connect.facebook.net/en_US/fbevents.js"></script>`,
			expectError: false,
			description: "Multiple valid external scripts should be allowed",
		},
		{
			name:        "img_with_onerror",
			input:       `<img src="https://example.com/pixel.gif" onerror="alert('xss')">`,
			expectError: false, // onerror will be stripped, img will remain
			description: "Image with onerror should be sanitized",
		},
		{
			name:        "mixed_valid_invalid",
			input:       `<script src="https://analytics.com/script.js"></script><script>alert('xss')</script>`,
			expectError: true, // Should fail because of inline script
			description: "Mix of valid external and invalid inline script should be rejected",
		},
		{
			name:        "google_analytics_complete",
			input:       `<script async src="https://www.googletagmanager.com/gtag/js?id=GA_MEASUREMENT_ID"></script><script>window.dataLayer = window.dataLayer || [];function gtag(){dataLayer.push(arguments);}gtag('js', new Date());gtag('config', 'GA_MEASUREMENT_ID');</script>`,
			expectError: true, // Should fail due to inline script
			description: "Complete Google Analytics (with inline config) should be rejected",
		},
		{
			name:        "trusted_iframe",
			input:       `<iframe src="https://www.googletagmanager.com/ns.html?id=GTM-XXXXXX" height="0" width="0" style="display:none;visibility:hidden"></iframe>`,
			expectError: false,
			description: "Trusted Google Tag Manager iframe should be allowed",
		},
		{
			name:        "self_hosted_iframe",
			input:       `<iframe src="https://evil.com/tracking.html" width="1" height="1"></iframe>`,
			expectError: false, // Allow self-hosted analytics domains
			description: "Self-hosted iframe domain should be allowed for analytics flexibility",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := validateTrackingCode(tt.input)
			
			if tt.expectError && err == nil {
				t.Errorf("Expected error for %s but got none. Input: %s, Output: %s", tt.description, tt.input, result)
			}
			
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error for %s: %v. Input: %s", tt.description, err, tt.input)
			}
			
			t.Logf("Test %s: Error=%v, Input length=%d, Output length=%d", 
				tt.name, err, len(tt.input), len(result))
		})
	}
}

func TestValidateTrackingCodeRealWorldExamples(t *testing.T) {
	// Test real-world analytics snippets (external scripts only)
	tests := []struct {
		name  string
		input string
		valid bool
	}{
		{
			name:  "google_analytics_external_only",
			input: `<script async src="https://www.googletagmanager.com/gtag/js?id=G-XXXXXXXXXX"></script>`,
			valid: true,
		},
		{
			name:  "facebook_pixel_external_only",
			input: `<script async defer crossorigin="anonymous" src="https://connect.facebook.net/en_US/fbevents.js"></script>`,
			valid: true,
		},
		{
			name:  "hotjar_tracking",
			input: `<script async src="https://static.hotjar.com/c/hotjar-XXXXXX.js?sv=6"></script>`,
			valid: true,
		},
		{
			name:  "tracking_pixel_combination",
			input: `<img src="https://www.facebook.com/tr?id=XXXXXXXXX&ev=PageView&noscript=1" width="1" height="1" style="display:none">`,
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := validateTrackingCode(tt.input)
			
			if tt.valid && err != nil {
				t.Errorf("Valid tracking code was rejected: %v\nInput: %s", err, tt.input)
			}
			
			if !tt.valid && err == nil {
				t.Errorf("Invalid tracking code was accepted\nInput: %s\nOutput: %s", tt.input, result)
			}
			
			if tt.valid && len(result) == 0 {
				t.Errorf("Valid tracking code produced empty output\nInput: %s", tt.input)
			}
			
			t.Logf("Real world test %s: Success=%t, Output length=%d", tt.name, tt.valid == (err == nil), len(result))
		})
	}
}

func TestValidateTrackingCodeSelfHostedAnalytics(t *testing.T) {
	// Test self-hosted analytics scenarios including zerolens.disinfo.zone
	tests := []struct {
		name  string
		input string
		valid bool
		description string
	}{
		{
			name:  "zerolens_script",
			input: `<script src="https://zerolens.disinfo.zone/script.js"></script>`,
			valid: true,
			description: "zerolens.disinfo.zone script should be allowed",
		},
		{
			name:  "zerolens_with_attributes",
			input: `<script async defer src="https://zerolens.disinfo.zone/analytics.js" data-site="example"></script>`,
			valid: true,
			description: "zerolens script with additional attributes should be allowed",
		},
		{
			name:  "self_hosted_iframe",
			input: `<iframe src="https://analytics.mydomain.com/tracking.html" width="1" height="1" style="display:none"></iframe>`,
			valid: true,
			description: "Self-hosted analytics iframe should be allowed",
		},
		{
			name:  "custom_domain_script",
			input: `<script src="https://stats.mycompany.io/track.js"></script>`,
			valid: true,
			description: "Custom domain analytics script should be allowed",
		},
		{
			name:  "localhost_on_custom_port",
			input: `<script src="https://localhost:3000/analytics.js"></script>`,
			valid: true,
			description: "Localhost on non-standard port should be allowed for development",
		},
		{
			name:  "localhost_standard_port",
			input: `<script src="https://localhost/analytics.js"></script>`,
			valid: false,
			description: "Localhost on standard port should be blocked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := validateTrackingCode(tt.input)
			
			if tt.valid && err != nil {
				t.Errorf("Valid self-hosted analytics code was rejected: %v\nInput: %s\nDescription: %s", err, tt.input, tt.description)
			}
			
			if !tt.valid && err == nil {
				t.Errorf("Invalid analytics code was accepted\nInput: %s\nOutput: %s\nDescription: %s", tt.input, result, tt.description)
			}
			
			if tt.valid && len(result) == 0 {
				t.Errorf("Valid analytics code produced empty output\nInput: %s\nDescription: %s", tt.input, tt.description)
			}
			
			t.Logf("Self-hosted analytics test %s: Success=%t, Output length=%d, Description: %s", 
				tt.name, tt.valid == (err == nil), len(result), tt.description)
		})
	}
}
