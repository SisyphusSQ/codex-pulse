package appserver

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
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
	candidates := defaultCodexBinaryCandidates()
	if options.codexCandidatesForTest != nil {
		candidates = options.codexCandidatesForTest
	}
	invocations, discoveryErr := resolveCodexInvocations(options, candidates)
	if len(invocations) == 0 {
		return result, discoveryErr
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
	command.Stderr = io.Discard
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
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return result, ErrCodexLaunchFailed
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer func() {
		cancelProcess()
		_ = stdin.Close()
		<-done
		if options.OnExit != nil && command.ProcessState != nil {
			options.OnExit(command.ProcessState.UserTime(), command.ProcessState.SystemTime())
		}
	}()

	rpc := newJSONLineRPC(stdin, stdout)
	var initializeResult struct{}
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
		return result, err
	}
	if err := rpc.Notify(ctx, "initialized", struct{}{}); err != nil {
		return result, err
	}
	return operation(ctx, rpc, canonicalHome)
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
	output, err := command.Output()
	if err != nil {
		return inspection
	}
	inspection.Version = parseCodexCLIVersion(string(output))
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

func prepareCodexInvocations(path, nodeOverride string) ([]codexInvocation, error) {
	invocation := codexInvocation{binary: path}
	if !usesEnvNode(path) {
		return []codexInvocation{invocation}, nil
	}
	var invocations []codexInvocation
	seen := make(map[string]bool)
	for _, candidate := range nodeCandidates(path, nodeOverride) {
		node, err := executablePath(candidate)
		if err == nil && !seen[node] {
			seen[node] = true
			invocations = append(invocations, codexInvocation{binary: path, node: node})
		}
	}
	if len(invocations) == 0 {
		return nil, ErrNodeRuntimeUnavailable
	}
	return invocations, nil
}

func usesEnvNode(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()
	var header [256]byte
	read, _ := file.Read(header[:])
	line, _, _ := strings.Cut(string(header[:read]), "\n")
	if !strings.HasPrefix(line, "#!/usr/bin/env") {
		return false
	}
	fields := strings.Fields(strings.TrimPrefix(line, "#!/usr/bin/env"))
	return len(fields) > 0 && (fields[0] == "node" ||
		(fields[0] == "-S" && len(fields) > 1 && fields[1] == "node"))
}

func nodeCandidates(binary, override string) []string {
	if override != "" {
		if filepath.IsAbs(override) {
			return []string{override}
		}
		return nil
	}
	var candidates []string
	for _, directory := range strings.Split(os.Getenv("PATH"), string(os.PathListSeparator)) {
		if directory != "" {
			candidates = append(candidates, filepath.Join(directory, "node"))
		}
	}
	candidates = append(candidates, filepath.Join(filepath.Dir(binary), "node"))
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return append(candidates, "/opt/homebrew/bin/node", "/usr/local/bin/node", "/opt/local/bin/node")
	}
	candidates = append(candidates,
		filepath.Join(home, ".volta", "bin", "node"),
		filepath.Join(home, ".asdf", "shims", "node"),
		filepath.Join(home, ".local", "share", "mise", "shims", "node"),
	)
	for _, pattern := range []string{
		filepath.Join(home, ".nvm", "versions", "node", "*", "bin", "node"),
		filepath.Join(home, ".fnm", "node-versions", "*", "installation", "bin", "node"),
		filepath.Join(home, ".local", "share", "fnm", "node-versions", "*", "installation", "bin", "node"),
	} {
		matches, _ := filepath.Glob(pattern)
		for index := len(matches) - 1; index >= 0; index-- {
			candidates = append(candidates, matches[index])
		}
	}
	return append(candidates, "/opt/homebrew/bin/node", "/usr/local/bin/node", "/opt/local/bin/node")
}

func executablePath(candidate string) (string, error) {
	path, err := exec.LookPath(candidate)
	if err != nil {
		return "", err
	}
	return filepath.Abs(path)
}

func defaultCodexBinaryCandidates() []string {
	candidates := []string{
		"/Applications/ChatGPT.app/Contents/Resources/codex",
		"/Applications/Codex.app/Contents/Resources/codex",
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		candidates = append(candidates,
			filepath.Join(home, "Applications", "ChatGPT.app", "Contents", "Resources", "codex"),
			filepath.Join(home, "Applications", "Codex.app", "Contents", "Resources", "codex"),
			filepath.Join(home, ".local", "bin", "codex"),
		)
	}
	candidates = append(candidates,
		"/opt/homebrew/bin/codex",
		"/usr/local/bin/codex",
	)
	if err != nil || home == "" {
		return candidates
	}
	candidates = append(candidates,
		filepath.Join(home, ".codex", "plugins", ".plugin-appserver", "codex"),
	)
	nvmCandidates, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin", "codex"))
	for index := len(nvmCandidates) - 1; index >= 0; index-- {
		candidates = append(candidates, nvmCandidates[index])
	}
	return candidates
}

func isolatedCodexEnvironment(environment []string, confirmedHome string) []string {
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if strings.HasPrefix(entry, "CODEX_HOME=") {
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
		if !strings.HasPrefix(entry, "PATH=") {
			result = append(result, entry)
			continue
		}
		if pathFound {
			result = append(result, entry)
			continue
		}
		pathValue := strings.TrimPrefix(entry, "PATH=")
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
		if entry == directory {
			return true
		}
	}
	return false
}
