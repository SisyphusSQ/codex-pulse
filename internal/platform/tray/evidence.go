package tray

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

type Observation struct {
	Name       string `json:"name"`
	OccurredAt string `json:"occurred_at"`
	Detail     string `json:"detail,omitempty"`
}

type Rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type WindowGeometry struct {
	Window      Rect `json:"window"`
	Screen      Rect `json:"screen"`
	ScreenCount int  `json:"screen_count"`
	Primary     bool `json:"primary"`
}

type Evidence struct {
	WailsVersion string          `json:"wails_version"`
	GOOS         string          `json:"goos"`
	GOARCH       string          `json:"goarch"`
	Capabilities []Capability    `json:"capabilities"`
	Observations []Observation   `json:"observations"`
	Geometry     *WindowGeometry `json:"geometry,omitempty"`
}

type Recorder struct {
	mu       sync.Mutex
	evidence Evidence
	clock    func() time.Time
}

func NewRecorder(goos, goarch string, clock func() time.Time) (*Recorder, error) {
	if goos == "" || goarch == "" || clock == nil {
		return nil, errors.New("tray evidence recorder requires platform and clock")
	}
	capabilities := LockedCapabilities()
	if err := ValidateCapabilities(capabilities); err != nil {
		return nil, err
	}
	return &Recorder{evidence: Evidence{WailsVersion: LockedWailsVersion, GOOS: goos, GOARCH: goarch, Capabilities: capabilities}, clock: clock}, nil
}

func (recorder *Recorder) Observe(name, detail string) {
	if recorder == nil || name == "" {
		return
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.evidence.Observations = append(recorder.evidence.Observations, Observation{Name: name, OccurredAt: recorder.clock().UTC().Format(time.RFC3339Nano), Detail: detail})
}

func (recorder *Recorder) Snapshot() Evidence {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	result := recorder.evidence
	result.Capabilities = slices.Clone(result.Capabilities)
	result.Observations = slices.Clone(result.Observations)
	if result.Geometry != nil {
		geometry := *result.Geometry
		result.Geometry = &geometry
	}
	return result
}

func (recorder *Recorder) SetWindowGeometry(geometry WindowGeometry) {
	if recorder == nil {
		return
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.evidence.Geometry = &geometry
}

func (recorder *Recorder) Write(path string) error {
	if recorder == nil || path == "" || filepath.Clean(path) == "." {
		return errors.New("tray evidence path is required")
	}
	data, err := json.MarshalIndent(recorder.Snapshot(), "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tray evidence: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create tray evidence directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".tray-evidence-*")
	if err != nil {
		return fmt.Errorf("create tray evidence staging file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("chmod tray evidence staging file: %w", err)
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write tray evidence staging file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close tray evidence staging file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace tray evidence: %w", err)
	}
	return nil
}
