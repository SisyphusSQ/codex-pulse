//go:build darwin && cgo

package tray

import "testing"

func TestCalculatePopoverOriginClampsToCurrentDisplayInPointCoordinates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                                                                                 string
		anchorX, anchorMinY, minX, maxX, visibleHeight, primaryHeight, width, height, offset float64
		want                                                                                 PopoverOrigin
	}{
		{name: "primary right edge", anchorX: 1780, anchorMinY: 1147, minX: 0, maxX: 1800, visibleHeight: 1110, primaryHeight: 1169, width: 420, height: 760, offset: 6, want: PopoverOrigin{X: 1380, Y: 28}},
		{name: "negative x aligned secondary", anchorX: -1200, anchorMinY: 1147, minX: -1440, maxX: 0, visibleHeight: 840, primaryHeight: 1169, width: 420, height: 760, offset: 6, want: PopoverOrigin{X: -1410, Y: 28}},
		{name: "secondary above primary", anchorX: 500, anchorMinY: 2047, minX: 0, maxX: 1440, visibleHeight: 840, primaryHeight: 1169, width: 420, height: 760, offset: 6, want: PopoverOrigin{X: 290, Y: -872}},
		{name: "secondary below primary", anchorX: 500, anchorMinY: -22, minX: 0, maxX: 1440, visibleHeight: 840, primaryHeight: 1169, width: 420, height: 760, offset: 6, want: PopoverOrigin{X: 290, Y: 1197}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, valid := calculatePopoverOrigin(test.anchorX, test.anchorMinY, test.minX, test.maxX, test.visibleHeight, test.primaryHeight, test.width, test.height, test.offset)
			if !valid || got != test.want {
				t.Fatalf("origin = %#v valid=%t, want %#v", got, valid, test.want)
			}
		})
	}
}

func TestCalculatePopoverOriginFailsClosedForTooNarrowDisplay(t *testing.T) {
	t.Parallel()
	if _, valid := calculatePopoverOrigin(100, 1147, 0, 300, 800, 1169, 420, 760, 6); valid {
		t.Fatal("too-narrow display was accepted")
	}
	if _, valid := calculatePopoverOrigin(100, 1147, 0, 900, 750, 1169, 420, 760, 6); valid {
		t.Fatal("too-short display was accepted")
	}
}

func TestPlatformCallbackRegistrationIsSynchronousAndRejectsAfterClose(t *testing.T) {
	called := make([]PlatformChange, 0, 1)
	registration := &platformCallbackRegistration{callback: func(change PlatformChange) {
		called = append(called, change)
	}}
	registration.dispatch(PlatformChangeDisplay)
	registration.close()
	registration.dispatch(PlatformChangeWake)
	if len(called) != 1 || called[0] != PlatformChangeDisplay {
		t.Fatalf("callbacks = %v", called)
	}
}

func TestNativeStatusItemCloseRejectsCallbackReentry(t *testing.T) {
	item := &NativeStatusItem{platformCallbackID: 42}
	registration := &platformCallbackRegistration{callback: func(PlatformChange) {}}
	item.platformCallback = registration
	if err := item.Close(); err != nil {
		t.Fatal(err)
	}
	if !registration.closed {
		t.Fatal("platform callback registration remained open")
	}
}
