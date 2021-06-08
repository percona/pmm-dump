package main

import (
	"os"
	"pmm-transferer/pkg/clickhouse"
	"pmm-transferer/pkg/victoriametrics"

	"github.com/alecthomas/kingpin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// TODO: lint checker; readme; git version command

func main() {
	var (
		transferer = kingpin.New("pmm-transferer", "Percona PMM Transferer")

		clickHouseURL      = transferer.Flag("click_house_url", "ClickHouse connection string").String()
		victoriaMetricsURL = transferer.Flag("victoria_metrics_url", "VictoriaMetrics connection string").String()
		enableVerboseMode  = transferer.Flag("verbose_mode", "Enable verbose mode").Short('v').Bool()

		exportCmd  = transferer.Command("export", "Export PMM Server metrics to dump file")
		outPath    = exportCmd.Flag("out", "Path to put out file").Short('o').String()
		tsSelector = exportCmd.Flag("ts_selector", "Time series selector to pass to VM").String()

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
		p := exportParams{
			outPath: *outPath,
		}

		if url := *victoriaMetricsURL; url != "" {
			p.victoriaMetrics = &victoriametrics.Config{
				ConnectionURL:      url,
				TimeSeriesSelector: *tsSelector,
			}
		}

		if url := *clickHouseURL; url != "" {
			p.clickHouse = &clickhouse.Config{
				ConnectionURL: url,
			}
		}

		if err = runExport(p); err != nil {
			log.Fatal().Msgf("Failed to export: %v", err)
		}
	case importCmd.FullCommand():
		log.Fatal().Msg("TO BE DONE") // TODO: import
	default:
		log.Fatal().Msgf("Undefined command found: %s", cmd)
	}
}
