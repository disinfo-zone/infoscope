package server

import "testing"

func TestValidateBackupFilename(t *testing.T) {
	tests := []struct {
		name string
		ok   bool
	}{
		{"infoscope_backup_20260904_123456.json", true},
		{"..", false},
		{"../infoscope_backup_20260904_123456.json", false},
		{`..\infoscope_backup_20260904_123456.json`, false},
		{"other.json", false},
		{"infoscope_backup_20260904_123456.json.exe", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := validateBackupFilename(tt.name)
			if got != tt.ok {
				t.Fatalf("validateBackupFilename(%q) = %v, want %v", tt.name, got, tt.ok)
			}
		})
	}
}
