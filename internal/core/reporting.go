package core

import (
	"context"
	"errors"

	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/reporting"
)

type reportingControl interface {
	Status(context.Context) (reporting.Status, error)
	Pair(context.Context, reporting.PairRequest) (reporting.Status, error)
	Configure(context.Context, reporting.ConfigureRequest) (reporting.Status, error)
	SyncNow(context.Context) (reporting.Status, error)
}

func (s *Service) bindReporting(control reportingControl) error {
	if s == nil || control == nil {
		return ErrService
	}
	s.reportingMu.Lock()
	defer s.reportingMu.Unlock()
	if s.reporting != nil {
		return ErrService
	}
	s.reporting = control
	return nil
}
func (s *Service) reportingControl() (reportingControl, error) {
	if s == nil {
		return nil, newServiceFailure(ErrService)
	}
	s.reportingMu.RLock()
	defer s.reportingMu.RUnlock()
	if s.reporting == nil {
		return nil, newServiceFailure(ErrService)
	}
	return s.reporting, nil
}
func reportingFailure(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, reporting.ErrSettings):
		return newServiceFailure(basequery.NewValidationFailure("settings", err))
	case errors.Is(err, reporting.ErrPending):
		return newServiceFailure(basequery.NewPartialFailure(err))
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return newServiceFailure(err)
	default:
		return newServiceFailure(basequery.NewUnavailableFailure(err))
	}
}
func (s *Service) ReportingStatus(ctx context.Context) (reporting.Status, error) {
	c, err := s.reportingControl()
	if err != nil {
		return reporting.Status{}, err
	}
	out, err := c.Status(ctx)
	return out, reportingFailure(err)
}
func (s *Service) PairReporting(ctx context.Context, request reporting.PairRequest) (reporting.Status, error) {
	c, err := s.reportingControl()
	if err != nil {
		return reporting.Status{}, err
	}
	out, err := c.Pair(ctx, request)
	return out, reportingFailure(err)
}
func (s *Service) ConfigureReporting(ctx context.Context, request reporting.ConfigureRequest) (reporting.Status, error) {
	c, err := s.reportingControl()
	if err != nil {
		return reporting.Status{}, err
	}
	out, err := c.Configure(ctx, request)
	return out, reportingFailure(err)
}
func (s *Service) SyncReportingNow(ctx context.Context) (reporting.Status, error) {
	c, err := s.reportingControl()
	if err != nil {
		return reporting.Status{}, err
	}
	out, err := c.SyncNow(ctx)
	return out, reportingFailure(err)
}
