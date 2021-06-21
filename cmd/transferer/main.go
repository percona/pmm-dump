package main

import (
	"os"
	"pmm-transferer/pkg/clickhouse"
	"pmm-transferer/pkg/dump"
	"pmm-transferer/pkg/transferer"
	"pmm-transferer/pkg/victoriametrics"
	"time"

	"github.com/alecthomas/kingpin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// TODO:
//  lint checker;
//  readme;
//  git version command;
//  end points ping;
//  vendor;
//  short versions of commands;
//  more logs;

func main() {
	var (
		cli = kingpin.New("pmm-transferer", "Percona PMM Transferer")

		clickHouseURL      = cli.Flag("click_house_url", "ClickHouse connection string").String()
		victoriaMetricsURL = cli.Flag("victoria_metrics_url", "VictoriaMetrics connection string").String()
		enableVerboseMode  = cli.Flag("verbose_mode", "Enable verbose mode").Short('v').Bool()

		exportCmd  = cli.Command("export", "Export PMM Server metrics to dump file")
		outPath    = exportCmd.Flag("out", "Path to put out file").Short('o').String()
		tsSelector = exportCmd.Flag("ts_selector", "Time series selector to pass to VM").String()
		start      = exportCmd.Flag("start", "Start date-time to filter exported metrics, ex. "+time.RFC3339).String()
		end        = exportCmd.Flag("end", "End date-time to filter exported metrics, ex. "+time.RFC3339).String()

		importCmd = cli.Command("import", "Import PMM Server metrics from dump file")
		dumpPath  = importCmd.Flag("dump_path", "Path to dump file").Short('d').Required().String()
	)

	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})
	if *enableVerboseMode {
		log.Logger = log.Logger.
			With().Caller().Logger(). // TODO: fix with caller log
			Level(zerolog.DebugLevel)
	}

	cmd, err := cli.DefaultEnvars().Parse(os.Args[1:])
	if err != nil {
		log.Fatal().Msgf("Error parsing parameters: %s", err.Error())
	}

	if *clickHouseURL == "" && *victoriaMetricsURL == "" {
		log.Fatal().Msg("Please, specify at least one data source via connection string")
	}

	var sources []dump.Source

	httpC := newClientHTTP()

	if url := *victoriaMetricsURL; url != "" {
		c := &victoriametrics.Config{
			ConnectionURL:      url,
			TimeSeriesSelector: *tsSelector,
		}

		sources = append(sources, victoriametrics.NewSource(httpC, *c))

		log.Info().Msgf("Got Victoria Metrics URL: %s", c.ConnectionURL)
	}

	if url := *clickHouseURL; url != "" {
		c := &clickhouse.Config{
			ConnectionURL: url,
		}

		// TODO: add clickhouse source

		log.Info().Msgf("Got ClickHouse URL: %s", c.ConnectionURL)
	}

	switch cmd {
	case exportCmd.FullCommand():
		var startTime, endTime *time.Time

		if *start != "" {
			start, err := time.Parse(time.RFC3339, *start)
			if err != nil {
				log.Fatal().Msgf("Error parsing start date-time: %v", err)
			}
			startTime = &start
		}

		if *end != "" {
			end, err := time.Parse(time.RFC3339, *end)
			if err != nil {
				log.Fatal().Msgf("Error parsing end date-time: %v", err)
			}
			endTime = &end
		}

		t, err := transferer.New(*outPath, sources)
		if err != nil {
			log.Fatal().Msgf("Failed to transfer: %v", err)
		}

		if err = t.Export(startTime, endTime); err != nil {
			log.Fatal().Msgf("Failed to export: %v", err)
		}

		log.Info().Msg("Successfully exported!")
	case importCmd.FullCommand():
		t, err := transferer.New(*dumpPath, sources)
		if err != nil {
			log.Fatal().Msgf("Failed to transfer: %v", err)
		}

		if err = t.Import(); err != nil {
			log.Fatal().Msgf("Failed to import: %v", err)
		}

		log.Info().Msg("Successfully imported!")
	default:
		log.Fatal().Msgf("Undefined command found: %s", cmd)
	}
}
