package http

import (
	"crypto/subtle"
	"net/http"
	"regexp"
	"time"
	"uuid"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/SisyphusSQ/codex-pulse/server/internal/lib/log"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/vo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/requestinfo"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

type EchoMiddleware struct{ config config.Config }

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func InitMiddleware(cfg config.Config) *EchoMiddleware { return &EchoMiddleware{config: cfg} }
func (e *EchoMiddleware) CORS(next echo.HandlerFunc) echo.HandlerFunc {
	return middleware.CORSWithConfig(middleware.CORSConfig{AllowOrigins: e.config.Server.CORSOrigins, AllowHeaders: []string{"Content-Type", "Authorization", "access_key", "secret_key", "X-Request-ID"}, ExposeHeaders: []string{"X-Request-ID"}})(next)
}
func (e *EchoMiddleware) Recover(next echo.HandlerFunc) echo.HandlerFunc {
	return middleware.Recover()(next)
}
func (e *EchoMiddleware) Logger(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		id := c.Request().Header.Get("X-Request-ID")
		if !validRequestID.MatchString(id) {
			id = uuid.New().String()
		}
		c.SetRequest(c.Request().WithContext(requestinfo.WithID(c.Request().Context(), id)))
		c.Response().Header().Set("X-Request-ID", id)
		start := time.Now()
		err := next(c)
		if err != nil {
			e.ErrorHandler(c, err)
		}
		response, _ := echo.UnwrapResponse(c.Response())
		status := http.StatusOK
		if response != nil {
			status = response.Status
		}
		log.FromContext(c.Request().Context()).Infow("request completed", "method", c.Request().Method, "route", c.Path(), "status", status, "duration", time.Since(start))
		return nil
	}
}
func (e *EchoMiddleware) Auth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if e.isPublicURI(c.Request().URL.Path) {
			return next(c)
		}
		switch e.config.Key.Type {
		case "none":
			return next(c) // Config.Validate 限制为 loopback debug。
		case "basic":
			user, password, ok := c.Request().BasicAuth()
			if ok && equal(user, e.config.Key.Basic.User) && equal(password, e.config.Key.Basic.Password) {
				return next(c)
			}
		case "key":
			if equal(c.Request().Header.Get("access_key"), e.config.Key.AK.AccessKey) && equal(c.Request().Header.Get("secret_key"), e.config.Key.AK.SecretKey) {
				return next(c)
			}

		}
		return utils.ErrUnauthorized
	}
}
func equal(a, b string) bool { return b != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func (e *EchoMiddleware) isPublicURI(path string) bool {
	return path == "/health" || path == "/ready"
}

func (e *EchoMiddleware) ErrorHandler(c *echo.Context, err error) {
	response, _ := echo.UnwrapResponse(c.Response())
	if response != nil && response.Committed {
		return
	}
	status := utils.GetStatusCode(err)
	if code := echo.StatusCode(err); code != 0 {
		status = code
	}
	if status < 400 || status > 599 {
		status = 500
	}
	message := http.StatusText(status)
	if status >= 500 {
		log.FromContext(c.Request().Context()).Errorw("request failed", "status", status)
	}
	_ = c.JSON(status, vo.Response{Code: status, Message: message, RequestID: requestinfo.ID(c.Request().Context())})
}
