package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
	datadogPlugin "github.com/seabird-chat/seabird-nwwsio-plugin"
)

func main() {
	// Attempt to load from .env if it exists
	_ = godotenv.Load()

	var logger zerolog.Logger

	if isatty.IsTerminal(os.Stdout.Fd()) {
		logger = zerolog.New(zerolog.NewConsoleWriter())
	} else {
		logger = zerolog.New(os.Stdout)
	}

	logger = logger.With().Timestamp().Logger()
	logger.Level(zerolog.InfoLevel)

	coreURL := os.Getenv("SEABIRD_HOST")
	coreToken := os.Getenv("SEABIRD_TOKEN")
	dogstatsdEndpoint := os.Getenv("DOGSTATSD_ENDPOINT")

	// Verify things
	if coreURL == "" || coreToken == "" {
		log.Fatal("Missing SEABIRD_HOST or SEABIRD_TOKEN")
	}
	if dogstatsdEndpoint == "" {
		log.Fatal("Missing DOGSTATSD_ENDPOINT")
	}
	c, err := datadogPlugin.NewSeabirdClient(coreURL, coreToken, dogstatsdEndpoint)
	if err != nil {
		log.Fatalf("Failed to connect to seabird-core: %s", err)
	}
	log.Printf("Successfully connected to seabird-core at %s", coreURL)

	err = c.Run()
	if err != nil {
		log.Fatal(err)
	}
}
