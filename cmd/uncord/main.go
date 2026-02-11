package main

import (
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	log.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
	log.Info().Msg("Starting Uncord Server")
	log.Error().Msg("Not yet implemented!")
}
