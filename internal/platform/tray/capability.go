package tray

import (
	"errors"
	"fmt"
	"slices"
)

const LockedWailsVersion = "v3.0.0-alpha2.117"

type Status string

const (
	StatusSupported       Status = "supported"
	StatusAdapterRequired Status = "adapter-required"
)

type Capability struct {
	ID       string `json:"id"`
	Status   Status `json:"status"`
	API      string `json:"api"`
	Evidence string `json:"evidence"`
	Fallback string `json:"fallback"`
}

// LockedCapabilities is the reviewed platform boundary for macOS 15+ arm64.
// It intentionally distinguishes public Wails APIs from implementation-only
// AppKit details so production code never depends on a private native handle.
func LockedCapabilities() []Capability {
	return []Capability{
		{ID: "template-icon", Status: StatusSupported, API: "(*application.SystemTray).SetTemplateIcon([]byte)", Evidence: "public API; darwin implementation marks NSImage as template", Fallback: "none"},
		{ID: "left-click", Status: StatusSupported, API: "(*application.SystemTray).OnClick(func())", Evidence: "public callback routed by the darwin status-item controller", Fallback: "menu-only interaction"},
		{ID: "right-click-menu", Status: StatusSupported, API: "(*application.SystemTray).OnRightClick(func()) + OpenMenu()", Evidence: "public callback and native menu tracking are available", Fallback: "single-click menu"},
		{ID: "attached-window", Status: StatusSupported, API: "AttachWindow(Window).WindowOffset(int)", Evidence: "public Popover-like window toggle and native positioning", Fallback: "open the regular main window"},
		{ID: "window-activation", Status: StatusSupported, API: "Show().Focus() + ActivationPolicyAccessory", Evidence: "public window lifecycle and macOS accessory policy", Fallback: "activate regular main window"},
		{ID: "multi-display-anchor", Status: StatusAdapterRequired, API: "PositionWindow(Window, int) + Window geometry readback", Evidence: "single-screen horizontal centering works; Retina offset and screen identity require adapter validation", Fallback: "zero offset or center the regular main window"},
		{ID: "native-status-item-custom-view", Status: StatusAdapterRequired, API: "none", Evidence: "Wails keeps NSStatusItem and bounds/screen readback private", Fallback: "isolated AppKit adapter or pre-rendered template image"},
		{ID: "native-nspopover", Status: StatusAdapterRequired, API: "none", Evidence: "AttachWindow provides a Wails WebView window, not NSPopover", Fallback: "frozen frameless attached window"},
	}
}

var ErrCapabilityContract = errors.New("tray capability contract is invalid")

func ValidateCapabilities(capabilities []Capability) error {
	if len(capabilities) == 0 {
		return fmt.Errorf("%w: empty matrix", ErrCapabilityContract)
	}
	seen := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if capability.ID == "" || capability.API == "" || capability.Evidence == "" || capability.Fallback == "" {
			return fmt.Errorf("%w: incomplete capability %q", ErrCapabilityContract, capability.ID)
		}
		if capability.Status != StatusSupported && capability.Status != StatusAdapterRequired {
			return fmt.Errorf("%w: unsupported status %q", ErrCapabilityContract, capability.Status)
		}
		if _, ok := seen[capability.ID]; ok {
			return fmt.Errorf("%w: duplicate capability %q", ErrCapabilityContract, capability.ID)
		}
		seen[capability.ID] = struct{}{}
	}
	for _, required := range []string{"template-icon", "left-click", "right-click-menu", "attached-window", "window-activation", "multi-display-anchor", "native-status-item-custom-view", "native-nspopover"} {
		if !slices.ContainsFunc(capabilities, func(capability Capability) bool { return capability.ID == required }) {
			return fmt.Errorf("%w: missing capability %q", ErrCapabilityContract, required)
		}
	}
	return nil
}
