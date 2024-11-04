package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port           int
	DBPath         string
	DataPath       string
	ProductionMode bool
}

func GetConfig() Config {
	config := Config{
		Port:           8080, // default port
		DBPath:         "data/infoscope.db",
		DataPath:       "data",
		ProductionMode: false, // default to development mode
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
	if dataPath := os.Getenv("INFOSCOPE_DATA_PATH"); dataPath != "" {
		config.DataPath = dataPath
	}
	if prodMode := os.Getenv("INFOSCOPE_PRODUCTION"); prodMode == "true" {
		config.ProductionMode = true
	}

	return config
}

func (c Config) GetAddress() string {
	return fmt.Sprintf(":%d", c.Port)
}
