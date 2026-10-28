//go:build windows

package appserver

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func environmentKeyEqual(a, b string) bool { return strings.EqualFold(a, b) }
func executablePathsEqual(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func executablePath(candidate string) (string, error) {
	path, err := exec.LookPath(candidate)
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", ErrCodexBinaryUnavailable
	}
	return path, nil
}

func prepareCodexInvocations(path, nodeOverride string) ([]codexInvocation, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".exe":
		if !nativeWindowsExecutable(path) {
			return nil, ErrCodexBinaryUnavailable
		}
		return []codexInvocation{{binary: path}}, nil
	case ".cmd", ".ps1":
		return npmCodexInvocations(filepath.Join(filepath.Dir(path), "node_modules", "@openai", "codex"), nodeOverride)
	case ".js":
		if !strings.EqualFold(filepath.Base(path), "codex.js") || !strings.EqualFold(filepath.Base(filepath.Dir(path)), "bin") {
			return nil, ErrCodexBinaryUnavailable
		}
		return npmCodexInvocations(filepath.Dir(filepath.Dir(path)), nodeOverride)
	default:
		if nativeWindowsExecutable(path) {
			return []codexInvocation{{binary: path}}, nil
		}
		return nil, ErrCodexBinaryUnavailable
	}
}

func npmCodexInvocations(packageRoot, nodeOverride string) ([]codexInvocation, error) {
	file, err := os.Open(filepath.Join(packageRoot, "package.json"))
	if err != nil {
		return nil, ErrCodexBinaryUnavailable
	}
	var metadata struct {
		Name string `json:"name"`
	}
	err = json.NewDecoder(io.LimitReader(file, 64<<10)).Decode(&metadata)
	file.Close()
	if err != nil || metadata.Name != "@openai/codex" {
		return nil, ErrCodexBinaryUnavailable
	}
	arch, target := "x64", "x86_64-pc-windows-msvc"
	if runtime.GOARCH == "arm64" {
		arch, target = "arm64", "aarch64-pc-windows-msvc"
	}
	for _, candidate := range []string{
		filepath.Join(filepath.Dir(packageRoot), "codex-win32-"+arch, "vendor", target, "codex", "codex.exe"),
		filepath.Join(packageRoot, "vendor", target, "codex", "codex.exe"),
	} {
		if nativeWindowsExecutable(candidate) {
			return []codexInvocation{{binary: candidate}}, nil
		}
	}
	script := filepath.Join(packageRoot, "bin", "codex.js")
	if info, err := os.Stat(script); err != nil || !info.Mode().IsRegular() {
		return nil, ErrCodexBinaryUnavailable
	}
	var result []codexInvocation
	for _, candidate := range nodeCandidates(script, nodeOverride) {
		node, err := executablePath(candidate)
		if err == nil && nativeWindowsExecutable(node) && !slices.ContainsFunc(result, func(value codexInvocation) bool { return executablePathsEqual(value.node, node) }) {
			result = append(result, codexInvocation{binary: script, node: node})
		}
	}
	if len(result) == 0 {
		return nil, ErrNodeRuntimeUnavailable
	}
	return result, nil
}

func nativeWindowsExecutable(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	var header [2]byte
	_, err = io.ReadFull(file, header[:])
	return err == nil && bytes.Equal(header[:], []byte("MZ"))
}

func nodeCandidates(binary, override string) []string {
	if override != "" {
		if filepath.IsAbs(override) {
			return []string{override}
		}
		return nil
	}
	candidates := []string{"node.exe", filepath.Join(filepath.Dir(binary), "node.exe")}
	for _, variable := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		if root := os.Getenv(variable); root != "" {
			candidates = append(candidates, filepath.Join(root, "nodejs", "node.exe"))
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".volta", "bin", "node.exe"))
	}
	return candidates
}

func defaultCodexBinaryCandidates() []string {
	var candidates []string
	for _, root := range codexPackageRoots() {
		candidates = append(candidates, filepath.Join(root, "app", "resources", "codex.exe"))
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		directory := filepath.Join(local, "OpenAI", "Codex", "bin")
		entries, _ := os.ReadDir(directory)
		slices.SortStableFunc(entries, func(a, b os.DirEntry) int {
			ai, ae := a.Info()
			bi, be := b.Info()
			if ae == nil && be == nil {
				return bi.ModTime().Compare(ai.ModTime())
			}
			return strings.Compare(b.Name(), a.Name())
		})
		for _, entry := range entries {
			if entry.IsDir() {
				candidates = append(candidates, filepath.Join(directory, entry.Name(), "codex.exe"))
			}
		}
		candidates = append(candidates, filepath.Join(local, "Microsoft", "WinGet", "Links", "codex.exe"))
	}
	candidates = append(candidates, "codex.exe")
	if roaming := os.Getenv("APPDATA"); roaming != "" {
		candidates = append(candidates, filepath.Join(roaming, "npm", "codex.cmd"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".local", "bin", "codex.exe"), filepath.Join(home, ".codex", "plugins", ".plugin-appserver", "codex.exe"))
	}
	return candidates
}

// Query registered packages for this user; do not scan or guess WindowsApps versions.
func codexPackageRoots() []string {
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	packages := kernel.NewProc("GetPackagesByPackageFamily")
	pathFor := kernel.NewProc("GetPackagePathByFullName")
	family, _ := windows.UTF16PtrFromString("OpenAI.Codex_2p2nqsd0c76g0")
	var count, length uint32
	code, _, _ := packages.Call(uintptr(unsafe.Pointer(family)), uintptr(unsafe.Pointer(&count)), 0, uintptr(unsafe.Pointer(&length)), 0)
	if code != uintptr(windows.ERROR_INSUFFICIENT_BUFFER) || count == 0 || count > 128 || length == 0 || length > 1<<20 {
		return nil
	}
	names := make([]*uint16, count)
	buffer := make([]uint16, length)
	code, _, _ = packages.Call(uintptr(unsafe.Pointer(family)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&names[0])), uintptr(unsafe.Pointer(&length)), uintptr(unsafe.Pointer(&buffer[0])))
	if code != 0 {
		return nil
	}
	var roots []string
	for _, name := range names {
		var size uint32
		code, _, _ = pathFor.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&size)), 0)
		if code != uintptr(windows.ERROR_INSUFFICIENT_BUFFER) || size == 0 || size > 32768 {
			continue
		}
		path := make([]uint16, size)
		code, _, _ = pathFor.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&path[0])))
		if code == 0 {
			roots = append(roots, windows.UTF16ToString(path))
		}
	}
	runtime.KeepAlive(buffer)
	return roots
}
