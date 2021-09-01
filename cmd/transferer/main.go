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

		// general options
		pmmURL = cli.Flag("pmm-url", "PMM connection string").String()

		victoriaMetricsURL = cli.Flag("victoria-metrics-url", "VictoriaMetrics connection string").String()
		clickHouseURL      = cli.Flag("click-house-url", "ClickHouse connection string").String()

		dumpCore = cli.Flag("dump-core", "Specify to export/import core metrics").Default("true").Bool()
		dumpQAN  = cli.Flag("dump-qan", "Specify to export/import QAN metrics").Bool()

		enableVerboseMode  = cli.Flag("verbose", "Enable verbose mode").Short('v').Bool()
		allowInsecureCerts = cli.Flag("allow-insecure-certs",
			"Accept any certificate presented by the server and any host name in that certificate").Bool()

		dumpPath = cli.Flag("dump-path", "Path to dump file").Short('d').String()

		// export command options
		exportCmd = cli.Command("export", "Export PMM Server metrics to dump file."+
			"By default only the 4 last hours are exported, but it can be configured via start-ts/end-ts options")

		start = exportCmd.Flag("start-ts",
			"Start date-time to filter exported metrics, ex. "+time.RFC3339).String()
		end = exportCmd.Flag("end-ts",
			"End date-time to filter exported metrics, ex. "+time.RFC3339).String()

		tsSelector = exportCmd.Flag("ts-selector", "Time series selector to pass to VM").String()
		where      = exportCmd.Flag("where", "ClickHouse only. WHERE statement").Short('w').String()

		chunkTimeRange = exportCmd.Flag("chunk-time-range", "Time range to be fit into a single chunk (core metrics). "+
			"5 minutes by default, example '45s', '5m', '1h'").Default("5m").Duration()
		chunkRows = exportCmd.Flag("chunk-rows", "Amount of rows to fit into a single chunk (qan metrics)").Default("1000").Int()

		maxLoad      = exportCmd.Flag("max-load", "Max load threshold values").String()
		criticalLoad = exportCmd.Flag("critical-load", "Critical load threshold values").String()

		// import command options
		importCmd = cli.Command("import", "Import PMM Server metrics from dump file")
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

	if *pmmURL == "" && *victoriaMetricsURL == "" && *clickHouseURL == "" {
		log.Fatal().Msg("Please, specify PMM URL")
	}

	if !(*dumpQAN || *dumpCore) {
		log.Fatal().Msg("Please, specify at least one data source")
	}

	var sources []dump.Source

	log.Debug().Msg("Setting up HTTP client...")

	httpC := newClientHTTP(*allowInsecureCerts)

	pmmConfig, err := getPMMConfig(*pmmURL, *victoriaMetricsURL, *clickHouseURL)
	if err != nil {
		log.Fatal().Err(err)
	}

	if *dumpCore {
		c := &victoriametrics.Config{
			ConnectionURL:      pmmConfig.VictoriaMetricsURL,
			TimeSeriesSelector: *tsSelector,
		}

		sources = append(sources, victoriametrics.NewSource(httpC, *c))

		log.Debug().Msgf("Got Victoria Metrics URL: %s", c.ConnectionURL)
	}

	var clickhouseSource *clickhouse.Source
	if *dumpQAN {
		c := &clickhouse.Config{
			ConnectionURL: pmmConfig.ClickHouseURL,
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

		t, err := transferer.New(*dumpPath, sources)
		if err != nil {
			log.Fatal().Msgf("Failed to transfer: %v", err)
		}

		var chunks []dump.ChunkMeta

		if *dumpCore {
			chunks = append(chunks, victoriametrics.SplitTimeRangeIntoChunks(startTime, endTime, *chunkTimeRange)...)
		}

		if *dumpQAN {
			chChunks, err := clickhouseSource.SplitIntoChunks(*chunkRows)
			if err != nil {
				log.Fatal().Msgf("Failed to create clickhouse chunks: %s", err.Error())
			}
			chunks = append(chunks, chChunks...)
		}

		pool, err := dump.NewChunkPool(chunks)
		if err != nil {
			log.Fatal().Msgf("Failed to generate chunk pool: %v", err)
		}

		lc, err := transferer.NewLoadChecker(ctx, httpC, pmmConfig.VictoriaMetricsURL, *maxLoad, *criticalLoad)
		if err != nil {
			log.Fatal().Msgf("Failed to set threshold values: %v", err)
		}

		if err = t.Export(ctx, lc, pool); err != nil {
			log.Fatal().Msgf("Failed to export: %v", err)
		}
	case importCmd.FullCommand():
		if *dumpPath == "" {
			log.Fatal().Msg("Please, specify path to dump file")
		}

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
