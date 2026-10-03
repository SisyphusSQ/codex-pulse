package reporting_controller

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	apphttp "github.com/SisyphusSQ/codex-pulse/server/internal/http"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/vo"
	reporting_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/reporting_vo"
	reporting_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/reporting_srv"
)

type Reporting struct{ service *reporting_srv.Reporting }

func NewReporting(service *reporting_srv.Reporting) *Reporting { return &Reporting{service: service} }
func (r *Reporting) Register(e *echo.Echo) {
	e.POST("/api/v1/batches", r.Accept)
	e.GET("/api/v1/sync", r.Sync)
	e.POST("/api/v1/projects/associate", r.AssociateProjects)
}
func (r *Reporting) Accept(c *echo.Context) error {
	var request reporting_vo.ReportingBatchRequest
	if err := apphttp.DecodeJSON(c, &request, reportingv1.MaxBodyBytes); err != nil {
		return err
	}
	receipt, err := r.service.Accept(c.Request().Context(), apphttp.Principal(c), reportingv1.Batch(request))
	if errors.Is(err, reporting_srv.ErrReportingVersion) {
		return echo.NewHTTPError(http.StatusUpgradeRequired, http.StatusText(http.StatusUpgradeRequired))
	}
	if errors.Is(err, reporting_srv.ErrReportingBudget) {
		return echo.NewHTTPError(http.StatusRequestEntityTooLarge, http.StatusText(http.StatusRequestEntityTooLarge))
	}
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, receipt)
}
func (r *Reporting) Sync(c *echo.Context) error {
	out, err := r.service.Sync(c.Request().Context(), apphttp.Principal(c))
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, out)
}

func (r *Reporting) AssociateProjects(c *echo.Context) error {
	var request reporting_vo.ProjectAssociationRequest
	if err := apphttp.DecodeJSON(c, &request, 16<<10); err != nil {
		return err
	}
	out, err := r.service.AssociateProjects(c.Request().Context(), apphttp.Principal(c), request)
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, out)
}
