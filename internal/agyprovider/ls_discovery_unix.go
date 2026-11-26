//go:build !windows

package agyprovider

import (
	"context"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	psLineRe   = regexp.MustCompile(`^\s*(\d+)\s+(\d+)\s+(.+)$`)
	lsofPortRe = regexp.MustCompile(`:(\d{1,5})\s+\(LISTEN\)`)
)

func defaultIsPIDAlive(pid int) bool {
	if pid <= 0 {
		return true
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func defaultListProcesses(ctx context.Context) ([]osProcess, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	formatArg := "pid=,ppid=,args="
	if runtime.GOOS == "darwin" {
		formatArg = "pid=,ppid=,command="
	}
	out, err := exec.CommandContext(cmdCtx, "ps", "-ax", "-o", formatArg).Output()
	if err != nil {
		return nil, err
	}
	return parseUnixPSOutput(string(out)), nil
}

func parseUnixPSOutput(output string) []osProcess {
	var procs []osProcess
	for _, line := range strings.Split(output, "\n") {
		m := psLineRe.FindStringSubmatch(strings.TrimSpace(line))
		if len(m) != 4 {
			continue
		}
		cmd := m[3]
		lower := strings.ToLower(cmd)
		if !strings.Contains(lower, "agy") &&
			!strings.Contains(lower, "antigravity-cli") &&
			!strings.Contains(lower, "language_server") {
			continue
		}
		pid, _ := strconv.Atoi(m[1])
		ppid, _ := strconv.Atoi(m[2])
		if pid > 0 {
			procs = append(procs, osProcess{PID: pid, PPID: ppid, CommandLine: cmd})
		}
	}
	return procs
}

func defaultListListeningPorts(ctx context.Context, pid int) ([]int, error) {
	if pid <= 0 {
		return nil, nil
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cmdCtx, "lsof", "-Pan", "-p", strconv.Itoa(pid), "-iTCP", "-sTCP:LISTEN").Output()
	if err != nil {
		return nil, err
	}
	return parseLsofPorts(string(out)), nil
}

func parseLsofPorts(output string) []int {
	seen := make(map[int]struct{})
	var ports []int
	for _, line := range strings.Split(output, "\n") {
		m := lsofPortRe.FindStringSubmatch(line)
		if len(m) != 2 {
			continue
		}
		p, err := strconv.Atoi(m[1])
		if err != nil || !validPort(p) {
			continue
		}
		if _, dup := seen[p]; !dup {
			seen[p] = struct{}{}
			ports = append(ports, p)
		}
	}
	return ports
}
