package tray

import (
	"fmt"
	"math"
	"strings"
	"sync"
)

const (
	StatusItemWidth  = 252
	StatusItemHeight = 54
)

type WindowKind string

const (
	WindowPrimary   WindowKind = "primary"
	WindowSecondary WindowKind = "secondary"
)

type Freshness string

const (
	FreshnessNeverLoaded    Freshness = "never_loaded"
	FreshnessFresh          Freshness = "fresh"
	FreshnessStale          Freshness = "stale"
	FreshnessExpiredUnknown Freshness = "expired_unknown"
	FreshnessSuspicious     Freshness = "suspicious"
)

type HealthState string

const (
	HealthNone     HealthState = "none"
	HealthDegraded HealthState = "degraded"
	HealthBlocked  HealthState = "blocked"
)

type DisplayState string

const (
	DisplayTrusted     DisplayState = "trusted"
	DisplayStale       DisplayState = "stale"
	DisplayConflict    DisplayState = "conflict"
	DisplayUnavailable DisplayState = "unavailable"
	DisplayExhausted   DisplayState = "exhausted"
)

type WindowSnapshot struct {
	Kind             WindowKind
	RemainingPercent *float64
	ResetRemainingMS *int64
	Freshness        Freshness
	Conflict         bool
}

type Snapshot struct {
	Windows   []WindowSnapshot
	Health    HealthState
	ReadError error
}

type StatusRow struct {
	Kind             WindowKind `json:"kind"`
	Label            string     `json:"label"`
	Value            string     `json:"value"`
	Known            bool       `json:"known"`
	Progress         float64    `json:"progress"`
	ResetDescription string     `json:"resetDescription,omitempty"`
}

type StatusViewModel struct {
	Width              int          `json:"width"`
	Height             int          `json:"height"`
	State              DisplayState `json:"state"`
	Health             HealthState  `json:"health"`
	HealthMarker       string       `json:"healthMarker"`
	Rows               []StatusRow  `json:"rows"`
	AccessibilityLabel string       `json:"accessibilityLabel"`
}

type Projector struct {
	mu          sync.Mutex
	lastTrusted map[WindowKind]WindowSnapshot
}

func NewProjector() *Projector {
	return &Projector{lastTrusted: make(map[WindowKind]WindowSnapshot, 2)}
}

func (projector *Projector) Project(snapshot Snapshot) StatusViewModel {
	if projector == nil {
		projector = NewProjector()
	}
	projector.mu.Lock()
	defer projector.mu.Unlock()

	health := normalizeHealth(snapshot.Health)
	if snapshot.ReadError != nil {
		if len(projector.lastTrusted) > 0 {
			return buildModel(projector.lastTrusted, DisplayStale, health)
		}
		return buildModel(nil, DisplayUnavailable, health)
	}
	windows, valid := normalizeWindows(snapshot.Windows)
	if !valid {
		return buildModel(nil, DisplayUnavailable, health)
	}
	state := aggregateDisplayState(windows)
	// A successful response is authoritative about which quota capabilities
	// currently exist. Remove an absent window so a later transport failure
	// cannot resurrect a retired quota row from an older plan.
	for _, kind := range []WindowKind{WindowPrimary, WindowSecondary} {
		if _, exists := windows[kind]; !exists {
			delete(projector.lastTrusted, kind)
		}
	}
	for kind, window := range windows {
		if window.RemainingPercent != nil && window.Freshness == FreshnessFresh && !window.Conflict {
			projector.lastTrusted[kind] = cloneWindow(window)
		}
	}
	return buildModel(windows, state, health)
}

func normalizeWindows(input []WindowSnapshot) (map[WindowKind]WindowSnapshot, bool) {
	if len(input) < 1 || len(input) > 2 {
		return nil, false
	}
	windows := make(map[WindowKind]WindowSnapshot, 2)
	for _, window := range input {
		if window.Kind != WindowPrimary && window.Kind != WindowSecondary {
			return nil, false
		}
		if _, exists := windows[window.Kind]; exists {
			return nil, false
		}
		if !validFreshness(window.Freshness) || !validPercent(window.RemainingPercent) {
			return nil, false
		}
		if window.ResetRemainingMS != nil && *window.ResetRemainingMS < 0 {
			return nil, false
		}
		windows[window.Kind] = cloneWindow(window)
	}
	return windows, true
}

func validFreshness(value Freshness) bool {
	switch value {
	case FreshnessNeverLoaded, FreshnessFresh, FreshnessStale, FreshnessExpiredUnknown, FreshnessSuspicious:
		return true
	default:
		return false
	}
}

func validPercent(value *float64) bool {
	return value == nil || (!math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 0 && *value <= 100)
}

func aggregateDisplayState(windows map[WindowKind]WindowSnapshot) DisplayState {
	state := DisplayTrusted
	exhausted := false
	for _, kind := range []WindowKind{WindowPrimary, WindowSecondary} {
		window, exists := windows[kind]
		if !exists {
			continue
		}
		if window.RemainingPercent == nil || window.Freshness == FreshnessNeverLoaded ||
			window.Freshness == FreshnessExpiredUnknown || window.Freshness == FreshnessSuspicious {
			return DisplayUnavailable
		}
		if window.Conflict {
			state = DisplayConflict
			continue
		}
		if window.Freshness == FreshnessStale && state != DisplayConflict {
			state = DisplayStale
		}
		if *window.RemainingPercent == 0 {
			exhausted = true
		}
	}
	if state == DisplayTrusted && exhausted {
		return DisplayExhausted
	}
	return state
}

func buildModel(windows map[WindowKind]WindowSnapshot, state DisplayState, health HealthState) StatusViewModel {
	model := StatusViewModel{
		Width: StatusItemWidth, Height: StatusItemHeight, State: state, Health: health,
		Rows: make([]StatusRow, 0, len(windows)),
	}
	for _, definition := range []struct {
		kind  WindowKind
		label string
	}{{WindowPrimary, "5 小时"}, {WindowSecondary, "本周"}} {
		if _, exists := windows[definition.kind]; exists {
			model.Rows = append(model.Rows, buildRow(definition.kind, definition.label, windows))
		}
	}
	switch health {
	case HealthBlocked:
		model.HealthMarker = "!"
	case HealthDegraded:
		model.HealthMarker = "△"
	}
	model.AccessibilityLabel = accessibilityLabel(model)
	return model
}

func buildRow(kind WindowKind, label string, windows map[WindowKind]WindowSnapshot) StatusRow {
	row := StatusRow{Kind: kind, Label: label, Value: "--"}
	window, ok := windows[kind]
	if !ok || window.RemainingPercent == nil {
		return row
	}
	row.Known = true
	row.Progress = *window.RemainingPercent / 100
	row.Value = fmt.Sprintf("%.0f%%", *window.RemainingPercent)
	row.ResetDescription = formatResetRemaining(window.ResetRemainingMS)
	return row
}

func accessibilityLabel(model StatusViewModel) string {
	parts := make([]string, 0, 4)
	for _, row := range model.Rows {
		if row.Known {
			description := fmt.Sprintf("%s剩余 %s", row.Label, row.Value)
			if row.ResetDescription != "" {
				description += "，" + row.ResetDescription
			}
			parts = append(parts, description)
		} else {
			parts = append(parts, fmt.Sprintf("%s额度未知", row.Label))
		}
	}
	parts = append(parts, map[DisplayState]string{
		DisplayTrusted: "数据可信", DisplayStale: "数据陈旧", DisplayConflict: "数据冲突",
		DisplayUnavailable: "数据不可用", DisplayExhausted: "额度已用尽",
	}[model.State])
	switch model.Health {
	case HealthBlocked:
		parts = append(parts, "健康受阻")
	case HealthDegraded:
		parts = append(parts, "健康降级")
	}
	return strings.Join(parts, "，")
}

func formatResetRemaining(value *int64) string {
	if value == nil {
		return ""
	}
	if *value < 60_000 {
		return "不足 1 分钟后重置"
	}
	minutes := (*value + 59_999) / 60_000
	if minutes < 60 {
		return fmt.Sprintf("%d 分钟后重置", minutes)
	}
	hours := minutes / 60
	remainingMinutes := minutes % 60
	if remainingMinutes == 0 {
		return fmt.Sprintf("%d 小时后重置", hours)
	}
	return fmt.Sprintf("%d 小时 %d 分钟后重置", hours, remainingMinutes)
}

func normalizeHealth(value HealthState) HealthState {
	if value == HealthDegraded || value == HealthBlocked {
		return value
	}
	return HealthNone
}

func cloneWindow(value WindowSnapshot) WindowSnapshot {
	value.RemainingPercent = cloneFloat64Pointer(value.RemainingPercent)
	value.ResetRemainingMS = cloneInt64Pointer(value.ResetRemainingMS)
	return value
}

func cloneFloat64Pointer(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
