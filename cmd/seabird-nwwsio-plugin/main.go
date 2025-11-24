package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	seabirdPlugin "github.com/seabird-chat/seabird-nwwsio-plugin"
)

func main() {
	_ = godotenv.Load()

	if isatty.IsTerminal(os.Stdout.Fd()) {
		log.Logger = zerolog.New(zerolog.NewConsoleWriter()).With().Timestamp().Logger()
	} else {
		log.Logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
	}

	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	coreURL := os.Getenv("SEABIRD_HOST")
	coreToken := os.Getenv("SEABIRD_TOKEN")

	if coreURL == "" || coreToken == "" {
		log.Fatal().Msg("Missing SEABIRD_HOST or SEABIRD_TOKEN")
	}

	nwwsioUsername := os.Getenv("NWWSIO_USERNAME")
	nwwsioPassword := os.Getenv("NWWSIO_PASSWORD")
	if nwwsioUsername == "" || nwwsioPassword == "" {
		log.Fatal().Msg("Missing NWWSIO_USERNAME or NWWSIO_PASSWORD")
	}

	c, err := seabirdPlugin.NewSeabirdClient(coreURL, coreToken, nwwsioUsername, nwwsioPassword)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize seabird client")
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		if err := c.Shutdown(); err != nil {
			log.Error().Err(err).Msg("Error during shutdown")
		}
		os.Exit(0)
	}()

	err = c.Run()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to run client")
	}
}
