package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/valyala/fasthttp"
	"os"
	"pmm-transferer/pkg/clickhouse"
	"pmm-transferer/pkg/dump"
	"pmm-transferer/pkg/grafana"
	"pmm-transferer/pkg/transferer"
	"pmm-transferer/pkg/victoriametrics"
	"time"

	"github.com/alecthomas/kingpin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	GitBranch string
	GitCommit string
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

		instances  = exportCmd.Flag("instance", "Service name to filter instances. Use multiple times to filter by multiple instances").Strings()
		dashboards = exportCmd.Flag("dashboard", "Dashboard name to filter. Use multiple times to filter by multiple dashboards").Strings()

		chunkTimeRange = exportCmd.Flag("chunk-time-range", "Time range to be fit into a single chunk (core metrics). "+
			"5 minutes by default, example '45s', '5m', '1h'").Default("5m").Duration()
		chunkRows = exportCmd.Flag("chunk-rows", "Amount of rows to fit into a single chunk (qan metrics)").Default("1000").Int()

		maxLoad = exportCmd.Flag("max-load", "Max load threshold values").
			Default(fmt.Sprintf("%v=50,%v=50", transferer.ThresholdCPU, transferer.ThresholdRAM)).String()
		criticalLoad = exportCmd.Flag("critical-load", "Critical load threshold values").
				Default(fmt.Sprintf("%v=70,%v=70", transferer.ThresholdCPU, transferer.ThresholdRAM)).String()

		stdout = exportCmd.Flag("stdout", "Redirect output to STDOUT").Bool()

		workersCount = exportCmd.Flag("workers", "Set the number of reading workers").Int()
		// import command options
		importCmd = cli.Command("import", "Import PMM Server metrics from dump file")

		// show meta command options
		showMetaCmd  = cli.Command("show-meta", "Shows metadata from the specified dump file")
		prettifyMeta = showMetaCmd.Flag("prettify", "Print meta in human readable format").Default("true").Bool()

		// version command options
		versionCmd = cli.Command("version", "Shows tool version of the binary")
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

	httpC := newClientHTTP(*allowInsecureCerts)

	switch cmd {
	case exportCmd.FullCommand():
		if *pmmURL == "" {
			log.Fatal().Msg("Please, specify PMM URL")
		}

		if !(*dumpQAN || *dumpCore) {
			log.Fatal().Msg("Please, specify at least one data source")
		}

		if *dumpQAN && *dumpCore && len(*instances) == 0 {
			if *where == "" && (*tsSelector != "" || len(*dashboards) > 0) {
				log.Warn().Msg("Filter for QAN found, but not for core dump. Core metrics for all metrics would be exported")
			} else if *where != "" && *tsSelector == "" && len(*dashboards) == 0 {
				log.Warn().Msg("Filter for core dump found, but not for QAN. QAN metrics for all metrics would be exported")
			}
		}

		var sources []dump.Source

		pmmConfig, err := getPMMConfig(*pmmURL, *victoriaMetricsURL, *clickHouseURL)
		if err != nil {
			log.Fatal().Err(err)
		}

		selectors, err := grafana.GetDashboardSelectors(*pmmURL, *dashboards, *instances, httpC)
		if err != nil {
			log.Fatal().Msgf("Error retrieving dashboard selectors: %v", err)
		}
		if *tsSelector != "" {
			selectors = append(selectors, *tsSelector)
		} else if len(selectors) == 0 && len(*instances) > 0 {
			for _, serviceName := range *instances {
				selectors = append(selectors, fmt.Sprintf(`{service_name="%s"}`, serviceName))
			}
		}
		vmSource, ok := prepareVictoriaMetricsSource(httpC, *dumpCore, pmmConfig.VictoriaMetricsURL, selectors)
		if ok {
			sources = append(sources, vmSource)
		}

		if *where == "" && len(*instances) > 0 {
			for i, serviceName := range *instances {
				if i != 0 {
					*where += " AND "
				}
				*where += fmt.Sprintf("service_name='%s'", serviceName)
			}
		}

		chSource, ok := prepareClickHouseSource(ctx, *dumpQAN, pmmConfig.ClickHouseURL, *where)
		if ok {
			sources = append(sources, chSource)
		}

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

		t, err := transferer.New(*dumpPath, *stdout, sources, *workersCount)
		if err != nil {
			log.Fatal().Msgf("Failed to transfer: %v", err)
		}

		var chunks []dump.ChunkMeta

		if *dumpCore {
			chunks = append(chunks, victoriametrics.SplitTimeRangeIntoChunks(startTime, endTime, *chunkTimeRange)...)
		}

		if *dumpQAN {
			chChunks, err := chSource.SplitIntoChunks(startTime, endTime, *chunkRows)
			if err != nil {
				log.Fatal().Msgf("Failed to create clickhouse chunks: %s", err.Error())
			}
			chunks = append(chunks, chChunks...)
		}

		meta, err := composeMeta(*pmmURL, httpC)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to compose meta")
		}

		pool, err := dump.NewChunkPool(chunks)
		if err != nil {
			log.Fatal().Msgf("Failed to generate chunk pool: %v", err)
		}

		thresholds, err := transferer.ParseThresholdList(*maxLoad, *criticalLoad)
		if err != nil {
			log.Fatal().Err(err).Msgf("Failed to parse max/critical load args")
		}

		lc := transferer.NewLoadChecker(ctx, httpC, pmmConfig.VictoriaMetricsURL, thresholds)

		if err = t.Export(ctx, lc, *meta, pool); err != nil {
			log.Fatal().Msgf("Failed to export: %v", err)
		}
	case importCmd.FullCommand():
		if *pmmURL == "" {
			log.Fatal().Msg("Please, specify PMM URL")
		}

		if !(*dumpQAN || *dumpCore) {
			log.Fatal().Msg("Please, specify at least one data source")
		}

		var sources []dump.Source

		pmmConfig, err := getPMMConfig(*pmmURL, *victoriaMetricsURL, *clickHouseURL)
		if err != nil {
			log.Fatal().Err(err)
		}

		vmSource, ok := prepareVictoriaMetricsSource(httpC, *dumpCore, pmmConfig.VictoriaMetricsURL, nil)
		if ok {
			sources = append(sources, vmSource)
		}

		chSource, ok := prepareClickHouseSource(ctx, *dumpQAN, pmmConfig.ClickHouseURL, *where)
		if ok {
			sources = append(sources, chSource)
		}

		piped, err := checkPiped()
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to check if a program is piped")
		}

		if *dumpPath == "" && piped == false {
			log.Fatal().Msg("Please, specify path to dump file")
		}

		t, err := transferer.New(*dumpPath, piped, sources, *workersCount)
		if err != nil {
			log.Fatal().Msgf("Failed to transfer: %v", err)
		}

		meta, err := composeMeta(*pmmURL, httpC)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to compose meta")
		}

		if err = t.Import(*meta); err != nil {
			log.Fatal().Msgf("Failed to import: %v", err)
		}
	case showMetaCmd.FullCommand():
		piped, err := checkPiped()
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to check if a program is piped")
		}
		if *dumpPath == "" && piped == false {
			log.Fatal().Msg("Please, specify path to dump file")
		}

		meta, err := transferer.ReadMetaFromDump(*dumpPath, piped)
		if err != nil {
			log.Fatal().Msgf("Can't show meta: %v", err)
		}

		if *prettifyMeta {
			fmt.Printf("Build: %v\n", meta.Version.GitCommit)
			fmt.Printf("PMM Version: %v\n", meta.PMMServerVersion)
			fmt.Printf("Max Chunk Size: %v (%v)\n", ByteCountDecimal(meta.MaxChunkSize),
				ByteCountBinary(meta.MaxChunkSize))
		} else {
			jsonMeta, err := json.MarshalIndent(meta, "", "\t")
			if err != nil {
				log.Fatal().Msgf("Failed to format meta as json: %v", err)
			}

			fmt.Printf("%v\n", string(jsonMeta))
		}
	case versionCmd.FullCommand():
		fmt.Printf("Build: %v\n", GitCommit)
	default:
		log.Fatal().Msgf("Undefined command found: %s", cmd)
	}
}

func prepareVictoriaMetricsSource(httpC *fasthttp.Client, dumpCore bool, url string, selectors []string) (*victoriametrics.Source, bool) {
	if !dumpCore {
		return nil, false
	}

	c := &victoriametrics.Config{
		ConnectionURL:       url,
		TimeSeriesSelectors: selectors,
	}

	log.Debug().Msgf("Got Victoria Metrics URL: %s", c.ConnectionURL)

	return victoriametrics.NewSource(httpC, *c), true
}

func prepareClickHouseSource(ctx context.Context, dumpQAN bool, url, where string) (*clickhouse.Source, bool) {
	if !dumpQAN {
		return nil, false
	}

	c := &clickhouse.Config{
		ConnectionURL: url,
		Where:         where,
	}

	clickhouseSource, err := clickhouse.NewSource(ctx, *c)
	if err != nil {
		log.Fatal().Msgf("Failed to create ClickHouse source: %s", err.Error())
	}

	log.Debug().Msgf("Got ClickHouse URL: %s", c.ConnectionURL)

	return clickhouseSource, true
}
