package main

import (
	"os"

	"github.com/alecthomas/kingpin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// TODO: lint checker; readme; version command

func main() {
	var (
		transferer = kingpin.New("pmm-transferer", "Percona PMM Transferer")

		clickHouseURL      = transferer.Flag("click_house_url", "ClickHouse connection string").String()
		victoriaMetricsURL = transferer.Flag("victoria_metrics_url", "VictoriaMetrics connection string").String()
		enableVerboseMode  = transferer.Flag("verbose_mode", "Enable verbose mode").Short('v').Bool()

		exportCmd = transferer.Command("export", "Export PMM Server metrics to dump file")

		importCmd = transferer.Command("import", "Import PMM Server metrics from dump file")
	)

	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})
	if *enableVerboseMode {
		log.Logger = log.Logger.
			Level(zerolog.DebugLevel).
			With().Caller().
			Logger()
	}

	cmd, err := transferer.DefaultEnvars().Parse(os.Args[1:])
	if err != nil {
		log.Fatal().Msgf("Error parsing parameters: %s", err.Error())
	}

	if *clickHouseURL == "" && *victoriaMetricsURL == "" {
		log.Fatal().Msg("Please, specify at least one data source via connection string")
	}

	switch cmd {
	case exportCmd.FullCommand():
		if *clickHouseURL != "" {
			err = runClickHouseExport(*clickHouseURL)
			if err != nil {
				log.Fatal().Msgf("Failed to run click house export: %v", err)
			}
		}
		if *victoriaMetricsURL != "" {
			err = runVictoriaMetricsExport(*victoriaMetricsURL)
			if err != nil {
				log.Fatal().Msgf("Failed to run victoria metrics export: %v", err)
			}
		}
	case importCmd.FullCommand():
		log.Fatal().Msg("TO BE DONE")
	default:
		log.Fatal().Msgf("Undefined command found: %s", cmd)
	}
}
