package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

const (
	minimumRounds            = 3
	maximumFirstScreenMS     = int64(60_000)
	maximumBootstrapMS       = int64(20 * 60 * 1_000)
	minimumBootstrapBytesSec = int64(4 << 20)
	maximumSessionP95Micros  = int64(50_000)
	maximumProjectP95Micros  = int64(200_000)
	maximumUsageP95Micros    = int64(50_000)
	maximumQuotaP95Micros    = int64(750_000)
	maximumPopoverP95Micros  = int64(1_000_000)
	maximumPeakRSSBytes      = int64(1 << 30)
	maximumWALBytes          = int64(512 << 20)
)

type roundSummary struct {
	Version              string `json:"version"`
	Result               string `json:"result"`
	SourceFilesBefore    int    `json:"sourceFilesBefore"`
	BootstrapMS          int64  `json:"bootstrapMs"`
	FirstScreenMS        int64  `json:"firstScreenMs"`
	BootstrapBytes       int64  `json:"bootstrapBytes"`
	BootstrapBytesPerSec int64  `json:"bootstrapBytesPerSec"`
	PeakRSSBytes         int64  `json:"peakRssBytes"`
	AverageCPUPercent    int64  `json:"averageCpuPercent"`
	DatabaseBytes        int64  `json:"databaseBytes"`
	WALBytes             int64  `json:"walBytes"`
	WarmQuerySamples     int    `json:"warmQuerySamples"`
	SessionP95Micros     int64  `json:"sessionP95Micros"`
	ProjectP95Micros     int64  `json:"projectP95Micros"`
	UsageP95Micros       int64  `json:"usageP95Micros"`
	QuotaP95Micros       int64  `json:"quotaP95Micros"`
	PopoverP95Micros     int64  `json:"popoverP95Micros"`
	FullHistoryReady     bool   `json:"fullHistoryReady"`
	DTOPrivacyPassed     bool   `json:"dtoPrivacyPassed"`
	SourceReadOnlyPassed bool   `json:"sourceReadOnlyPassed"`
	IsolatedStoreClosed  bool   `json:"isolatedStoreClosed"`
	IsolatedRootRemoved  bool   `json:"isolatedRootRemoved"`
}

type aggregate struct {
	Version                   string `json:"version"`
	Result                    string `json:"result"`
	Rounds                    int    `json:"rounds"`
	SourceFilesMin            int    `json:"sourceFilesMin"`
	SourceFilesMax            int    `json:"sourceFilesMax"`
	FirstScreenP50MS          int64  `json:"firstScreenP50Ms"`
	FirstScreenP95MS          int64  `json:"firstScreenP95Ms"`
	BootstrapP50MS            int64  `json:"bootstrapP50Ms"`
	BootstrapP95MS            int64  `json:"bootstrapP95Ms"`
	BootstrapThroughputP50BPS int64  `json:"bootstrapThroughputP50Bps"`
	BootstrapThroughputP95BPS int64  `json:"bootstrapThroughputP95Bps"`
	PeakRSSMaxBytes           int64  `json:"peakRssMaxBytes"`
	AverageCPUMaxPercent      int64  `json:"averageCpuMaxPercent"`
	DatabaseMaxBytes          int64  `json:"databaseMaxBytes"`
	WALMaxBytes               int64  `json:"walMaxBytes"`
	SessionQueryP95MaxMicros  int64  `json:"sessionQueryP95MaxMicros"`
	ProjectQueryP95MaxMicros  int64  `json:"projectQueryP95MaxMicros"`
	UsageQueryP95MaxMicros    int64  `json:"usageQueryP95MaxMicros"`
	QuotaQueryP95MaxMicros    int64  `json:"quotaQueryP95MaxMicros"`
	PopoverQueryP95MaxMicros  int64  `json:"popoverQueryP95MaxMicros"`
}

func main() {
	input := flag.String("summaries", "", "comma-separated content-free M11 summary files")
	flag.Parse()
	paths := strings.Split(*input, ",")
	if *input == "" || len(paths) < minimumRounds {
		fatal("at least three summary files are required")
	}
	rounds := make([]roundSummary, 0, len(paths))
	for _, path := range paths {
		value, err := readRound(path)
		if err != nil {
			fatal("invalid round summary")
		}
		rounds = append(rounds, value)
	}
	result, err := aggregateRounds(rounds)
	if err != nil {
		fatal(err.Error())
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fatal("encode aggregate")
	}
}

func readRound(path string) (roundSummary, error) {
	file, err := os.Open(path)
	if err != nil {
		return roundSummary{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	var value roundSummary
	if err := decoder.Decode(&value); err != nil {
		return roundSummary{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return roundSummary{}, errors.New("trailing JSON value")
	}
	return value, nil
}

func aggregateRounds(rounds []roundSummary) (aggregate, error) {
	if len(rounds) < minimumRounds {
		return aggregate{}, errors.New("insufficient rounds")
	}
	firstScreen := make([]int64, 0, len(rounds))
	bootstrap := make([]int64, 0, len(rounds))
	throughput := make([]int64, 0, len(rounds))
	result := aggregate{Version: "m11-performance-v1", Result: "passed", Rounds: len(rounds)}
	for index, round := range rounds {
		if round.Version != "m11-real-home-v1" || round.Result != "passed" ||
			!round.FullHistoryReady || !round.DTOPrivacyPassed || !round.SourceReadOnlyPassed ||
			!round.IsolatedStoreClosed || !round.IsolatedRootRemoved || round.WarmQuerySamples < 30 ||
			round.SourceFilesBefore <= 0 || round.BootstrapBytes <= 0 ||
			round.PeakRSSBytes <= 0 || round.AverageCPUPercent <= 0 || round.DatabaseBytes <= 0 ||
			round.SessionP95Micros <= 0 || round.ProjectP95Micros <= 0 ||
			round.UsageP95Micros <= 0 || round.QuotaP95Micros <= 0 || round.PopoverP95Micros <= 0 {
			return aggregate{}, fmt.Errorf("round %d contract failed", index+1)
		}
		if round.FirstScreenMS <= 0 || round.FirstScreenMS > maximumFirstScreenMS ||
			round.BootstrapMS <= 0 || round.BootstrapMS > maximumBootstrapMS ||
			round.BootstrapBytesPerSec < minimumBootstrapBytesSec || round.PeakRSSBytes > maximumPeakRSSBytes ||
			round.WALBytes > maximumWALBytes || round.SessionP95Micros > maximumSessionP95Micros ||
			round.ProjectP95Micros > maximumProjectP95Micros || round.UsageP95Micros > maximumUsageP95Micros ||
			round.QuotaP95Micros > maximumQuotaP95Micros || round.PopoverP95Micros > maximumPopoverP95Micros {
			return aggregate{}, fmt.Errorf("round %d threshold failed", index+1)
		}
		if index == 0 || round.SourceFilesBefore < result.SourceFilesMin {
			result.SourceFilesMin = round.SourceFilesBefore
		}
		result.SourceFilesMax = max(result.SourceFilesMax, round.SourceFilesBefore)
		result.PeakRSSMaxBytes = max(result.PeakRSSMaxBytes, round.PeakRSSBytes)
		result.AverageCPUMaxPercent = max(result.AverageCPUMaxPercent, round.AverageCPUPercent)
		result.DatabaseMaxBytes = max(result.DatabaseMaxBytes, round.DatabaseBytes)
		result.WALMaxBytes = max(result.WALMaxBytes, round.WALBytes)
		result.SessionQueryP95MaxMicros = max(result.SessionQueryP95MaxMicros, round.SessionP95Micros)
		result.ProjectQueryP95MaxMicros = max(result.ProjectQueryP95MaxMicros, round.ProjectP95Micros)
		result.UsageQueryP95MaxMicros = max(result.UsageQueryP95MaxMicros, round.UsageP95Micros)
		result.QuotaQueryP95MaxMicros = max(result.QuotaQueryP95MaxMicros, round.QuotaP95Micros)
		result.PopoverQueryP95MaxMicros = max(result.PopoverQueryP95MaxMicros, round.PopoverP95Micros)
		firstScreen = append(firstScreen, round.FirstScreenMS)
		bootstrap = append(bootstrap, round.BootstrapMS)
		throughput = append(throughput, round.BootstrapBytesPerSec)
	}
	result.FirstScreenP50MS, result.FirstScreenP95MS = percentiles(firstScreen)
	result.BootstrapP50MS, result.BootstrapP95MS = percentiles(bootstrap)
	result.BootstrapThroughputP50BPS, result.BootstrapThroughputP95BPS = percentiles(throughput)
	return result, nil
}

func percentiles(values []int64) (int64, int64) {
	ordered := append([]int64(nil), values...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left] < ordered[right] })
	if len(ordered) == 0 {
		return 0, 0
	}
	return ordered[nearestRankIndex(len(ordered), 50)], ordered[nearestRankIndex(len(ordered), 95)]
}

func nearestRankIndex(count, percentile int) int {
	return (count*percentile+99)/100 - 1
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "M11-PERF-001:", message)
	os.Exit(1)
}
