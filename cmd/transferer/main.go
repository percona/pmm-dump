package main

import (
	"os"

	"github.com/alecthomas/kingpin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func init() {
	log.Logger = log.
		Output(zerolog.ConsoleWriter{Out: os.Stdout}).
		With().Caller().Logger()
}

var (
	transferer = kingpin.New("pmm-transferer", "Percona PMM Transferer")

	// TODO: is it possible to get URL from PMM Server? Get metrics indirectly through PMM Server?
	chURL = transferer.Flag("ch_url", "ClickHouse connection URL.").String()
	vmURL = transferer.Flag("vm_url", "VictoriaMetrics connection URL.").String()

	exportCmd = transferer.Command("export", "Export metrics from PMM Server to file.")

	importCmd = transferer.Command("import", "Import metrics to PMM Server from file.")
)

func main() {
	cmd, err := transferer.DefaultEnvars().Parse(os.Args[1:])
	if err != nil {
		log.Fatal().Msgf("Error parsing parameters: %s", err.Error())
	}

	if chURL != nil {
		log.Info().Str("ch_url", *chURL).Msg("ClickHouse URL supplied")
	}

	if vmURL != nil {
		log.Info().Str("vm_url", *vmURL).Msg("VictoriaMetrics URL supplied")
	}

	log.Info().
		Str("current_cmd", cmd).
		Str("export", exportCmd.FullCommand()).
		Str("import", importCmd.FullCommand()).
		Msg("Run successfully!")
}
