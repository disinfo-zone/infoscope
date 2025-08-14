package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port                   int
	DBPath                 string
	DataPath               string
	WebPath                string
	ProductionMode         bool
	DisableTemplateUpdates bool
}

func GetConfig() Config {
	config := Config{
		Port:                   8080,
		DBPath:                 "data/infoscope.db",
		DataPath:               "data",
		WebPath:                "web",
		ProductionMode:         false,
		DisableTemplateUpdates: false,
	}

	// Override with environment variables if present
	if port := os.Getenv("INFOSCOPE_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			config.Port = p
		}
	}
	if dbPath := os.Getenv("INFOSCOPE_DB_PATH"); dbPath != "" {
		config.DBPath = dbPath
	}
	// DataPath already handled above; avoid duplicate
	if webPath := os.Getenv("INFOSCOPE_WEB_PATH"); webPath != "" {
		config.WebPath = webPath
	}
	if dataPath := os.Getenv("INFOSCOPE_DATA_PATH"); dataPath != "" {
		config.DataPath = dataPath
	}
	if prodMode := os.Getenv("INFOSCOPE_PRODUCTION"); prodMode == "true" {
		config.ProductionMode = true
	}
	if noUpdates := os.Getenv("INFOSCOPE_NO_TEMPLATE_UPDATES"); noUpdates == "true" {
		config.DisableTemplateUpdates = true
	}

	return config
}

func (c Config) GetAddress() string {
	return fmt.Sprintf(":%d", c.Port)
}
