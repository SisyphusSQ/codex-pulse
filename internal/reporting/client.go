package reporting

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
)

type Client struct {
	endpoint string
	http     *http.Client
}

func ValidateEndpoint(input string, allowHTTP bool) (string, error) {
	u, err := url.Parse(strings.TrimSpace(input))
	if err != nil || u.User != nil || u.Hostname() == "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") || strings.ContainsAny(u.Host, "\r\n%") {
		return "", ErrSettings
	}
	if u.Scheme != "https" && (u.Scheme != "http" || !allowHTTP) {
		return "", ErrSettings
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", ErrSettings
		}
	}
	if u.Scheme == "http" {
		if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !privateAddress(ip) {
			return "", ErrSettings
		}
	}
	u.Path = ""
	return u.String(), nil
}
func privateAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	return !ip.IsLinkLocalUnicast() && (ip.IsPrivate() || ip.IsLoopback() || netip.MustParsePrefix("100.64.0.0/10").Contains(ip))
}
func NewClient(input string, allowHTTP bool) (*Client, error) {
	endpoint, err := ValidateEndpoint(input, allowHTTP)
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(endpoint)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}
	if u.Scheme == "http" {
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil || !strings.EqualFold(host, u.Hostname()) {
				return nil, ErrTransport
			}
			expectedPort := u.Port()
			if expectedPort == "" {
				expectedPort = "80"
			}
			if port != expectedPort {
				return nil, ErrTransport
			}
			ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil || len(ips) == 0 {
				return nil, ErrTransport
			}
			for _, ip := range ips {
				if !privateAddress(ip) {
					return nil, ErrTransport
				}
			}
			// DNS is resolved exactly once. Every candidate is checked; the actual socket
			// connects to a checked address, including after DNS changes on later requests.
			for _, ip := range ips {
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
			}
			return nil, ErrTransport
		}
	} else {
		transport.DialContext = dialer.DialContext
	}
	return &Client{endpoint: endpoint, http: &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) Close() { c.http.CloseIdleConnections() }
func (c *Client) request(ctx context.Context, path, credential string, body []byte, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return ErrSettings
	}
	req.Header.Set("Content-Type", "application/json")
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	response, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrTransport
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return ErrReconnect
	}
	if response.StatusCode == 429 || response.StatusCode >= 500 {
		return ErrTransport
	}
	if response.StatusCode != http.StatusOK {
		return ErrProtocol
	}
	bytes, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil {
		return ErrTransport
	}
	if len(bytes) > 64<<10 {
		return ErrProtocol
	}
	var envelope struct {
		Code      int             `json:"code"`
		Message   string          `json:"message"`
		RequestID string          `json:"request_id"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(bytes, &envelope); err != nil || envelope.Code != http.StatusOK || len(envelope.Data) == 0 {
		return ErrProtocol
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		return ErrProtocol
	}
	return nil
}
func (c *Client) Pair(ctx context.Context, code string) (out reportingv1.CollectorPairResponse, err error) {
	code = strings.TrimSpace(code)
	if len(code) < 16 || len(code) > 64 || strings.ContainsAny(code, "\r\n") {
		return out, ErrSettings
	}
	body, _ := json.Marshal(reportingv1.CollectorPairRequest{Code: code, Mode: "collector"})
	err = c.request(ctx, "/api/v1/pair", "", body, &out)
	if err == nil {
		_, idErr := uuid.Parse(out.ClientID)
		bytes, credentialErr := base64.RawURLEncoding.DecodeString(out.Credential)
		if idErr != nil || credentialErr != nil || len(bytes) != 32 {
			return reportingv1.CollectorPairResponse{}, ErrProtocol
		}
	}
	if err == nil && (len(out.ClientID) != 36 || len(out.Credential) != 43 || strings.ContainsAny(out.Credential, "\r\n") || out.ProtocolVersion != reportingv1.Version) {
		err = ErrProtocol
	}
	return
}
func (c *Client) Upload(ctx context.Context, credential string, item queued) (receipt reportingv1.Receipt, err error) {
	err = c.request(ctx, "/api/v1/batches", credential, item.Body, &receipt)
	return
}
