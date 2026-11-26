package agyprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	lsCallTimeout      = 6 * time.Second
	lsMaxResponseBytes = 8 << 20
)

var (
	ErrLSUnavailable    = errors.New("antigravity language server rpc unavailable")
	ErrLSMethodRejected = errors.New("antigravity language server method not allowed")
	ErrAccountMismatch  = errors.New("antigravity account changed during quota fetch")
)

type LSQuotaInfo struct {
	RemainingFraction *float64 `json:"remainingFraction"`
	ResetTime         string   `json:"resetTime"`
	Disabled          bool     `json:"disabled"`
	Description       string   `json:"description"`
}

type LSModelOrAlias struct {
	Model string `json:"model"`
}

type LSClientModelConfig struct {
	Label        string          `json:"label"`
	ModelID      string          `json:"modelId"`
	ModelOrAlias *LSModelOrAlias `json:"modelOrAlias"`
	QuotaInfo    *LSQuotaInfo    `json:"quotaInfo"`
}

type LSUserStatus struct {
	Email              string
	Name               string
	PlanName           string
	TierName           string
	ClientModelConfigs []LSClientModelConfig
}

func (s LSUserStatus) AccountIdentity() string {
	if e := strings.TrimSpace(s.Email); e != "" {
		return strings.ToLower(e)
	}
	return strings.ToLower(strings.TrimSpace(s.Name))
}

type LSQuotaBucket struct {
	BucketID          string   `json:"bucketId"`
	DisplayName       string   `json:"displayName"`
	Window            string   `json:"window"`
	RemainingFraction *float64 `json:"remainingFraction"`
	ResetTime         string   `json:"resetTime"`
	Disabled          bool     `json:"disabled"`
	Description       string   `json:"description"`
}

type LSQuotaGroup struct {
	DisplayName string          `json:"displayName"`
	Description string          `json:"description"`
	Buckets     []LSQuotaBucket `json:"buckets"`
}

type LSQuotaSummary struct {
	Groups  []LSQuotaGroup  `json:"groups"`
	Buckets []LSQuotaBucket `json:"buckets"`
}

// LSSandwichResult 是经过 GetUserStatus -> RetrieveUserQuotaSummary -> GetUserStatus 夹心校验的可信结果。
type LSSandwichResult struct {
	Status       LSUserStatus
	QuotaSummary LSQuotaSummary
	PlanType     string
}

// LSClient 严格限定仅向本机回环 Language Server 发起 GetUserStatus 与 RetrieveUserQuotaSummary 只读 RPC。
type LSClient struct {
	discoverer *LSDiscoverer
	httpClient *http.Client

	mu       sync.Mutex
	endpoint *LSEndpoint
}

func NewLSClient(discoverer *LSDiscoverer, httpClient *http.Client) *LSClient {
	if httpClient == nil {
		httpClient = newLoopbackHTTPClient(lsCallTimeout)
	}
	return &LSClient{
		discoverer: discoverer,
		httpClient: httpClient,
	}
}

// FetchAccountAndQuota 执行账号夹心校验：
// GetUserStatus -> RetrieveUserQuotaSummary({forceRefresh: true}) -> GetUserStatus，
// 切号竞态下直接丢弃脏观测并返回 ErrAccountMismatch。
func (c *LSClient) FetchAccountAndQuota(ctx context.Context) (LSSandwichResult, error) {
	before, err := c.GetUserStatus(ctx)
	if err != nil {
		return LSSandwichResult{}, err
	}
	beforeID := before.AccountIdentity()
	if beforeID == "" {
		return LSSandwichResult{}, ErrLSUnavailable
	}
	summary, err := c.RetrieveUserQuotaSummary(ctx)
	if err != nil {
		return LSSandwichResult{}, err
	}
	after, err := c.GetUserStatus(ctx)
	if err != nil {
		return LSSandwichResult{}, err
	}
	afterID := after.AccountIdentity()
	if afterID == "" || afterID != beforeID {
		return LSSandwichResult{}, ErrAccountMismatch
	}
	// 以第二次状态为准（确保模型配置与最新账号一致），并补全计划名
	status := after
	if status.PlanName == "" {
		status.PlanName = before.PlanName
	}
	if status.TierName == "" {
		status.TierName = before.TierName
	}
	if len(status.ClientModelConfigs) == 0 {
		status.ClientModelConfigs = before.ClientModelConfigs
	}
	return LSSandwichResult{
		Status:       status,
		QuotaSummary: summary,
		PlanType:     NormalizeAGYPlanType(status.PlanName, status.TierName),
	}, nil
}

func (c *LSClient) GetUserStatus(ctx context.Context) (LSUserStatus, error) {
	raw, err := c.call(ctx, lsMethodGetUserStatus, map[string]any{})
	if err != nil {
		return LSUserStatus{}, err
	}
	var env struct {
		UserStatus *struct {
			Email      string `json:"email"`
			Name       string `json:"name"`
			PlanStatus *struct {
				PlanInfo *struct {
					PlanName    string `json:"planName"`
					TeamsTier   string `json:"teamsTier"`
					DisplayName string `json:"displayName"`
				} `json:"planInfo"`
			} `json:"planStatus"`
			UserTier *struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"userTier"`
			CascadeModelConfigData *struct {
				ClientModelConfigs []LSClientModelConfig `json:"clientModelConfigs"`
			} `json:"cascadeModelConfigData"`
		} `json:"userStatus"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || env.UserStatus == nil {
		return LSUserStatus{}, ErrLSUnavailable
	}
	us := env.UserStatus
	var planName, tierName string
	if us.PlanStatus != nil && us.PlanStatus.PlanInfo != nil {
		planName = firstNonEmpty(us.PlanStatus.PlanInfo.DisplayName, us.PlanStatus.PlanInfo.PlanName, us.PlanStatus.PlanInfo.TeamsTier)
	}
	if us.UserTier != nil {
		tierName = firstNonEmpty(us.UserTier.Name, us.UserTier.ID)
	}
	var configs []LSClientModelConfig
	if us.CascadeModelConfigData != nil {
		configs = us.CascadeModelConfigData.ClientModelConfigs
	}
	return LSUserStatus{
		Email:              strings.TrimSpace(us.Email),
		Name:               strings.TrimSpace(us.Name),
		PlanName:           strings.TrimSpace(planName),
		TierName:           strings.TrimSpace(tierName),
		ClientModelConfigs: configs,
	}, nil
}

func (c *LSClient) RetrieveUserQuotaSummary(ctx context.Context) (LSQuotaSummary, error) {
	raw, err := c.call(ctx, lsMethodRetrieveQuota, map[string]any{"forceRefresh": true})
	if err != nil {
		return LSQuotaSummary{}, err
	}
	// 兼容顶层直接返回或嵌套在 {"response": ...} 中
	var wrapper struct {
		Response json.RawMessage `json:"response"`
		Groups   []LSQuotaGroup  `json:"groups"`
		Buckets  []LSQuotaBucket `json:"buckets"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return LSQuotaSummary{}, ErrLSUnavailable
	}
	if len(wrapper.Response) > 0 && wrapper.Response[0] == '{' {
		var inner LSQuotaSummary
		if err := json.Unmarshal(wrapper.Response, &inner); err == nil {
			return inner, nil
		}
	}
	return LSQuotaSummary{Groups: wrapper.Groups, Buckets: wrapper.Buckets}, nil
}

func (c *LSClient) call(ctx context.Context, method string, payload map[string]any) ([]byte, error) {
	if c == nil {
		return nil, ErrLSUnavailable
	}
	// 严格白名单：禁止调用 GetAllCascadeTrajectories / GetCascadeTrajectory 等轨迹接口
	if method != lsMethodGetUserStatus && method != lsMethodRetrieveQuota {
		return nil, ErrLSMethodRejected
	}
	ep, err := c.ensureEndpoint(ctx, false)
	if err != nil {
		return nil, err
	}
	raw, status, err := c.postOnce(ctx, ep, method, payload)
	if err == nil {
		return raw, nil
	}
	if errors.Is(err, ErrLSNonLoopback) || errors.Is(err, ErrLSRedirectBlocked) {
		return nil, err
	}
	// 网络错误或 401/403 时重发现一次端点后重试
	if status == 0 || status == http.StatusUnauthorized || status == http.StatusForbidden {
		newEP, discErr := c.ensureEndpoint(ctx, true)
		if discErr == nil && (newEP.BaseURL != ep.BaseURL || newEP.CSRFToken != ep.CSRFToken) {
			raw2, _, err2 := c.postOnce(ctx, newEP, method, payload)
			if err2 == nil {
				return raw2, nil
			}
			return nil, err2
		}
	}
	return nil, err
}

func (c *LSClient) ensureEndpoint(ctx context.Context, force bool) (LSEndpoint, error) {
	c.mu.Lock()
	if !force && c.endpoint != nil && c.endpoint.BaseURL != "" && c.endpoint.CSRFToken != "" {
		ep := *c.endpoint
		c.mu.Unlock()
		return ep, nil
	}
	c.mu.Unlock()

	if c.discoverer == nil {
		return LSEndpoint{}, ErrLSNotFound
	}
	found, err := c.discoverer.Discover(ctx)
	if err != nil {
		return LSEndpoint{}, err
	}
	if !isStrictLoopbackURL(found.BaseURL) {
		return LSEndpoint{}, ErrLSNonLoopback
	}
	c.mu.Lock()
	c.endpoint = &found
	c.mu.Unlock()
	return found, nil
}

func (c *LSClient) postOnce(ctx context.Context, ep LSEndpoint, method string, payload map[string]any) ([]byte, int, error) {
	if !isStrictLoopbackURL(ep.BaseURL) {
		return nil, 0, ErrLSNonLoopback
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	callCtx, cancel := context.WithTimeout(ctx, lsCallTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(
		callCtx,
		http.MethodPost,
		strings.TrimRight(ep.BaseURL, "/")+lsServicePath+method,
		bytes.NewReader(bodyBytes),
	)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-codeium-csrf-token", ep.CSRFToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, ErrLSRedirectBlocked) {
			return nil, 0, ErrLSRedirectBlocked
		}
		return nil, 0, fmt.Errorf("%w: %v", ErrLSUnavailable, err)
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, lsMaxResponseBytes+1))
	if readErr != nil || int64(len(raw)) > lsMaxResponseBytes {
		return nil, resp.StatusCode, ErrLSUnavailable
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 严禁把原始响应体或 CSRF token 放入错误信息
		return nil, resp.StatusCode, fmt.Errorf("%w: status %d", ErrLSUnavailable, resp.StatusCode)
	}
	return raw, resp.StatusCode, nil
}

// NormalizeAGYPlanType 将 Antigravity 上报的套餐/档位名映射为标准显示标签。
func NormalizeAGYPlanType(planName, tierName string) string {
	combined := strings.ToLower(strings.TrimSpace(planName + " " + tierName))
	if combined == "" {
		return ""
	}
	switch {
	case strings.Contains(combined, "ultra") && (strings.Contains(combined, "20") || strings.Contains(combined, "max")):
		return "Google AI Ultra 20×"
	case strings.Contains(combined, "ultra") && strings.Contains(combined, "5"):
		return "Google AI Ultra 5×"
	case strings.Contains(combined, "ultra"):
		return "Google AI Ultra 20×"
	case strings.Contains(combined, "pro"):
		return "Google AI Pro"
	case strings.Contains(combined, "free") || strings.Contains(combined, "standard") || strings.Contains(combined, "basic"):
		return "Google AI Free"
	default:
		return sanitizeLabel(firstNonEmpty(tierName, planName), "")
	}
}
