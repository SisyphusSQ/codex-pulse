package agyprovider

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	lsServicePath         = "/exa.language_server_pb.LanguageServerService/"
	lsMethodGetUserStatus = "GetUserStatus"
	lsMethodRetrieveQuota = "RetrieveUserQuotaSummary"
	lsMaxHubFileBytes     = 256 << 10
	lsProbeTimeout        = 1500 * time.Millisecond
)

var (
	ErrLSNotFound        = errors.New("antigravity language server endpoint not found")
	ErrLSNonLoopback     = errors.New("antigravity language server URL must be loopback")
	ErrLSRedirectBlocked = errors.New("antigravity language server redirect blocked")

	lsCmdBinaryRe   = regexp.MustCompile(`(?i)(?:^|[\s/\\"'])(?:agy|antigravity-cli)(?:\.exe)?(?:["'\s]|$)`)
	lsCmdHubFlagRe  = regexp.MustCompile(`(?i)(?:--hub\b|\bremote-control\b|--csrf[-_]token\b)`)
	lsCmdPortRe     = regexp.MustCompile(`(?i)--(?:hub[-_]port|http[-_]port|https[-_]server[-_]port|server[-_]port)(?:=|\s+)["']?(\d{1,5})["']?(?:\s|$)`)
	lsCmdCSRFRe     = regexp.MustCompile(`(?i)--csrf[-_]token(?:=|\s+)(?:"([^"]+)"|'([^']+)'|([^\s"']+))`)
	lsHubFileNameRe = regexp.MustCompile(`(?i)^hub_.*\.json$`)
)

// LSEndpoint 描述已通过握手验证的本机回环 Language Server 端点（仅内存持有，严禁持久化或写日志）。
type LSEndpoint struct {
	BaseURL   string
	CSRFToken string
	PID       int
}

type osProcess struct {
	PID         int
	PPID        int
	CommandLine string
}

// LSDiscoverer 按 daemon/hub_*.json 优先、跨平台进程枚举兜底的顺序发现本机回环 LS 端点。
type LSDiscoverer struct {
	DaemonDirs         []string
	HTTPClient         *http.Client
	IsPIDAlive         func(int) bool
	ListProcesses      func(context.Context) ([]osProcess, error)
	ListListeningPorts func(context.Context, int) ([]int, error)
}

func NewLSDiscoverer(baseRoot string) *LSDiscoverer {
	daemonDirs := make([]string, 0, len(ProductDirectories))
	if baseRoot != "" {
		for _, product := range ProductDirectories {
			daemonDirs = append(daemonDirs, filepath.Join(baseRoot, product, "daemon"))
		}
	}
	return &LSDiscoverer{
		DaemonDirs:         daemonDirs,
		HTTPClient:         newLoopbackHTTPClient(lsProbeTimeout),
		IsPIDAlive:         defaultIsPIDAlive,
		ListProcesses:      defaultListProcesses,
		ListListeningPorts: defaultListListeningPorts,
	}
}

func newLoopbackHTTPClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{
		// 仅限本机回环 HTTPS 允许 Antigravity 自签证书；外网地址在发起前已由 isStrictLoopbackURL 拒绝。
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // strictly gated to 127.0.0.1 / localhost / ::1
		},
		DisableKeepAlives: true,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return ErrLSRedirectBlocked
		},
	}
}

func isStrictLoopbackURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	if parsed.User != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

type hubTarget struct {
	port   int
	protos []string
}

type hubCandidate struct {
	pid       int
	mtimeNS   int64
	csrfToken string
	targets   []hubTarget
}

func (d *LSDiscoverer) Discover(ctx context.Context) (LSEndpoint, error) {
	if d == nil {
		return LSEndpoint{}, ErrLSNotFound
	}
	isAlive := d.IsPIDAlive
	if isAlive == nil {
		isAlive = defaultIsPIDAlive
	}
	client := d.HTTPClient
	if client == nil {
		client = newLoopbackHTTPClient(lsProbeTimeout)
	}

	probed := make(map[string]struct{})
	// 1. 优先读取 ~/.gemini/<product>/daemon/hub_*.json
	for _, cand := range readHubCandidates(d.DaemonDirs, isAlive) {
		for _, target := range cand.targets {
			if err := ctx.Err(); err != nil {
				return LSEndpoint{}, err
			}
			key := strconv.Itoa(target.port) + ":" + cand.csrfToken
			if _, seen := probed[key]; seen {
				continue
			}
			probed[key] = struct{}{}
			if ep, ok := probeLoopbackEndpoint(ctx, client, target.port, cand.csrfToken, target.protos); ok {
				ep.PID = cand.pid
				return ep, nil
			}
		}
	}

	// 2. 回退跨平台进程与监听端口发现
	listProcs := d.ListProcesses
	if listProcs == nil {
		listProcs = defaultListProcesses
	}
	listPorts := d.ListListeningPorts
	if listPorts == nil {
		listPorts = defaultListListeningPorts
	}
	procs, err := listProcs(ctx)
	if err != nil || len(procs) == 0 {
		return LSEndpoint{}, ErrLSNotFound
	}

	type procCand struct {
		pid       int
		priority  int
		port      int
		csrfToken string
	}
	var cands []procCand
	for _, p := range procs {
		parsed := parseLSCommandLine(p.CommandLine)
		if parsed.csrfToken == "" || (!parsed.isHub && !parsed.isLanguageServer) {
			continue
		}
		prio := 50
		if parsed.isHub && parsed.port > 0 {
			prio = 80
		}
		cands = append(cands, procCand{
			pid: p.PID, priority: prio, port: parsed.port, csrfToken: parsed.csrfToken,
		})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].priority != cands[j].priority {
			return cands[i].priority > cands[j].priority
		}
		return cands[i].pid > cands[j].pid
	})

	for _, cand := range cands {
		var ports []int
		if cand.port > 0 {
			ports = []int{cand.port}
		} else {
			ports, _ = listPorts(ctx, cand.pid)
		}
		for _, port := range ports {
			if err := ctx.Err(); err != nil {
				return LSEndpoint{}, err
			}
			key := strconv.Itoa(port) + ":" + cand.csrfToken
			if _, seen := probed[key]; seen {
				continue
			}
			probed[key] = struct{}{}
			if ep, ok := probeLoopbackEndpoint(ctx, client, port, cand.csrfToken, []string{"http", "https"}); ok {
				ep.PID = cand.pid
				return ep, nil
			}
		}
	}

	return LSEndpoint{}, ErrLSNotFound
}

func readHubCandidates(dirs []string, isAlive func(int) bool) []hubCandidate {
	var cands []hubCandidate
	seen := make(map[string]struct{})
	for _, dir := range dirs {
		entries, err := readDirSafe(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !lsHubFileNameRe.MatchString(entry.Name()) {
				continue
			}
			fullPath := filepath.Join(dir, entry.Name())
			file, info, err := openRegularFileReadOnly(fullPath)
			if err != nil {
				continue
			}
			if info.Size() <= 0 || info.Size() > lsMaxHubFileBytes {
				_ = file.Close()
				continue
			}
			raw, readErr := io.ReadAll(io.LimitReader(file, lsMaxHubFileBytes))
			_ = file.Close()
			if readErr != nil {
				continue
			}
			var doc struct {
				PID         int    `json:"pid"`
				HTTPPort    int    `json:"httpPort"`
				Port        int    `json:"port"`
				HTTPSPort   int    `json:"httpsPort"`
				CSRFToken   string `json:"csrfToken"`
				CSRFTokenSN string `json:"csrf_token"`
			}
			if json.Unmarshal(raw, &doc) != nil {
				continue
			}
			csrf := strings.TrimSpace(firstNonEmpty(doc.CSRFToken, doc.CSRFTokenSN))
			if csrf == "" {
				continue
			}
			if doc.PID > 0 && !isAlive(doc.PID) {
				continue
			}
			httpPort := doc.HTTPPort
			if httpPort <= 0 {
				httpPort = doc.Port
			}
			httpsPort := doc.HTTPSPort
			var targets []hubTarget
			if validPort(httpPort) {
				targets = append(targets, hubTarget{port: httpPort, protos: []string{"http", "https"}})
			}
			if validPort(httpsPort) && httpsPort != httpPort {
				targets = append(targets, hubTarget{port: httpsPort, protos: []string{"https", "http"}})
			}
			if len(targets) == 0 {
				continue
			}
			key := fmt.Sprintf("%d:%d:%d:%s", doc.PID, httpPort, httpsPort, csrf)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			cands = append(cands, hubCandidate{
				pid:       doc.PID,
				mtimeNS:   info.ModTime().UnixNano(),
				csrfToken: csrf,
				targets:   targets,
			})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].mtimeNS != cands[j].mtimeNS {
			return cands[i].mtimeNS > cands[j].mtimeNS
		}
		return cands[i].pid > cands[j].pid
	})
	return cands
}

type parsedLSCommand struct {
	port             int
	csrfToken        string
	isHub            bool
	isLanguageServer bool
}

func parseLSCommandLine(commandLine string) parsedLSCommand {
	cmd := strings.TrimSpace(commandLine)
	if cmd == "" {
		return parsedLSCommand{}
	}
	isAgyBin := lsCmdBinaryRe.MatchString(cmd)
	isHub := isAgyBin && lsCmdHubFlagRe.MatchString(cmd)
	isLS := strings.Contains(strings.ToLower(cmd), "language_server")
	var port int
	if m := lsCmdPortRe.FindStringSubmatch(cmd); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil && validPort(n) {
			port = n
		}
	}
	var csrf string
	if m := lsCmdCSRFRe.FindStringSubmatch(cmd); len(m) == 4 {
		csrf = strings.TrimSpace(firstNonEmpty(m[1], m[2], m[3]))
	}
	return parsedLSCommand{
		port:             port,
		csrfToken:        csrf,
		isHub:            isHub,
		isLanguageServer: isLS,
	}
}

func probeLoopbackEndpoint(
	ctx context.Context,
	client *http.Client,
	port int,
	csrfToken string,
	protos []string,
) (LSEndpoint, bool) {
	if !validPort(port) || csrfToken == "" {
		return LSEndpoint{}, false
	}
	for _, proto := range protos {
		if proto != "http" && proto != "https" {
			continue
		}
		baseURL := fmt.Sprintf("%s://127.0.0.1:%d", proto, port)
		if !isStrictLoopbackURL(baseURL) {
			continue
		}
		probeCtx, cancel := context.WithTimeout(ctx, lsProbeTimeout)
		req, err := http.NewRequestWithContext(
			probeCtx,
			http.MethodPost,
			baseURL+lsServicePath+lsMethodGetUserStatus,
			bytes.NewReader([]byte("{}")),
		)
		if err != nil {
			cancel()
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-codeium-csrf-token", csrfToken)
		resp, err := client.Do(req)
		cancel()
		if err != nil {
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return LSEndpoint{BaseURL: baseURL, CSRFToken: csrfToken}, true
		}
	}
	return LSEndpoint{}, false
}

func validPort(p int) bool {
	return p > 0 && p <= 65535
}
