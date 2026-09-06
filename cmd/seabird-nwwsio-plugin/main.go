package main

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/seabird-chat/seabird-nwwsio-plugin/client"
)

func main() {
	if isatty.IsTerminal(os.Stdout.Fd()) {
		consoleWriter := zerolog.NewConsoleWriter()
		consoleWriter.FormatLevel = func(i interface{}) string {
			return strings.ToUpper(fmt.Sprintf("| %-6s|", i))
		}
		log.Logger = zerolog.New(consoleWriter).With().Timestamp().Logger()
	} else {
		log.Logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
	}

	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

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

	subscriptionFile := os.Getenv("SUBSCRIPTION_FILE")
	if subscriptionFile == "" {
		subscriptionFile = "./data/subscriptions.json"
	}

	c, err := client.NewSeabirdClient(client.Config{
		SeabirdCoreURL:     coreURL,
		SeabirdCoreToken:   coreToken,
		NWWSIOUsername:     nwwsioUsername,
		NWWSIOPassword:     nwwsioPassword,
		SubscriptionFile:   subscriptionFile,
		FilterTestMessages: envBool("FILTER_TEST_MESSAGES", true),
	})
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

func envBool(name string, def bool) bool {
	value := os.Getenv(name)
	if value == "" {
		return def
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		log.Warn().Str("var", name).Str("value", value).Bool("default", def).Msg("Invalid boolean environment variable, using default")
		return def
	}
	return parsed
}
