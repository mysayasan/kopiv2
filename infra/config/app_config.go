package config

import (
	"encoding/json"
	"os"
)

func LoadAppConfiguration(file string) (*AppConfigModel, error) {
	config := AppConfigModel{}
	configFile, err := os.Open(file)
	if err != nil {
		return nil, err
	}

	defer configFile.Close()
	jsonParser := json.NewDecoder(configFile)
	jsonParser.Decode(&config)
	return &config, nil
}
