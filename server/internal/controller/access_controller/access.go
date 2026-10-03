package access_controller

import (
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/server/config"
	apphttp "github.com/SisyphusSQ/codex-pulse/server/internal/http"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/vo"
	access_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/access_vo"
	access_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

type Access struct {
	service *access_srv.Access
	config  config.Config
}

func NewAccess(service *access_srv.Access, config config.Config) *Access {
	return &Access{service: service, config: config}
}

func (a *Access) Register(e *echo.Echo) {
	e.POST("/api/v1/pair", a.Pair)
	e.GET("/api/v1/session", a.Session)
	e.POST("/api/v1/logout", a.Logout)
	e.POST("/api/v1/pairings", a.Issue)
	e.POST("/api/v1/pairings/revoke", a.RevokePairing)
	e.GET("/api/v1/clients", a.Clients)
	e.POST("/api/v1/clients/:id/rename", a.Rename)
	e.POST("/api/v1/clients/:id/revoke", a.Revoke)
}

func (a *Access) Pair(c *echo.Context) error {
	var request access_vo.PairRequest
	if err := apphttp.DecodeJSON(c, &request, 4096); err != nil {
		return err
	}
	origin, err := apphttp.RequestOrigin(c.Request(), a.config)
	if err != nil {
		return err
	}
	if request.Mode == "browser" && c.Request().Header.Get("Origin") != origin {
		return utils.ErrForbidden
	}
	paired, err := a.service.Pair(c.Request().Context(), request.Code, request.Mode, origin)
	if err != nil {
		return err
	}
	if request.Mode == "collector" {
		return vo.CommSuccResp(c, access_vo.CollectorPairView{ClientID: paired.Principal.ID, Credential: paired.Credential, ProtocolVersion: reportingv1.Version})
	}
	c.SetCookie(&http.Cookie{Name: apphttp.SessionCookieName(origin), Value: paired.Credential, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(origin, "https://"), SameSite: http.SameSiteStrictMode, Expires: time.UnixMilli(*paired.ExpiresAtMS)})
	return vo.CommSuccResp(c, access_vo.SessionView{ClientID: paired.Principal.ID, Name: paired.Principal.Name, Purpose: paired.Principal.Purpose, CSRF: paired.CSRF, ExpiresAtMS: paired.ExpiresAtMS})
}

func (a *Access) Session(c *echo.Context) error {
	principal := apphttp.Principal(c)
	if err := access_srv.RequireAdmin(principal); err != nil {
		return err
	}
	cookie, err := c.Request().Cookie(apphttp.SessionCookieName(principal.Origin))
	if err != nil {
		return utils.ErrUnauthorized
	}
	return vo.CommSuccResp(c, access_vo.SessionView{ClientID: principal.ID, Name: principal.Name, Purpose: principal.Purpose, CSRF: access_srv.CSRFToken(cookie.Value, principal.Origin), ExpiresAtMS: principal.ExpiresAtMS})
}

func (a *Access) Logout(c *echo.Context) error {
	principal := apphttp.Principal(c)
	if err := a.service.Revoke(c.Request().Context(), principal, principal.ID); err != nil {
		return err
	}
	c.SetCookie(&http.Cookie{Name: apphttp.SessionCookieName(principal.Origin), Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: strings.HasPrefix(principal.Origin, "https://"), SameSite: http.SameSiteStrictMode})
	return vo.CommSuccResp(c, vo.MutationView{Applied: true})
}

func (a *Access) Issue(c *echo.Context) error {
	var request access_vo.CreatePairingRequest
	if err := apphttp.DecodeJSON(c, &request, 4096); err != nil {
		return err
	}
	code, err := a.service.Issue(c.Request().Context(), apphttp.Principal(c), request.Purpose, request.Name)
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, code)
}
func (a *Access) Clients(c *echo.Context) error {
	clients, err := a.service.Clients(c.Request().Context(), apphttp.Principal(c))
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, clients)
}
func (a *Access) Revoke(c *echo.Context) error {
	if err := a.service.Revoke(c.Request().Context(), apphttp.Principal(c), c.Param("id")); err != nil {
		return err
	}
	return vo.CommSuccResp(c, vo.MutationView{Applied: true})
}

func (a *Access) RevokePairing(c *echo.Context) error {
	var request access_vo.RevokePairingRequest
	if err := apphttp.DecodeJSON(c, &request, 4096); err != nil {
		return err
	}
	if err := a.service.RevokePairing(c.Request().Context(), apphttp.Principal(c), request.Code); err != nil {
		return err
	}
	return vo.CommSuccResp(c, vo.MutationView{Applied: true})
}

func (a *Access) Rename(c *echo.Context) error {
	var request access_vo.RenameClientRequest
	if err := apphttp.DecodeJSON(c, &request, 4096); err != nil {
		return err
	}
	if err := a.service.Rename(c.Request().Context(), apphttp.Principal(c), c.Param("id"), request.Name); err != nil {
		return err
	}
	return vo.CommSuccResp(c, vo.MutationView{Applied: true})
}
