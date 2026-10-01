package reporting

import (
	"context"
	"errors"
	"strconv"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type Preferences interface {
	LoadPreferences(context.Context) (preferences.Snapshot, error)
}
type Exporter struct {
	repository  *store.Repository
	preferences Preferences
	state       *State
	identities  AccountIdentitySource
}

func NewExporter(repository *store.Repository, prefs Preferences, state *State, identities ...AccountIdentitySource) *Exporter {
	e := &Exporter{repository: repository, preferences: prefs, state: state}
	if len(identities) > 0 {
		e.identities = identities[0]
	}
	return e
}
func (e *Exporter) source(ctx context.Context, provider string, start int64) (store.ReportingSource, error) {
	prefs, err := e.preferences.LoadPreferences(ctx)
	if err != nil {
		return store.ReportingSource{}, store.ErrReportingSource
	}
	if prefs.PendingSwitch != nil || prefs.PendingResume != nil {
		return store.ReportingSource{}, store.ErrReportingSource
	}
	partition, err := e.repository.ReportingPartition(ctx, provider)
	if err != nil {
		return store.ReportingSource{}, err
	}
	s := store.ReportingSource{StartAtMS: start, Partition: partition, HomeID: e.state.HomeID("provider", provider, partition)}
	var intent preferences.ProviderIntent
	switch provider {
	case "codex":
		intent = prefs.Providers.Codex.Intent
		if prefs.CodexHome == nil {
			return s, store.ErrReportingSource
		}
		h := prefs.CodexHome.Source
		s.Path = h.Path
		s.DeviceID = h.DeviceID
		s.Inode = h.Inode
		s.HomeID = e.state.HomeID("codex-home", h.Path, h.DeviceID, strconv.FormatInt(h.Inode, 10))
	case "cursor":
		intent = prefs.Providers.Cursor.Intent
	case "grok":
		intent = prefs.Providers.Grok.Intent
	default:
		return s, store.ErrReportingSource
	}
	if intent == preferences.ProviderIntentDisabled {
		return s, store.ErrReportingSource
	}
	return s, nil
}
func (e *Exporter) Partition(ctx context.Context, provider string) (string, error) {
	s, err := e.source(ctx, provider, 0)
	return s.HomeID, err
}
func (e *Exporter) Page(ctx context.Context, provider, after string, start int64) (ExportPage, error) {
	before, err := e.source(ctx, provider, start)
	if err != nil {
		return ExportPage{}, err
	}
	page, err := e.repository.ReportingPage(ctx, provider, before, after)
	if err != nil {
		return ExportPage{}, err
	}
	later, err := e.source(ctx, provider, start)
	if err != nil || later.HomeID != before.HomeID {
		return ExportPage{}, store.ErrReportingSource
	}
	return ExportPage{Sessions: page.Sessions, Next: page.Next, Authority: page.Authority}, nil
}

func (e *Exporter) Status(ctx context.Context, provider string, start int64) (reportingv1.DeviceStatus, error) {
	source, err := e.source(ctx, provider, start)
	if errors.Is(err, store.ErrReportingSource) {
		status := "source_unavailable"
		if prefs, loadErr := e.preferences.LoadPreferences(ctx); loadErr == nil {
			switch provider {
			case "codex":
				if prefs.Providers.Codex.Intent == preferences.ProviderIntentDisabled {
					status = "disabled"
				}
			case "cursor":
				if prefs.Providers.Cursor.Intent == preferences.ProviderIntentDisabled {
					status = "disabled"
				}
			case "grok":
				if prefs.Providers.Grok.Intent == preferences.ProviderIntentDisabled {
					status = "disabled"
				}
			}
		}
		return reportingv1.DeviceStatus{Provider: provider, Status: status}, nil
	}
	if err != nil {
		return reportingv1.DeviceStatus{}, err
	}
	status, err := e.repository.ReportingStatus(ctx, provider, source)
	if err != nil {
		return status, err
	}
	later, err := e.source(ctx, provider, start)
	if err != nil || source.HomeID != later.HomeID {
		return reportingv1.DeviceStatus{Provider: provider, Status: "source_unavailable"}, nil
	}
	return status, nil
}
