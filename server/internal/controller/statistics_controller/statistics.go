package statistics_controller

import (
	"time"

	"github.com/labstack/echo/v5"

	apphttp "github.com/SisyphusSQ/codex-pulse/server/internal/http"
	statistics_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/statistics_dto"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/vo"
	statistics_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/statistics_srv"
)

type Statistics struct{ service *statistics_srv.Statistics }

func NewStatistics(service *statistics_srv.Statistics) *Statistics {
	return &Statistics{service: service}
}
func (s *Statistics) Register(e *echo.Echo) {
	e.GET("/api/v1/statistics/summary", s.Summary)
	e.GET("/api/v1/statistics/annual", s.Annual)
	e.GET("/api/v1/statistics/totals", s.Totals)
	e.GET("/api/v1/statistics/activity", s.Activity)
	e.GET("/api/v1/statistics/top-sessions", s.Top)
	e.GET("/api/v1/statistics/providers", s.Providers)
	e.GET("/api/v1/statistics/models", s.Models)
	e.GET("/api/v1/statistics/source-usage", s.SourceUsage)
	e.GET("/api/v1/statistics/usage", s.Usage)
	e.GET("/api/v1/sessions", s.Sessions)
	e.GET("/api/v1/sessions/:id", s.Session)
	e.GET("/api/v1/projects", s.Projects)
	e.GET("/api/v1/projects/:id", s.Project)
	e.GET("/api/v1/devices/status", s.Devices)
}
func statisticsQuery(c *echo.Context) (statistics_dto.StatisticsQuery, error) {
	return statistics_srv.ParseStatisticsQuery(c.Request().URL.Query(), time.Now())
}
func (s *Statistics) Summary(c *echo.Context) error {
	q, err := statisticsQuery(c)
	if err != nil {
		return err
	}
	out, err := s.service.Summary(c.Request().Context(), apphttp.Principal(c), q)
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, out)
}
func (s *Statistics) Sessions(c *echo.Context) error {
	q, err := statisticsQuery(c)
	if err != nil {
		return err
	}
	out, err := s.service.Sessions(c.Request().Context(), apphttp.Principal(c), q)
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, out)
}
func (s *Statistics) Session(c *echo.Context) error {
	q, err := statisticsQuery(c)
	if err != nil {
		return err
	}
	out, err := s.service.Session(c.Request().Context(), apphttp.Principal(c), q, c.Param("id"))
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, out)
}
func (s *Statistics) Projects(c *echo.Context) error {
	q, err := statisticsQuery(c)
	if err != nil {
		return err
	}
	out, err := s.service.Projects(c.Request().Context(), apphttp.Principal(c), q)
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, out)
}
func (s *Statistics) Project(c *echo.Context) error {
	q, err := statisticsQuery(c)
	if err != nil {
		return err
	}
	out, err := s.service.Project(c.Request().Context(), apphttp.Principal(c), q, c.Param("id"))
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, out)
}
func (s *Statistics) Devices(c *echo.Context) error {
	out, err := s.service.Devices(c.Request().Context(), apphttp.Principal(c))
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, out)
}

func (s *Statistics) Usage(c *echo.Context) error {
	q, err := statisticsQuery(c)
	if err != nil {
		return err
	}
	out, err := s.service.Usage(c.Request().Context(), apphttp.Principal(c), q)
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, out)
}

func (s *Statistics) SourceUsage(c *echo.Context) error {
	q, err := statisticsQuery(c)
	if err != nil {
		return err
	}
	out, err := s.service.SourceUsage(c.Request().Context(), apphttp.Principal(c), q)
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, out)
}
