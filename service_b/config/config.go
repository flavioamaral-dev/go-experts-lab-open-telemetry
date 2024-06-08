package config

import (
	"os"
)

type Config struct {
	UrlZipKin string `mapstructure:"URL_ZIPKIN"`
}

func LoadConfig() *Config {
	return &Config{
		UrlZipKin: os.Getenv("URL_ZIPKIN"),
	}
}
