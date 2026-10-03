package http

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/SisyphusSQ/codex-pulse/server/internal/lib/log"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/vo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/requestinfo"
	access_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

type EchoMiddleware struct {
	config            config.Config
	access            *access_srv.Access
	pairing           *pairingLimiter
	metricsDigest     [32]byte
	metricsCredential bool
}

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func InitMiddleware(cfg config.Config, access *access_srv.Access) *EchoMiddleware {
	return &EchoMiddleware{config: cfg, access: access, pairing: newPairingLimiter()}
}
func (e *EchoMiddleware) CORS(next echo.HandlerFunc) echo.HandlerFunc {
	// 空白名单表示仅同源访问；Echo v5 不接受空 AllowOrigins。
	if len(e.config.Server.CORSOrigins) == 0 {
		return next
	}
	return middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     e.config.Server.CORSOrigins,
		AllowHeaders:     []string{"Content-Type", "Authorization", "X-Pulse-CSRF", "X-Request-ID"},
		AllowCredentials: !slices.Contains(e.config.Server.CORSOrigins, "*"),
		ExposeHeaders:    []string{"X-Request-ID"},
	})(next)
}
func (e *EchoMiddleware) Recover(next echo.HandlerFunc) echo.HandlerFunc {
	return middleware.Recover()(next)
}

// Deadline 给一次完整请求共享截止时间，数据库/事务不能逐次延长整体预算。
func (e *EchoMiddleware) Deadline(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		ctx, cancel := context.WithTimeout(c.Request().Context(), e.config.ContextTimeout)
		defer cancel()
		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
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

const principalKey = "pulse.principal"

func Principal(c *echo.Context) access_dto.Principal {
	principal, _ := c.Get(principalKey).(access_dto.Principal)
	return principal
}

func SessionCookieName(origin string) string {
	if strings.HasPrefix(origin, "https://") {
		return "__Host-pulse_session"
	}
	return "pulse_session"
}

func (e *EchoMiddleware) Auth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		request := c.Request()
		c.Response().Header().Set("X-Content-Type-Options", "nosniff")
		c.Response().Header().Set("Cache-Control", "no-store")
		c.Response().Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'")
		if (request.Method == http.MethodGet || request.Method == http.MethodHead) && (request.URL.Path == "/health" || request.URL.Path == "/ready") {
			return next(c)
		}
		// 公开静态壳仅提供登录界面，业务 API 永远通过统一授权。
		if (request.Method == http.MethodGet || request.Method == http.MethodHead) && (c.Path() == "/" || c.Path() == "/assets/*") {
			return next(c)
		}
		if e.config.Server.Metrics && request.URL.Path == "/metrics" && (request.Method == http.MethodGet || request.Method == http.MethodHead) && request.Header.Get("Authorization") != "" {
			credential, ok := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
			digest := sha256.Sum256([]byte(credential))
			if !ok || !e.metricsCredential || subtle.ConstantTimeCompare(digest[:], e.metricsDigest[:]) != 1 {
				return utils.ErrUnauthorized
			}
			return next(c)
		}
		origin, err := RequestOrigin(request, e.config)
		if err != nil {
			return err
		}
		if request.Method == http.MethodPost && request.URL.Path == "/api/v1/pair" {
			if !e.pairing.Allow(request.RemoteAddr, time.Now()) {
				return echo.NewHTTPError(http.StatusTooManyRequests, http.StatusText(http.StatusTooManyRequests))
			}
			return next(c)
		}
		if e.access == nil {
			return utils.ErrUnauthorized
		}
		mutation := request.Method != http.MethodGet && request.Method != http.MethodHead && request.Method != http.MethodOptions
		var principal access_dto.Principal
		if header := request.Header.Get("Authorization"); header != "" {
			credential, ok := strings.CutPrefix(header, "Bearer ")
			if !ok {
				return utils.ErrUnauthorized
			}
			principal, err = e.access.Authenticate(request.Context(), credential, "", "", false, mutation)
			if err != nil {
				return err
			}
			allowed := (request.Method == http.MethodPost && request.URL.Path == "/api/v1/batches") || (request.Method == http.MethodGet && request.URL.Path == "/api/v1/sync")
			if !allowed {
				return utils.ErrForbidden
			}
		} else {
			cookie, cookieErr := request.Cookie(SessionCookieName(origin))
			if cookieErr != nil {
				return utils.ErrUnauthorized
			}
			if mutation && request.Header.Get("Origin") != origin {
				return utils.ErrForbidden
			}
			principal, err = e.access.Authenticate(request.Context(), cookie.Value, origin, request.Header.Get("X-Pulse-CSRF"), true, mutation)
			if err != nil {
				return err
			}
		}
		c.Set(principalKey, principal)
		return next(c)
	}
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

// loadMetricsCredential 只加载专用只读采集密钥，绝不复用设备或浏览器凭据。
func (e *EchoMiddleware) loadMetricsCredential() error {
	path := e.config.Server.MetricsTokenFile
	if !e.config.Server.Metrics || path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 4096 {
		return fmt.Errorf("metrics credential requires a private regular file")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read metrics credential file failed")
	}
	token := strings.TrimSpace(string(body))
	if len(token) < 32 || len(token) > 256 || strings.ContainsAny(token, " \r\n\t") {
		return fmt.Errorf("metrics credential requires 32-256 non-whitespace characters")
	}
	e.metricsDigest = sha256.Sum256([]byte(token))
	e.metricsCredential = true
	return nil
}
