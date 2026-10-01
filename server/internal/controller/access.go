package controller

import (
	"encoding/json/v2"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/server/config"
	apphttp "github.com/SisyphusSQ/codex-pulse/server/internal/http"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/vo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

type Access struct {
	service *service.Access
	config  config.Config
}

func NewAccess(service *service.Access, config config.Config) *Access {
	return &Access{service: service, config: config}
}

// DecodeJSON 拒绝未知、重复字段及超预算正文，不回显原始解析错误。
func DecodeJSON(c *echo.Context, target any, maximum int64) error {
	contentType, _, err := mime.ParseMediaType(c.Request().Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return echo.ErrUnsupportedMediaType
	}
	bytes, err := io.ReadAll(io.LimitReader(c.Request().Body, maximum+1))
	if err != nil {
		return utils.ErrBadParamInput
	}
	if int64(len(bytes)) > maximum {
		return echo.NewHTTPError(http.StatusRequestEntityTooLarge, http.StatusText(http.StatusRequestEntityTooLarge))
	}
	if err := json.Unmarshal(bytes, target, json.RejectUnknownMembers(true)); err != nil {
		return utils.ErrBadParamInput
	}
	return nil
}

func (a *Access) Register(e *echo.Echo) {
	e.POST("/api/v1/pair", a.Pair)
	e.GET("/api/v1/session", a.Session)
	e.POST("/api/v1/logout", a.Logout)
	e.POST("/api/v1/pairings", a.Issue)
	e.GET("/api/v1/clients", a.Clients)
	e.POST("/api/v1/clients/:id/revoke", a.Revoke)
}

func (a *Access) Pair(c *echo.Context) error {
	var request vo.PairRequest
	if err := DecodeJSON(c, &request, 4096); err != nil {
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
		return vo.CommSuccResp(c, vo.CollectorPairView{ClientID: paired.Principal.ID, Credential: paired.Credential, ProtocolVersion: reportingv1.Version})
	}
	c.SetCookie(&http.Cookie{Name: apphttp.SessionCookieName(origin), Value: paired.Credential, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(origin, "https://"), SameSite: http.SameSiteStrictMode, Expires: time.UnixMilli(*paired.ExpiresAtMS)})
	return vo.CommSuccResp(c, vo.SessionView{ClientID: paired.Principal.ID, Name: paired.Principal.Name, Purpose: paired.Principal.Purpose, CSRF: paired.CSRF, ExpiresAtMS: paired.ExpiresAtMS})
}

func (a *Access) Session(c *echo.Context) error {
	principal := apphttp.Principal(c)
	if err := service.RequireAdmin(principal); err != nil {
		return err
	}
	cookie, err := c.Request().Cookie(apphttp.SessionCookieName(principal.Origin))
	if err != nil {
		return utils.ErrUnauthorized
	}
	return vo.CommSuccResp(c, vo.SessionView{ClientID: principal.ID, Name: principal.Name, Purpose: principal.Purpose, CSRF: service.CSRFToken(cookie.Value, principal.Origin), ExpiresAtMS: principal.ExpiresAtMS})
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
	var request vo.CreatePairingRequest
	if err := DecodeJSON(c, &request, 4096); err != nil {
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
