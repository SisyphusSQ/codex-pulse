//go:build windows

package agyprovider

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const stillActiveExitCode = 259

func defaultIsPIDAlive(pid int) bool {
	if pid <= 0 {
		return true
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return err == windows.ERROR_ACCESS_DENIED
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return true
	}
	return code == stillActiveExitCode
}

func defaultListProcesses(ctx context.Context) ([]osProcess, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	script := `Get-CimInstance Win32_Process -Filter "Name LIKE 'agy%' OR Name LIKE 'antigravity-cli%' OR Name LIKE 'language_server%'" | Select-Object ProcessId,ParentProcessId,CommandLine | ConvertTo-Json -Compress`
	cmd := exec.CommandContext(cmdCtx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseWindowsProcessJSON(strings.TrimSpace(string(out))), nil
}

func parseWindowsProcessJSON(raw string) []osProcess {
	if raw == "" {
		return nil
	}
	type item struct {
		ProcessID       int    `json:"ProcessId"`
		ParentProcessID int    `json:"ParentProcessId"`
		CommandLine     string `json:"CommandLine"`
	}
	var list []item
	if strings.HasPrefix(raw, "[") {
		if json.Unmarshal([]byte(raw), &list) != nil {
			return nil
		}
	} else {
		var single item
		if json.Unmarshal([]byte(raw), &single) != nil {
			return nil
		}
		list = []item{single}
	}
	out := make([]osProcess, 0, len(list))
	for _, it := range list {
		if it.ProcessID > 0 {
			out = append(out, osProcess{PID: it.ProcessID, PPID: it.ParentProcessID, CommandLine: it.CommandLine})
		}
	}
	return out
}

func defaultListListeningPorts(ctx context.Context, pid int) ([]int, error) {
	if pid <= 0 {
		return nil, nil
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	script := `Get-NetTCPConnection -OwningProcess ` + strconv.Itoa(pid) + ` -State Listen -ErrorAction SilentlyContinue | Select-Object -ExpandProperty LocalPort | ConvertTo-Json -Compress`
	cmd := exec.CommandContext(cmdCtx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseWindowsPortsJSON(strings.TrimSpace(string(out))), nil
}

func parseWindowsPortsJSON(raw string) []int {
	if raw == "" {
		return nil
	}
	var rawPorts []int
	if strings.HasPrefix(raw, "[") {
		_ = json.Unmarshal([]byte(raw), &rawPorts)
	} else {
		var single int
		if json.Unmarshal([]byte(raw), &single) == nil {
			rawPorts = []int{single}
		}
	}
	seen := make(map[int]struct{}, len(rawPorts))
	var ports []int
	for _, p := range rawPorts {
		if !validPort(p) {
			continue
		}
		if _, dup := seen[p]; !dup {
			seen[p] = struct{}{}
			ports = append(ports, p)
		}
	}
	return ports
}
