package main

import (
	"context"
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

	ctx := context.Background()

	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})
	if *enableVerboseMode {
		log.Logger = log.Logger.
			With().Caller().Logger(). // TODO: fix with caller log
			Level(zerolog.DebugLevel)
	}

	log.Info().Msg("Parsing cli params...")

	cmd, err := cli.DefaultEnvars().Parse(os.Args[1:])
	if err != nil {
		log.Fatal().Msgf("Error parsing parameters: %s", err.Error())
	}

	if *clickHouseURL == "" && *victoriaMetricsURL == "" {
		log.Fatal().Msg("Please, specify at least one data source via connection string")
	}

	var sources []dump.Source

	log.Info().Msg("Setting up HTTP client...")

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

		// TODO\CH: add clickhouse source

		log.Info().Msgf("Got ClickHouse URL: %s", c.ConnectionURL)
	}

	switch cmd {
	case exportCmd.FullCommand():
		log.Info().Msg("Processing export...")

		var startTime, endTime time.Time

		if *end != "" {
			endTime, err = time.ParseInLocation(time.RFC3339, *end, time.UTC)
			if err != nil {
				log.Fatal().Msgf("Error parsing end date-time: %v", err)
			}
		} else {
			endTime = time.Now().UTC()
		}

		if *start != "" {
			startTime, err = time.ParseInLocation(time.RFC3339, *start, time.UTC)
			if err != nil {
				log.Fatal().Msgf("Error parsing start date-time: %v", err)
			}
		} else {
			startTime = endTime.Add(-1 * time.Hour * 4)
		}

		if startTime.After(endTime) {
			log.Fatal().Msg("Invalid time range: start > end")
		}

		t, err := transferer.New(*outPath, sources)
		if err != nil {
			log.Fatal().Msgf("Failed to transfer: %v", err)
		}

		var chunks []dump.ChunkMeta

		if *victoriaMetricsURL != "" {
			chunks = append(chunks, victoriametrics.SplitTimeRangeIntoChunks(startTime, endTime)...)
		}

		// TODO\CH: add chunks from clickhouse

		pool, err := dump.NewChunkPool(chunks)
		if err != nil {
			log.Fatal().Msgf("Failed to generate chunk pool: %v", err)
		}

		if err = t.Export(ctx, pool); err != nil {
			log.Fatal().Msgf("Failed to export: %v", err)
		}

		log.Info().Msg("Successfully exported!")
	case importCmd.FullCommand():
		log.Info().Msg("Processing import...")

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
