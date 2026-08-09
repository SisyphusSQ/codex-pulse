package quota

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/SisyphusSQ/codex-pulse/internal/runtimeclock"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const PaceContractVersion = "quota-pace-v1"

var (
	ErrInvalidPaceQuery     = errors.New("quota pace query is invalid")
	ErrQuotaPaceUnavailable = errors.New("quota pace projection is unavailable")
)

type PaceUnknownReason string

const (
	PaceUnknownWindowUnavailable PaceUnknownReason = "window_unavailable"
	PaceUnknownWindowInvalid     PaceUnknownReason = "window_invalid"
	PaceUnknownEvidenceStale     PaceUnknownReason = "evidence_stale"
	PaceUnknownSourceConflict    PaceUnknownReason = "source_conflict"
	PaceUnknownEvidenceSparse    PaceUnknownReason = "evidence_sparse"
	PaceUnknownEvidenceFlat      PaceUnknownReason = "evidence_flat"
	PaceUnknownEvidenceInvalid   PaceUnknownReason = "evidence_invalid"
)

type PaceForecastState string

const (
	PaceForecastUnavailable PaceForecastState = "unavailable"
	PaceForecastOnTrack     PaceForecastState = "on_track"
	PaceForecastAtRisk      PaceForecastState = "at_risk"
	PaceForecastExhausted   PaceForecastState = "exhausted"
)

type PaceForecastMethod string

const (
	PaceForecastMethodNone           PaceForecastMethod = "none"
	PaceForecastMethodRecentTheilSen PaceForecastMethod = "recent_theil_sen"
)

type PaceForecast struct {
	State             PaceForecastState  `json:"state"`
	Method            PaceForecastMethod `json:"method"`
	ExhaustAtMS       *int64             `json:"exhaustAtMs"`
	LeadBeforeResetMS *int64             `json:"leadBeforeResetMs"`
	EvidenceCount     int64              `json:"evidenceCount"`
	EvidenceSpanMS    int64              `json:"evidenceSpanMs"`
	UnknownReason     *PaceUnknownReason `json:"unknownReason"`
}

type PacePoint struct {
	ObservedAtMS     int64   `json:"observedAtMs"`
	ElapsedPercent   float64 `json:"elapsedPercent"`
	UsedPercent      float64 `json:"usedPercent"`
	RemainingPercent float64 `json:"remainingPercent"`
}

type PaceCycle struct {
	WindowGeneration int64       `json:"windowGeneration"`
	WindowStartAtMS  int64       `json:"windowStartAtMs"`
	ResetsAtMS       int64       `json:"resetsAtMs"`
	Complete         bool        `json:"complete"`
	Points           []PacePoint `json:"points"`
}

type PaceHistoryBandPoint struct {
	ElapsedPercent   float64 `json:"elapsedPercent"`
	MedianRemaining  float64 `json:"medianRemaining"`
	MinimumRemaining float64 `json:"minimumRemaining"`
	MaximumRemaining float64 `json:"maximumRemaining"`
	CycleCount       int64   `json:"cycleCount"`
}

type PaceWindow struct {
	WindowKind                      store.QuotaWindowKind  `json:"windowKind"`
	LimitID                         string                 `json:"limitId"`
	WindowStartAtMS                 *int64                 `json:"windowStartAtMs"`
	ResetsAtMS                      *int64                 `json:"resetsAtMs"`
	WindowMinutes                   *int64                 `json:"windowMinutes"`
	WindowGeneration                *int64                 `json:"windowGeneration"`
	UsedPercent                     *float64               `json:"usedPercent"`
	RemainingPercent                *float64               `json:"remainingPercent"`
	ElapsedPercent                  *float64               `json:"elapsedPercent"`
	PaceDeltaPP                     *float64               `json:"paceDeltaPp"`
	Forecast                        PaceForecast           `json:"forecast"`
	CurrentPoints                   []PacePoint            `json:"currentPoints"`
	PreviousCycle                   *PaceCycle             `json:"previousCycle"`
	HistoricalCycles                []PaceCycle            `json:"historicalCycles"`
	HistoryBand                     []PaceHistoryBandPoint `json:"historyBand"`
	HistoryCycleCount               int64                  `json:"historyCycleCount"`
	PreviousRemainingAtElapsed      *float64               `json:"previousRemainingAtElapsed"`
	HistoryMedianRemainingAtElapsed *float64               `json:"historyMedianRemainingAtElapsed"`
	UnknownReason                   *PaceUnknownReason     `json:"unknownReason"`
}

type PaceResponse struct {
	Version       string       `json:"version"`
	AccountScope  string       `json:"accountScope"`
	EvaluatedAtMS int64        `json:"evaluatedAtMs"`
	Windows       []PaceWindow `json:"windows"`
}

func (service *CurrentQueryService) Pace(
	ctx context.Context,
	evaluatedAtMS int64,
) (PaceResponse, error) {
	if service == nil || service.reader == nil || evaluatedAtMS < 0 ||
		evaluatedAtMS > runtimeclock.MaxTimestampMS {
		return PaceResponse{}, ErrInvalidPaceQuery
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return PaceResponse{}, err
	}
	snapshot, err := service.reader.QuotaCurrentSnapshot(
		ctx, store.QuotaAccountScopeDefault, evaluatedAtMS,
	)
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidRecord) {
		return PaceResponse{}, fmt.Errorf(
			"%w: stored facts are missing or invalid: %w", ErrQuotaPaceUnavailable, err,
		)
	}
	if err != nil {
		return PaceResponse{}, err
	}
	if snapshot.AccountScope != store.QuotaAccountScopeDefault ||
		snapshot.EvaluatedAtMS != evaluatedAtMS {
		return PaceResponse{}, fmt.Errorf("%w: snapshot identity is inconsistent", ErrInvalidPaceQuery)
	}
	windows := append([]store.QuotaCurrentWindowSnapshot(nil), snapshot.Windows...)
	sort.Slice(windows, func(left, right int) bool {
		leftRank := currentWindowRank(windows[left].Current.WindowKind)
		rightRank := currentWindowRank(windows[right].Current.WindowKind)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return windows[left].Current.LimitID < windows[right].Current.LimitID
	})
	response := PaceResponse{
		Version: PaceContractVersion, AccountScope: snapshot.AccountScope,
		EvaluatedAtMS: evaluatedAtMS, Windows: make([]PaceWindow, 0, len(windows)),
	}
	for _, facts := range windows {
		window, buildErr := buildPaceWindow(facts, evaluatedAtMS)
		if buildErr != nil {
			return PaceResponse{}, buildErr
		}
		response.Windows = append(response.Windows, window)
	}
	return response, nil
}

func buildPaceWindow(
	facts store.QuotaCurrentWindowSnapshot,
	evaluatedAtMS int64,
) (PaceWindow, error) {
	current := facts.Current
	window := PaceWindow{WindowKind: current.WindowKind, LimitID: current.LimitID}
	if current.AccountScope != store.QuotaAccountScopeDefault || current.LimitID == "" ||
		current.EvaluatedAtMS != evaluatedAtMS || evaluatedAtMS < 0 ||
		evaluatedAtMS > runtimeclock.MaxTimestampMS {
		return PaceWindow{}, fmt.Errorf("%w: current window identity is inconsistent", ErrInvalidPaceQuery)
	}
	if current.EffectiveUsedPercent == nil || current.WindowMinutes == nil ||
		current.ResetsAtMS == nil || current.WindowGeneration == nil {
		reason := PaceUnknownWindowUnavailable
		window.UnknownReason = &reason
		return window, nil
	}
	if *current.EffectiveUsedPercent < 0 || *current.EffectiveUsedPercent > 100 ||
		*current.WindowMinutes <= 0 || *current.ResetsAtMS < 0 ||
		*current.WindowGeneration != *current.ResetsAtMS {
		reason := PaceUnknownWindowInvalid
		window.UnknownReason = &reason
		return window, nil
	}
	durationMS := *current.WindowMinutes * 60_000
	if durationMS <= 0 || *current.ResetsAtMS < durationMS {
		reason := PaceUnknownWindowInvalid
		window.UnknownReason = &reason
		return window, nil
	}
	windowStartAtMS := *current.ResetsAtMS - durationMS
	elapsedPercent := float64(evaluatedAtMS-windowStartAtMS) / float64(durationMS) * 100
	if elapsedPercent < 0 {
		elapsedPercent = 0
	}
	if elapsedPercent > 100 {
		elapsedPercent = 100
	}
	usedPercent := *current.EffectiveUsedPercent
	remainingPercent := 100 - usedPercent
	paceDeltaPP := usedPercent - elapsedPercent
	window.WindowStartAtMS = &windowStartAtMS
	window.ResetsAtMS = cloneInt64(current.ResetsAtMS)
	window.WindowMinutes = cloneInt64(current.WindowMinutes)
	window.WindowGeneration = cloneInt64(current.WindowGeneration)
	window.UsedPercent = &usedPercent
	window.RemainingPercent = &remainingPercent
	window.ElapsedPercent = &elapsedPercent
	window.PaceDeltaPP = &paceDeltaPP
	observations := paceObservationsAllowedByArbitration(facts.Observations, facts.Evidence)
	window.CurrentPoints = currentPacePoints(current, observations)
	window.PreviousCycle, window.HistoricalCycles = historicalPaceCycles(
		current, observations,
	)
	window.HistoryCycleCount = int64(len(window.HistoricalCycles))
	if window.PreviousCycle != nil {
		window.PreviousRemainingAtElapsed = remainingAtElapsed(
			window.PreviousCycle.Points, elapsedPercent,
		)
	}
	window.HistoryBand = historyPaceBand(window.HistoricalCycles)
	historyRemaining := make([]float64, 0, len(window.HistoricalCycles))
	for _, cycle := range window.HistoricalCycles {
		if remaining := remainingAtElapsed(cycle.Points, elapsedPercent); remaining != nil {
			historyRemaining = append(historyRemaining, *remaining)
		}
	}
	window.HistoryMedianRemainingAtElapsed = medianFloat64(historyRemaining)
	window.Forecast = forecastPaceWindow(
		current, observations, windowStartAtMS, durationMS, evaluatedAtMS,
	)
	return window, nil
}

func paceObservationsAllowedByArbitration(
	observations []store.QuotaObservation,
	evidence []store.QuotaArbitrationEvidence,
) []store.QuotaObservation {
	if len(evidence) == 0 {
		return observations
	}
	allowed := make(map[string]*int64, len(evidence))
	for _, item := range evidence {
		switch item.Disposition {
		case store.QuotaEvidenceSelected,
			store.QuotaEvidenceEligible,
			store.QuotaEvidenceSuperseded:
			allowed[item.ObservationID] = cloneInt64(item.WindowGeneration)
		}
	}
	filtered := make([]store.QuotaObservation, 0, len(observations))
	for _, observation := range observations {
		generation, found := allowed[observation.ObservationID]
		if !found {
			continue
		}
		if generation != nil {
			observation.ResetsAtMS = *generation
		}
		filtered = append(filtered, observation)
	}
	return filtered
}

type paceEvidencePoint struct {
	atMS        int64
	usedPercent float64
}

func currentPacePoints(
	current store.QuotaCurrent,
	observations []store.QuotaObservation,
) []PacePoint {
	if current.SelectedSource == nil || current.EffectiveUsedPercent == nil ||
		current.WindowMinutes == nil || current.ResetsAtMS == nil ||
		*current.WindowMinutes <= 0 ||
		*current.EffectiveUsedPercent < 0 || *current.EffectiveUsedPercent > 100 {
		return []PacePoint{}
	}
	durationMS := *current.WindowMinutes * 60_000
	if durationMS <= 0 || *current.ResetsAtMS < durationMS {
		return []PacePoint{}
	}
	windowStartAtMS := *current.ResetsAtMS - durationMS
	if current.EvaluatedAtMS < windowStartAtMS || current.EvaluatedAtMS >= *current.ResetsAtMS {
		return []PacePoint{}
	}
	cycle, valid := buildPaceCycle(
		current, observations, *current.ResetsAtMS, *current.SelectedSource,
	)
	points := make([]PacePoint, 0, len(cycle.Points)+1)
	if valid {
		points = append(points, cycle.Points...)
	}
	currentPoint := PacePoint{
		ObservedAtMS:     current.EvaluatedAtMS,
		ElapsedPercent:   float64(current.EvaluatedAtMS-windowStartAtMS) / float64(durationMS) * 100,
		UsedPercent:      *current.EffectiveUsedPercent,
		RemainingPercent: 100 - *current.EffectiveUsedPercent,
	}
	if len(points) > 0 {
		last := points[len(points)-1]
		if last.ObservedAtMS > currentPoint.ObservedAtMS {
			return []PacePoint{}
		}
		if last.ObservedAtMS == currentPoint.ObservedAtMS {
			points[len(points)-1] = currentPoint
			return points
		}
	}
	points = append(points, currentPoint)
	return compactPacePoints(points)
}

func historicalPaceCycles(
	current store.QuotaCurrent,
	observations []store.QuotaObservation,
) (*PaceCycle, []PaceCycle) {
	if current.WindowMinutes == nil || current.ResetsAtMS == nil {
		return nil, []PaceCycle{}
	}
	eligible := make([]store.QuotaObservation, 0, len(observations))
	for _, observation := range observations {
		if !paceObservationMatchesWindow(current, observation) ||
			observation.ResetsAtMS >= *current.ResetsAtMS ||
			store.QuotaResetsEquivalentForWindow(
				observation.Source,
				observation.WindowMinutes,
				observation.ResetsAtMS,
				*current.ResetsAtMS,
			) {
			continue
		}
		eligible = append(eligible, observation)
	}
	sort.Slice(eligible, func(left, right int) bool {
		if eligible[left].ResetsAtMS != eligible[right].ResetsAtMS {
			return eligible[left].ResetsAtMS > eligible[right].ResetsAtMS
		}
		return eligible[left].LastObservedAtMS > eligible[right].LastObservedAtMS
	})
	type generationGroup struct {
		generation   int64
		observations []store.QuotaObservation
	}
	groups := make([]generationGroup, 0)
	for _, observation := range eligible {
		if len(groups) == 0 ||
			!store.QuotaResetsEquivalentForWindow(
				observation.Source,
				observation.WindowMinutes,
				groups[len(groups)-1].generation,
				observation.ResetsAtMS,
			) {
			groups = append(groups, generationGroup{generation: observation.ResetsAtMS})
		}
		last := len(groups) - 1
		groups[last].observations = append(groups[last].observations, observation)
	}
	var previous *PaceCycle
	complete := make([]PaceCycle, 0, 4)
	for _, group := range groups {
		source, found := selectPaceCycleSource(group.observations)
		if !found {
			continue
		}
		cycle, valid := buildPaceCycle(
			current,
			group.observations,
			group.generation,
			source,
		)
		if !valid {
			continue
		}
		if previous == nil {
			value := cycle
			previous = &value
		}
		if cycle.Complete && len(complete) < 4 {
			complete = append(complete, cycle)
		}
		if previous != nil && len(complete) == 4 {
			break
		}
	}
	return previous, complete
}

func paceObservationMatchesWindow(
	current store.QuotaCurrent,
	observation store.QuotaObservation,
) bool {
	return current.WindowMinutes != nil &&
		observation.AccountScope == current.AccountScope &&
		observation.WindowKind == current.WindowKind &&
		observation.LimitID != nil &&
		*observation.LimitID == current.LimitID &&
		observation.Validity == store.QuotaValidityAccepted &&
		observation.WindowMinutes == *current.WindowMinutes
}

func selectPaceCycleSource(
	observations []store.QuotaObservation,
) (store.QuotaSource, bool) {
	type sourceTerminal struct {
		source       store.QuotaSource
		observedAtMS int64
		usedPercent  float64
	}
	terminals := make(map[store.QuotaSource]sourceTerminal)
	for _, observation := range observations {
		terminal, found := terminals[observation.Source]
		if !found || observation.LastObservedAtMS > terminal.observedAtMS ||
			observation.LastObservedAtMS == terminal.observedAtMS &&
				observation.UsedPercent > terminal.usedPercent {
			terminals[observation.Source] = sourceTerminal{
				source: observation.Source, observedAtMS: observation.LastObservedAtMS,
				usedPercent: observation.UsedPercent,
			}
		}
	}
	var selected sourceTerminal
	found := false
	for _, terminal := range terminals {
		if !found || terminal.observedAtMS > selected.observedAtMS ||
			terminal.observedAtMS == selected.observedAtMS &&
				terminal.usedPercent > selected.usedPercent ||
			terminal.observedAtMS == selected.observedAtMS &&
				terminal.usedPercent == selected.usedPercent &&
				terminal.source < selected.source {
			selected = terminal
			found = true
		}
	}
	return selected.source, found
}

func buildPaceCycle(
	current store.QuotaCurrent,
	observations []store.QuotaObservation,
	generation int64,
	source store.QuotaSource,
) (PaceCycle, bool) {
	if current.WindowMinutes == nil || *current.WindowMinutes <= 0 {
		return PaceCycle{}, false
	}
	durationMS := *current.WindowMinutes * 60_000
	if durationMS <= 0 || generation < durationMS {
		return PaceCycle{}, false
	}
	windowStartAtMS := generation - durationMS
	pointsByTime := make(map[int64]float64)
	for _, observation := range observations {
		if !paceObservationMatchesWindow(current, observation) ||
			observation.Source != source ||
			!store.QuotaResetsEquivalentForWindow(
				source,
				observation.WindowMinutes,
				observation.ResetsAtMS,
				generation,
			) ||
			observation.UsedPercent < 0 || observation.UsedPercent > 100 ||
			observation.FirstObservedAtMS < windowStartAtMS ||
			observation.LastObservedAtMS < observation.FirstObservedAtMS ||
			observation.LastObservedAtMS >= generation {
			continue
		}
		for _, atMS := range []int64{observation.FirstObservedAtMS, observation.LastObservedAtMS} {
			if previous, found := pointsByTime[atMS]; !found || observation.UsedPercent > previous {
				pointsByTime[atMS] = observation.UsedPercent
			}
		}
	}
	times := make([]int64, 0, len(pointsByTime))
	for atMS := range pointsByTime {
		times = append(times, atMS)
	}
	sort.Slice(times, func(left, right int) bool { return times[left] < times[right] })
	points := make([]PacePoint, 0, len(times))
	for _, atMS := range times {
		usedPercent := pointsByTime[atMS]
		elapsedPercent := float64(atMS-windowStartAtMS) / float64(durationMS) * 100
		points = append(points, PacePoint{
			ObservedAtMS: atMS, ElapsedPercent: elapsedPercent,
			UsedPercent: usedPercent, RemainingPercent: 100 - usedPercent,
		})
	}
	if len(points) == 0 {
		return PaceCycle{}, false
	}
	points = compactPacePoints(points)
	complete := points[0].ElapsedPercent <= 10 &&
		points[len(points)-1].ElapsedPercent >= 90
	return PaceCycle{
		WindowGeneration: generation, WindowStartAtMS: windowStartAtMS,
		ResetsAtMS: generation, Complete: complete, Points: points,
	}, true
}

func compactPacePoints(points []PacePoint) []PacePoint {
	if len(points) <= 2 {
		return append([]PacePoint(nil), points...)
	}
	result := make([]PacePoint, 0, len(points))
	result = append(result, points[0])
	for index := 1; index < len(points)-1; index++ {
		previous := points[index-1]
		current := points[index]
		next := points[index+1]
		if current.UsedPercent != previous.UsedPercent ||
			current.UsedPercent != next.UsedPercent {
			result = append(result, current)
		}
	}
	result = append(result, points[len(points)-1])
	return result
}

func remainingAtElapsed(points []PacePoint, elapsedPercent float64) *float64 {
	var remaining *float64
	for _, point := range points {
		if point.ElapsedPercent > elapsedPercent {
			break
		}
		value := point.RemainingPercent
		remaining = &value
	}
	return remaining
}

func historyPaceBand(cycles []PaceCycle) []PaceHistoryBandPoint {
	result := make([]PaceHistoryBandPoint, 0, 21)
	for elapsed := 0; elapsed <= 100; elapsed += 5 {
		values := make([]float64, 0, len(cycles))
		for _, cycle := range cycles {
			if remaining := remainingAtElapsed(cycle.Points, float64(elapsed)); remaining != nil {
				values = append(values, *remaining)
			}
		}
		median := medianFloat64(values)
		if median == nil {
			continue
		}
		sort.Float64s(values)
		result = append(result, PaceHistoryBandPoint{
			ElapsedPercent: float64(elapsed), MedianRemaining: *median,
			MinimumRemaining: values[0], MaximumRemaining: values[len(values)-1],
			CycleCount: int64(len(values)),
		})
	}
	return result
}

func medianFloat64(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	middle := len(ordered) / 2
	value := ordered[middle]
	if len(ordered)%2 == 0 {
		value = (ordered[middle-1] + ordered[middle]) / 2
	}
	return &value
}

func forecastPaceWindow(
	current store.QuotaCurrent,
	observations []store.QuotaObservation,
	windowStartAtMS int64,
	durationMS int64,
	evaluatedAtMS int64,
) PaceForecast {
	forecast := PaceForecast{State: PaceForecastUnavailable, Method: PaceForecastMethodNone}
	if current.FreshnessState != store.QuotaCurrentFresh {
		reason := PaceUnknownEvidenceStale
		forecast.UnknownReason = &reason
		return forecast
	}
	if current.ConflictState != store.QuotaConflictNone {
		reason := PaceUnknownSourceConflict
		forecast.UnknownReason = &reason
		return forecast
	}
	if current.SelectedSource == nil || current.EffectiveUsedPercent == nil ||
		current.WindowMinutes == nil || current.ResetsAtMS == nil {
		reason := PaceUnknownWindowUnavailable
		forecast.UnknownReason = &reason
		return forecast
	}
	if *current.EffectiveUsedPercent >= 100 {
		forecast.State = PaceForecastExhausted
		return forecast
	}
	lookbackMS := durationMS / 4
	const (
		minimumLookbackMS = int64(30 * 60 * 1_000)
		maximumLookbackMS = int64(24 * 60 * 60 * 1_000)
	)
	if lookbackMS < minimumLookbackMS {
		lookbackMS = minimumLookbackMS
	}
	if lookbackMS > maximumLookbackMS {
		lookbackMS = maximumLookbackMS
	}
	cutoffMS := evaluatedAtMS - lookbackMS
	if cutoffMS < windowStartAtMS {
		cutoffMS = windowStartAtMS
	}
	points, valid := recentPaceEvidence(current, observations, cutoffMS, evaluatedAtMS)
	if !valid {
		reason := PaceUnknownEvidenceInvalid
		forecast.UnknownReason = &reason
		return forecast
	}
	if len(points) < 3 {
		reason := PaceUnknownEvidenceSparse
		forecast.UnknownReason = &reason
		return forecast
	}
	spanMS := points[len(points)-1].atMS - points[0].atMS
	forecast.EvidenceCount = int64(len(points))
	forecast.EvidenceSpanMS = spanMS
	if spanMS < minimumLookbackMS {
		reason := PaceUnknownEvidenceSparse
		forecast.UnknownReason = &reason
		return forecast
	}
	if points[len(points)-1].usedPercent-points[0].usedPercent < 1 {
		reason := PaceUnknownEvidenceFlat
		forecast.UnknownReason = &reason
		return forecast
	}
	slope, valid := theilSenSlope(points)
	if !valid {
		reason := PaceUnknownEvidenceInvalid
		forecast.UnknownReason = &reason
		return forecast
	}
	forecast.Method = PaceForecastMethodRecentTheilSen
	forecast.UnknownReason = nil
	if slope <= 0 {
		forecast.State = PaceForecastOnTrack
		return forecast
	}
	latestEvidence := points[len(points)-1]
	if math.Abs(latestEvidence.usedPercent-*current.EffectiveUsedPercent) > 1e-9 {
		forecast.State = PaceForecastUnavailable
		forecast.Method = PaceForecastMethodNone
		reason := PaceUnknownEvidenceInvalid
		forecast.UnknownReason = &reason
		return forecast
	}
	remainingPercent := 100 - latestEvidence.usedPercent
	untilExhausted := remainingPercent / slope
	if math.IsNaN(untilExhausted) || math.IsInf(untilExhausted, 0) ||
		untilExhausted < 0 ||
		untilExhausted > float64(runtimeclock.MaxTimestampMS-latestEvidence.atMS) {
		forecast.State = PaceForecastUnavailable
		forecast.Method = PaceForecastMethodNone
		reason := PaceUnknownEvidenceInvalid
		forecast.UnknownReason = &reason
		return forecast
	}
	exhaustAtMS := latestEvidence.atMS + int64(math.Round(untilExhausted))
	if exhaustAtMS >= *current.ResetsAtMS {
		forecast.State = PaceForecastOnTrack
		return forecast
	}
	leadBeforeResetMS := *current.ResetsAtMS - exhaustAtMS
	forecast.State = PaceForecastAtRisk
	forecast.ExhaustAtMS = &exhaustAtMS
	forecast.LeadBeforeResetMS = &leadBeforeResetMS
	return forecast
}

func recentPaceEvidence(
	current store.QuotaCurrent,
	observations []store.QuotaObservation,
	cutoffMS int64,
	evaluatedAtMS int64,
) ([]paceEvidencePoint, bool) {
	pointsByTime := make(map[int64]float64)
	for _, observation := range observations {
		if !paceObservationMatchesWindow(current, observation) ||
			observation.Source != *current.SelectedSource ||
			!store.QuotaResetsEquivalentForWindow(
				observation.Source,
				observation.WindowMinutes,
				observation.ResetsAtMS,
				*current.ResetsAtMS,
			) ||
			observation.UsedPercent < 0 || observation.UsedPercent > 100 ||
			observation.FirstObservedAtMS < 0 ||
			observation.LastObservedAtMS < observation.FirstObservedAtMS {
			continue
		}
		for _, atMS := range []int64{observation.FirstObservedAtMS, observation.LastObservedAtMS} {
			if atMS < cutoffMS || atMS > evaluatedAtMS {
				continue
			}
			if previous, found := pointsByTime[atMS]; !found || observation.UsedPercent > previous {
				pointsByTime[atMS] = observation.UsedPercent
			}
		}
	}
	points := make([]paceEvidencePoint, 0, len(pointsByTime))
	for atMS, usedPercent := range pointsByTime {
		points = append(points, paceEvidencePoint{atMS: atMS, usedPercent: usedPercent})
	}
	sort.Slice(points, func(left, right int) bool { return points[left].atMS < points[right].atMS })
	return points, true
}

func theilSenSlope(points []paceEvidencePoint) (float64, bool) {
	slopes := make([]float64, 0, len(points)*(len(points)-1)/2)
	for left := range points {
		for right := left + 1; right < len(points); right++ {
			elapsedMS := points[right].atMS - points[left].atMS
			if elapsedMS <= 0 {
				continue
			}
			slope := (points[right].usedPercent - points[left].usedPercent) / float64(elapsedMS)
			if math.IsNaN(slope) || math.IsInf(slope, 0) {
				return 0, false
			}
			slopes = append(slopes, slope)
		}
	}
	if len(slopes) == 0 {
		return 0, false
	}
	sort.Float64s(slopes)
	middle := len(slopes) / 2
	if len(slopes)%2 == 1 {
		return slopes[middle], true
	}
	return (slopes[middle-1] + slopes[middle]) / 2, true
}
