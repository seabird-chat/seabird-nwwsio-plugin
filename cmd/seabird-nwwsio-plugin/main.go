package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
	seabirdPlugin "github.com/seabird-chat/seabird-nwwsio-plugin"
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

	// Verify things
	if coreURL == "" || coreToken == "" {
		log.Fatal("Missing SEABIRD_HOST or SEABIRD_TOKEN")
	}

	nwwsioUsername := os.Getenv("NWWSIO_USERNAME")
	nwwsioPassword := os.Getenv("NWWSIO_PASSWORD")
	if nwwsioUsername == "" || nwwsioPassword == "" {
		log.Fatal("Missing NWWSIO_USERNAME or NWWSIO_PASSWORD")
	}

	c, err := seabirdPlugin.NewSeabirdClient(coreURL, coreToken, nwwsioUsername, nwwsioPassword)
	if err != nil {
		log.Fatalf("Failed to initialize seabird client: %v", err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		if err := c.Shutdown(); err != nil {
			log.Printf("Error during shutdown: %v", err)
		}
		os.Exit(0)
	}()

	err = c.Run()
	if err != nil {
		log.Fatal(err)
	}
}
