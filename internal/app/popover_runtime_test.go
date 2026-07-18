package app

import (
	"errors"
	"testing"

	platformtray "github.com/SisyphusSQ/codex-pulse/internal/platform/tray"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type popoverWindowStub struct {
	visible                bool
	shown, hidden, focused int
	x, y                   int
}

func (stub *popoverWindowStub) IsVisible() bool { return stub.visible }
func (stub *popoverWindowStub) Hide() application.Window {
	stub.visible = false
	stub.hidden++
	return nil
}
func (stub *popoverWindowStub) Show() application.Window {
	stub.visible = true
	stub.shown++
	return nil
}
func (stub *popoverWindowStub) Focus()               { stub.focused++ }
func (stub *popoverWindowStub) SetPosition(x, y int) { stub.x, stub.y = x, y }

type popoverStatusItemStub struct {
	callback func(platformtray.PopoverOrigin, bool)
	origin   platformtray.PopoverOrigin
	width    float64
	offset   float64
	err      error
}

func (stub *popoverStatusItemStub) SetClickHandler(width float64, offset float64, callback func(platformtray.PopoverOrigin, bool)) error {
	stub.callback = callback
	stub.width = width
	stub.offset = offset
	return stub.err
}

func TestPopoverControllerTogglesAtStatusItemAnchor(t *testing.T) {
	t.Parallel()
	window := &popoverWindowStub{}
	controller, err := newPopoverController(window)
	if err != nil {
		t.Fatal(err)
	}
	item := &popoverStatusItemStub{origin: platformtray.PopoverOrigin{X: 900, Y: 31}}
	if err := controller.configureStatusItem(item); err != nil {
		t.Fatal(err)
	}
	if item.width != popoverWidth || item.offset != popoverOffset {
		t.Fatalf("anchor contract = %.0fx%.0f", item.width, item.offset)
	}
	item.callback(item.origin, true)
	if !window.visible || window.shown != 1 || window.focused != 1 || window.x != 900 || window.y != 31 {
		t.Fatalf("show state = %#v", window)
	}
	item.callback(item.origin, true)
	if window.visible || window.hidden != 1 || window.shown != 1 {
		t.Fatalf("hide state = %#v", window)
	}
}

func TestPopoverControllerUsesSafePositionFallback(t *testing.T) {
	t.Parallel()
	window := &popoverWindowStub{x: 50, y: 50}
	controller, _ := newPopoverController(window)
	controller.Toggle(platformtray.PopoverOrigin{}, false)
	if window.x != 50 || window.y != 50 || !window.visible {
		t.Fatalf("fallback state = %#v", window)
	}
}

func TestPopoverControllerValidatesDependencies(t *testing.T) {
	t.Parallel()
	if _, err := newPopoverController(nil); !errors.Is(err, ErrPopoverRuntime) {
		t.Fatalf("error = %v", err)
	}
	controller, _ := newPopoverController(&popoverWindowStub{})
	if err := controller.configureStatusItem(nil); !errors.Is(err, ErrPopoverRuntime) {
		t.Fatalf("error = %v", err)
	}
}
