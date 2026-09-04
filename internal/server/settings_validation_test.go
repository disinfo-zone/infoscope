package server

import "testing"

func validSettingsForTest() Settings {
	return Settings{
		MaxPosts: 33, UpdateInterval: 900, BodyTextLength: 200,
		BackupIntervalHours: 24, BackupRetentionDays: 30,
	}
}

func TestValidateSettingsBounds(t *testing.T) {
	if err := validateSettings(validSettingsForTest()); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*Settings)
	}{
		{"zero posts", func(s *Settings) { s.MaxPosts = 0 }},
		{"excessive posts", func(s *Settings) { s.MaxPosts = 1001 }},
		{"fast updates", func(s *Settings) { s.UpdateInterval = 1 }},
		{"long previews", func(s *Settings) { s.BodyTextLength = 1001 }},
		{"excessive retention", func(s *Settings) { s.BackupRetentionDays = 3651 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := validSettingsForTest()
			tt.mutate(&s)
			if err := validateSettings(s); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
