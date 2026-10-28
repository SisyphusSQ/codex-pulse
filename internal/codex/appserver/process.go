package appserver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/diagnostics"
)

// ErrCodexBinaryUnavailable 表示当前环境没有可执行的 Codex CLI。
var ErrCodexBinaryUnavailable = errors.New("Codex binary unavailable")

var ErrNodeRuntimeUnavailable = errors.New("Node runtime unavailable for Codex CLI")
var ErrCodexLaunchFailed = errors.New("start Codex App Server")

type CodexCapabilityState string

const (
	CodexCapabilityUnavailable CodexCapabilityState = "unavailable"
	CodexCapabilityUnverified  CodexCapabilityState = "unverified"
)

type CodexBinaryInspection struct {
	Path            string
	Version         string
	CapabilityState CodexCapabilityState
}

var codexCLIVersionPattern = regexp.MustCompile(`(?i)(?:codex-cli[[:space:]]+)?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?`)

type ProcessOptions struct {
	CodexBinary string
	NodeBinary  string
	PageSize    int
	ClientName  string
	Version     string
	// BeforeStart 在 pipe 就绪后、command.Start 前执行调用方代际检查。
	BeforeStart func(context.Context) error
	// OnExit receives only process CPU counters after the App Server exits.
	OnExit                 func(user, system time.Duration)
	homeBinding            processHomeBinding
	codexCandidatesForTest []string
	// afterBeforeStartForTest 确定性覆盖最后校验返回到 Start 之间的竞态窗口。
	afterBeforeStartForTest func() error
}

type processHomeBinding interface {
	canonicalPath() string
	attach(*exec.Cmd) (string, error)
	validate(context.Context) error
	close() error
}

func ListLocalThreads(ctx context.Context, confirmedHome string, options ProcessOptions) (ThreadList, error) {
	return withInitializedLocalRPC(
		ctx,
		confirmedHome,
		options,
		func(ctx context.Context, rpc *jsonLineRPC, canonicalHome string) (ThreadList, error) {
			return NewThreadLister(
				rpc,
				ThreadListerOptions{PageSize: options.PageSize},
			).List(ctx, canonicalHome)
		},
	)
}

func withInitializedLocalRPC[T any](
	ctx context.Context,
	confirmedHome string,
	options ProcessOptions,
	operation func(context.Context, *jsonLineRPC, string) (T, error),
) (result T, returnErr error) {
	if ctx == nil || operation == nil {
		return result, errors.New("invalid App Server operation")
	}
	canonicalHome := confirmedHome
	if options.homeBinding == nil {
		var err error
		canonicalHome, err = canonicalConfirmedHome(confirmedHome)
		if err != nil {
			return result, err
		}
	} else if canonicalHome != options.homeBinding.canonicalPath() {
		return result, errors.New("invalid App Server Home binding")
	}
	if options.homeBinding == nil {
		binding, err := bindProcessHome(ctx, canonicalHome)
		if err != nil {
			return result, err
		}
		if binding != nil {
			defer binding.close()
			options.homeBinding = binding
		}
	}
	candidates := defaultCodexBinaryCandidates()
	if options.codexCandidatesForTest != nil {
		candidates = options.codexCandidatesForTest
	}
	invocations, discoveryErr := resolveCodexInvocations(options, candidates)
	if len(invocations) == 0 {
		reason := "cli_unavailable"
		if errors.Is(discoveryErr, ErrNodeRuntimeUnavailable) {
			reason = "node_unavailable"
		}
		return result, diagnostics.Wrap(discoveryErr, "cli_resolve", reason)
	}
	var lastCompatibilityErr error
	for _, invocation := range invocations {
		value, err := withInitializedLocalRPCInvocation(ctx, canonicalHome, options, invocation, operation)
		if err == nil {
			return value, nil
		}
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if !candidateCompatibilityFailure(err) {
			return result, err
		}
		lastCompatibilityErr = err
	}
	if discoveryErr != nil {
		return result, errors.Join(lastCompatibilityErr, discoveryErr)
	}
	return result, lastCompatibilityErr
}

type codexInvocation struct {
	binary string
	node   string
}

func candidateCompatibilityFailure(err error) bool {
	return errors.Is(err, ErrCapabilityUnavailable) ||
		errors.Is(err, ErrProtocolIncompatible) ||
		errors.Is(err, ErrRateLimitsSchemaIncompatible) ||
		errors.Is(err, ErrCodexLaunchFailed) || errors.Is(err, io.ErrUnexpectedEOF)
}

func withInitializedLocalRPCInvocation[T any](
	ctx context.Context,
	canonicalHome string,
	options ProcessOptions,
	invocation codexInvocation,
	operation func(context.Context, *jsonLineRPC, string) (T, error),
) (result T, returnErr error) {
	clientName := options.ClientName
	if clientName == "" {
		clientName = "codex-pulse"
	}
	version := options.Version
	if version == "" {
		version = "development"
	}

	processContext, cancelProcess := context.WithCancel(ctx)
	defer cancelProcess()
	command := codexCommand(processContext, invocation, "app-server", "--listen", "stdio://")
	command.WaitDelay = 2 * time.Second
	processHome := canonicalHome
	var err error
	command.Env = codexRuntimeEnvironment(
		isolatedCodexEnvironment(os.Environ(), processHome),
		invocation.binary, invocation.node,
	)
	if options.homeBinding != nil {
		processHome, err = options.homeBinding.attach(command)
		if err != nil {
			return result, err
		}
		command.Env = codexRuntimeEnvironment(
			isolatedCodexEnvironment(command.Env, processHome),
			invocation.binary, invocation.node,
		)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		return result, errors.New("open App Server stdin")
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return result, errors.New("open App Server stdout")
	}
	stderr := new(boundedDiagnosticStderr)
	command.Stderr = stderr
	if options.BeforeStart != nil {
		if err := options.BeforeStart(processContext); err != nil {
			_ = stdin.Close()
			_ = stdout.Close()
			return result, err
		}
	}
	if options.afterBeforeStartForTest != nil {
		if err := options.afterBeforeStartForTest(); err != nil {
			_ = stdin.Close()
			_ = stdout.Close()
			return result, err
		}
	}
	cleanupProcess, err := startCodexProcess(command)
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return result, diagnostics.Wrap(ErrCodexLaunchFailed, "cli_launch", "launch_failed")
	}
	diagnostics.Emit(ctx, diagnostics.Event{Stage: "cli_launch", Outcome: "started"})
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer func() {
		var waitErr error
		var exited bool
		select {
		case waitErr = <-done:
			exited = true
		default:
		}
		cancelProcess()
		cleanupProcess()
		_ = stdin.Close()
		_ = stdout.Close()
		if !exited {
			waitErr = <-done
		}
		if exited && waitErr != nil && command.ProcessState != nil && ctx.Err() == nil {
			diagnostics.Emit(ctx, diagnostics.Event{Stage: "cli_launch", Outcome: "failed", Reason: "unexpected_eof", ExitCode: new(command.ProcessState.ExitCode())})
		}
		if command.ProcessState != nil {
			diagnostics.Emit(ctx, diagnostics.Event{Stage: "cli_launch", Outcome: "stopped", ExitCode: new(command.ProcessState.ExitCode())})
		}
		if returnErr != nil && ctx.Err() == nil {
			if reason := stderr.reason(); reason != "" {
				diagnostics.Emit(ctx, diagnostics.Event{Stage: "stderr_hint", Outcome: "failed", Reason: reason})
			}
		}
		if options.OnExit != nil && command.ProcessState != nil {
			options.OnExit(command.ProcessState.UserTime(), command.ProcessState.SystemTime())
		}
	}()

	rpc := newJSONLineRPC(stdin, stdout)
	var initializeResult struct {
		UserAgent string `json:"userAgent"`
	}
	if err := rpc.Call(ctx, "initialize", struct {
		ClientInfo struct {
			Name    string `json:"name"`
			Title   string `json:"title"`
			Version string `json:"version"`
		} `json:"clientInfo"`
	}{ClientInfo: struct {
		Name    string `json:"name"`
		Title   string `json:"title"`
		Version string `json:"version"`
	}{Name: clientName, Title: "Codex Pulse", Version: version}}, &initializeResult); err != nil {
		return result, diagnostics.Wrap(err, "initialize", "rpc_error")
	}
	if match := codexCLIVersionPattern.FindStringSubmatch(initializeResult.UserAgent); len(match) >= 4 {
		diagnostics.Emit(ctx, diagnostics.Event{Stage: "initialize", Outcome: "succeeded", CodexVersion: strings.Join(match[1:4], ".")})
	}
	if err := rpc.Notify(ctx, "initialized", struct{}{}); err != nil {
		return result, err
	}
	return operation(ctx, rpc, canonicalHome)
}

// stderr 只在短生命周期内保留至多 64 KiB，退出后仅输出固定提示分类。
// 不记录服务器正文；提示不是 HTTP 状态的确证，不改变 RPC 错误语义。
type boundedDiagnosticStderr struct{ content bytes.Buffer }

func (writer *boundedDiagnosticStderr) Write(content []byte) (int, error) {
	n := len(content)
	if remaining := (64 << 10) - writer.content.Len(); remaining > 0 {
		_, _ = writer.content.Write(content[:min(n, remaining)])
	}
	return n, nil
}
func (writer *boundedDiagnosticStderr) reason() string {
	value := strings.ToLower(writer.content.String())
	for _, hint := range []struct {
		markers []string
		reason  string
	}{
		{[]string{"http 429", "status 429", "429 too many requests"}, "http_429"},
		{[]string{"http 401", "status 401", "401 unauthorized"}, "auth_required"},
		{[]string{"timed out", "timeout"}, "timeout"},
	} {
		for _, marker := range hint.markers {
			if strings.Contains(value, marker) {
				return hint.reason
			}
		}
	}
	return ""
}

func InspectCodexBinary(path string) (CodexBinaryInspection, error) {
	if path == "" {
		return CodexBinaryInspection{CapabilityState: CodexCapabilityUnavailable}, ErrCodexBinaryUnavailable
	}
	resolved, err := executablePath(path)
	if err != nil {
		return CodexBinaryInspection{CapabilityState: CodexCapabilityUnavailable}, ErrCodexBinaryUnavailable
	}
	invocations, err := prepareCodexInvocations(resolved, "")
	if err != nil {
		return CodexBinaryInspection{Path: resolved, CapabilityState: CodexCapabilityUnavailable}, err
	}
	return inspectCodexBinary(invocations[0]), nil
}

func resolveCodexInvocations(options ProcessOptions, fallbacks []string) ([]codexInvocation, error) {
	explicit := options.CodexBinary
	if explicit == "" {
		explicit = os.Getenv("CODEX_PULSE_CODEX_BINARY")
	}
	nodeOverride := options.NodeBinary
	if nodeOverride == "" {
		nodeOverride = os.Getenv("CODEX_PULSE_NODE_BINARY")
	}
	if explicit != "" {
		if !filepath.IsAbs(explicit) {
			return nil, ErrCodexBinaryUnavailable
		}
		path, err := executablePath(explicit)
		if err != nil {
			return nil, ErrCodexBinaryUnavailable
		}
		invocations, err := prepareCodexInvocations(path, nodeOverride)
		if err != nil {
			return nil, err
		}
		return invocations, nil
	}
	var invocations []codexInvocation
	var discoveryErr error
	seen := make(map[string]bool)
	candidates := append(append([]string(nil), fallbacks...), "codex")
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		path, err := executablePath(candidate)
		if err != nil || seen[path] {
			continue
		}
		seen[path] = true
		prepared, err := prepareCodexInvocations(path, nodeOverride)
		if err != nil {
			discoveryErr = errors.Join(discoveryErr, err)
			continue
		}
		invocations = append(invocations, prepared...)
	}
	if len(invocations) > 0 {
		return invocations, discoveryErr
	}
	if discoveryErr != nil {
		return nil, discoveryErr
	}
	return nil, ErrCodexBinaryUnavailable
}

func inspectCodexBinary(invocation codexInvocation) CodexBinaryInspection {
	inspection := CodexBinaryInspection{Path: invocation.binary, CapabilityState: CodexCapabilityUnverified}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := codexCommand(ctx, invocation, "--version")
	command.Env = codexRuntimeEnvironment(os.Environ(), invocation.binary, invocation.node)
	var output boundedDiagnosticStderr
	command.Stdout = &output
	command.Stderr = io.Discard
	command.WaitDelay = 2 * time.Second
	cleanup, err := startCodexProcess(command)
	if err != nil {
		return inspection
	}
	defer cleanup()
	if err := command.Wait(); err != nil {
		return inspection
	}
	inspection.Version = parseCodexCLIVersion(output.content.String())
	return inspection
}

func codexCommand(ctx context.Context, invocation codexInvocation, arguments ...string) *exec.Cmd {
	if invocation.node != "" {
		return exec.CommandContext(ctx, invocation.node, append([]string{invocation.binary}, arguments...)...)
	}
	return exec.CommandContext(ctx, invocation.binary, arguments...)
}

func parseCodexCLIVersion(output string) string {
	match := codexCLIVersionPattern.FindStringSubmatch(strings.TrimSpace(output))
	if match == nil {
		return ""
	}
	display := match[1] + "." + match[2] + "." + match[3]
	if match[4] != "" {
		return display + "-" + match[4]
	}
	return display
}

func isolatedCodexEnvironment(environment []string, confirmedHome string) []string {
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		if environmentKeyEqual(key, "CODEX_HOME") {
			continue
		}
		result = append(result, entry)
	}
	return append(result, "CODEX_HOME="+confirmedHome)
}

func codexRuntimeEnvironment(environment []string, binary, node string) []string {
	result := prependPathEntry(environment, filepath.Dir(binary))
	if node != "" {
		result = prependPathEntry(result, filepath.Dir(node))
	}
	return result
}

func prependPathEntry(environment []string, directory string) []string {
	directory = filepath.Clean(directory)
	if directory == "." || directory == "" {
		return append([]string(nil), environment...)
	}
	result := make([]string, 0, len(environment)+1)
	pathFound := false
	for _, entry := range environment {
		key, pathValue, _ := strings.Cut(entry, "=")
		if !environmentKeyEqual(key, "PATH") {
			result = append(result, entry)
			continue
		}
		if pathFound {
			continue
		}
		if !pathListContains(pathValue, directory) {
			if pathValue == "" {
				pathValue = directory
			} else {
				pathValue = directory + string(os.PathListSeparator) + pathValue
			}
		}
		result = append(result, "PATH="+pathValue)
		pathFound = true
	}
	if !pathFound {
		result = append(result, "PATH="+directory)
	}
	return result
}

func pathListContains(pathValue, directory string) bool {
	for _, entry := range strings.Split(pathValue, string(os.PathListSeparator)) {
		if executablePathsEqual(entry, directory) {
			return true
		}
	}
	return false
}
