package config

import "os"

type Config struct {
	UrlServiceB string `mapstructure:"URL_SERVICE_B"`
	UrlZipKin   string `mapstructure:"URL_ZIPKIN"`
}

func LoadConfig() *Config {
	return &Config{
		UrlServiceB: os.Getenv("URL_SERVICE_B"),
		UrlZipKin:   os.Getenv("URL_ZIPKIN"),
	}
}
