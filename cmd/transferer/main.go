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
		loadCheckerURL     = cli.Flag("load_checker_url", "Load checker connection string").String()
		enableVerboseMode  = cli.Flag("verbose_mode", "Enable verbose mode").Short('v').Bool()
		allowInsecureCerts = cli.Flag("allow-insecure-certs", "Accept any certificate presented by the server and any host name in that certificate").Bool()

		exportCmd  = cli.Command("export", "Export PMM Server metrics to dump file")
		outPath    = exportCmd.Flag("out", "Path to put out file").Short('o').String()
		tsSelector = exportCmd.Flag("ts_selector", "Time series selector to pass to VM").String()
		start      = exportCmd.Flag("start", "Start date-time to filter exported metrics, ex. "+time.RFC3339).String()
		end        = exportCmd.Flag("end", "End date-time to filter exported metrics, ex. "+time.RFC3339).String()
		where      = exportCmd.Flag("where", "ClickHouse only. WHERE statement").Short('w').String()

		importCmd = cli.Command("import", "Import PMM Server metrics from dump file")
		dumpPath  = importCmd.Flag("dump_path", "Path to dump file").Short('d').Required().String()
	)

	ctx := context.Background()

	log.Logger = log.Output(zerolog.ConsoleWriter{
		Out:        os.Stderr,
		NoColor:    true,
		TimeFormat: time.RFC3339,
	})

	cmd, err := cli.DefaultEnvars().Parse(os.Args[1:])
	if err != nil {
		log.Fatal().Msgf("Error parsing parameters: %s", err.Error())
	}

	if *enableVerboseMode {
		log.Logger = log.Logger.
			With().Caller().Logger().
			Hook(goroutineLoggingHook{}).
			Level(zerolog.DebugLevel)
	} else {
		log.Logger = log.Logger.
			Level(zerolog.InfoLevel)
	}

	if *clickHouseURL == "" && *victoriaMetricsURL == "" {
		log.Fatal().Msg("Please, specify at least one data source via connection string")
	}

	var sources []dump.Source

	log.Debug().Msg("Setting up HTTP client...")

	httpC := newClientHTTP(*allowInsecureCerts)

	if url := *victoriaMetricsURL; url != "" {
		c := &victoriametrics.Config{
			ConnectionURL:      url,
			TimeSeriesSelector: *tsSelector,
		}

		sources = append(sources, victoriametrics.NewSource(httpC, *c))

		log.Debug().Msgf("Got Victoria Metrics URL: %s", c.ConnectionURL)
	}

	var clickhouseSource *clickhouse.Source
	if url := *clickHouseURL; url != "" {
		c := &clickhouse.Config{
			ConnectionURL: url,
		}
		if where != nil {
			c.Where = *where
		}

		clickhouseSource, err = clickhouse.NewSource(ctx, *c)
		if err != nil {
			log.Fatal().Msgf("Failed to create ClickHouse source: %s", err.Error())
			return
		}

		sources = append(sources, clickhouseSource)

		log.Debug().Msgf("Got ClickHouse URL: %s", c.ConnectionURL)
	}

	switch cmd {
	case exportCmd.FullCommand():
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

		if *clickHouseURL != "" {
			chChunks, err := clickhouseSource.SplitIntoChunks()
			if err != nil {
				log.Fatal().Msgf("Failed to create clickhouse chunks: %s", err.Error())
			}
			chunks = append(chunks, chChunks...)
		}

		pool, err := dump.NewChunkPool(chunks)
		if err != nil {
			log.Fatal().Msgf("Failed to generate chunk pool: %v", err)
		}

		if *loadCheckerURL == "" {
			log.Fatal().Msgf("load_checker_url should be provided")
		}
		lc := transferer.NewLoadChecker(ctx, httpC, *loadCheckerURL)

		if err = t.Export(ctx, lc, pool); err != nil {
			log.Fatal().Msgf("Failed to export: %v", err)
		}
	case importCmd.FullCommand():
		t, err := transferer.New(*dumpPath, sources)
		if err != nil {
			log.Fatal().Msgf("Failed to transfer: %v", err)
		}

		if err = t.Import(); err != nil {
			log.Fatal().Msgf("Failed to import: %v", err)
		}
	default:
		log.Fatal().Msgf("Undefined command found: %s", cmd)
	}
}
