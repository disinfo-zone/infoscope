package server

import "testing"

func TestImageExtensionUsesDetectedContentType(t *testing.T) {
	tests := map[string]string{
		"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif",
		"image/webp": ".webp", "image/x-icon": ".ico",
	}
	for contentType, want := range tests {
		got, err := imageExtension(contentType)
		if err != nil || got != want {
			t.Fatalf("imageExtension(%q) = %q, %v; want %q", contentType, got, err, want)
		}
	}
	if _, err := imageExtension("text/html"); err == nil {
		t.Fatal("expected text/html to be rejected")
	}
}
