// Save as: internal/server/settings.go
package server

// Define the settings manager
type SettingsManager struct {
	db map[SettingKey]string
}

func NewSettingsManager() *SettingsManager {
	return &SettingsManager{
		db: map[SettingKey]string{
			SettingSiteTitle:      "infoscope_",
			SettingMaxPosts:       "33",
			SettingUpdateInterval: "900",
		},
	}
}

// GetSetting returns a setting value with fallback to default
func (s *SettingsManager) GetSetting(key SettingKey) string {
	if value, ok := s.db[key]; ok {
		return value
	}
	return s.getDefaultValue(key)
}

// getDefaultValue returns the default value for a setting
func (s *SettingsManager) getDefaultValue(key SettingKey) string {
	switch key {
	case SettingSiteTitle:
		return "infoscope_"
	case SettingMaxPosts:
		return "33"
	case SettingUpdateInterval:
		return "900"
	default:
		return ""
	}
}
