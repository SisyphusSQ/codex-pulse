//go:build darwin && cgo

package tray

/*
#cgo CFLAGS: -x objective-c -fmodules -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
#include "native_darwin.h"
*/
import "C"

import (
	"errors"
	"sync"
	"sync/atomic"
	"unsafe"
)

var ErrNativeStatusItem = errors.New("native status item is unavailable")

type NativeStatusItem struct {
	mu             sync.Mutex
	handle         unsafe.Pointer
	closed         bool
	callbackID     uintptr
	menuCallbackID uintptr
}

type PopoverOrigin struct {
	X int
	Y int
}

var nativeClickCallbacks sync.Map
var nativeMenuCallbacks sync.Map
var nativeClickSequence atomic.Uint64

//export cpTrayHandleClick
func cpTrayHandleClick(callbackID C.uintptr_t, x C.double, y C.double, valid C.int) {
	callback, ok := nativeClickCallbacks.Load(uintptr(callbackID))
	if !ok {
		return
	}
	origin := PopoverOrigin{X: int(x), Y: int(y)}
	go callback.(func(PopoverOrigin, bool))(origin, valid != 0)
}

//export cpTrayHandleMenu
func cpTrayHandleMenu(callbackID C.uintptr_t, rawAction C.int) {
	callback, ok := nativeMenuCallbacks.Load(uintptr(callbackID))
	if !ok {
		return
	}
	actions := [...]MenuAction{
		MenuActionOpenOverview,
		MenuActionRefresh,
		MenuActionOpenSettings,
		MenuActionAbout,
		MenuActionQuit,
	}
	index := int(rawAction)
	if index < 0 || index >= len(actions) {
		return
	}
	go callback.(func(MenuAction))(actions[index])
}

func NewNativeStatusItem() (*NativeStatusItem, error) {
	handle := C.cp_tray_create()
	if handle == nil {
		return nil, ErrNativeStatusItem
	}
	return &NativeStatusItem{handle: handle}, nil
}

func (item *NativeStatusItem) Update(model StatusViewModel) error {
	if item == nil {
		return ErrNativeStatusItem
	}
	item.mu.Lock()
	defer item.mu.Unlock()
	if item.closed || item.handle == nil {
		return ErrNativeStatusItem
	}
	state := C.CString(string(model.State))
	health := C.CString(string(model.Health))
	accessibility := C.CString(model.AccessibilityLabel)
	defer C.free(unsafe.Pointer(state))
	defer C.free(unsafe.Pointer(health))
	defer C.free(unsafe.Pointer(accessibility))

	labels := [2]*C.char{C.CString(""), C.CString("")}
	values := [2]*C.char{C.CString(""), C.CString("")}
	kinds := [2]*C.char{C.CString(""), C.CString("")}
	progress := [2]C.double{}
	known := [2]C.int{}
	for index := range min(len(model.Rows), 2) {
		C.free(unsafe.Pointer(labels[index]))
		C.free(unsafe.Pointer(values[index]))
		C.free(unsafe.Pointer(kinds[index]))
		labels[index] = C.CString(model.Rows[index].Label)
		values[index] = C.CString(model.Rows[index].Value)
		kinds[index] = C.CString(string(model.Rows[index].Kind))
		progress[index] = C.double(model.Rows[index].Progress)
		if model.Rows[index].Known {
			known[index] = 1
		}
	}
	defer func() {
		for index := range 2 {
			C.free(unsafe.Pointer(labels[index]))
			C.free(unsafe.Pointer(values[index]))
			C.free(unsafe.Pointer(kinds[index]))
		}
	}()
	C.cp_tray_update(
		item.handle, state, health, accessibility, C.int(min(len(model.Rows), 2)),
		kinds[0], labels[0], values[0], progress[0], known[0],
		kinds[1], labels[1], values[1], progress[1], known[1],
	)
	return nil
}

func (item *NativeStatusItem) Close() error {
	if item == nil {
		return nil
	}
	item.mu.Lock()
	defer item.mu.Unlock()
	if item.closed {
		return nil
	}
	item.closed = true
	if item.callbackID != 0 {
		C.cp_tray_set_click_handler(item.handle, 0, 0, 0)
		nativeClickCallbacks.Delete(item.callbackID)
		item.callbackID = 0
	}
	if item.menuCallbackID != 0 {
		C.cp_tray_set_menu_handler(item.handle, 0)
		nativeMenuCallbacks.Delete(item.menuCallbackID)
		item.menuCallbackID = 0
	}
	if item.handle != nil {
		C.cp_tray_close(item.handle)
		item.handle = nil
	}
	return nil
}

func (item *NativeStatusItem) SetMenuHandler(callback func(MenuAction)) error {
	if item == nil || callback == nil {
		return ErrNativeStatusItem
	}
	item.mu.Lock()
	defer item.mu.Unlock()
	if item.closed || item.handle == nil || item.menuCallbackID != 0 {
		return ErrNativeStatusItem
	}
	id := uintptr(nativeClickSequence.Add(1))
	nativeMenuCallbacks.Store(id, callback)
	item.menuCallbackID = id
	C.cp_tray_set_menu_handler(item.handle, C.uintptr_t(id))
	return nil
}

func (item *NativeStatusItem) SetClickHandler(width, offset float64, callback func(PopoverOrigin, bool)) error {
	if item == nil || width <= 0 || offset < 0 || callback == nil {
		return ErrNativeStatusItem
	}
	item.mu.Lock()
	defer item.mu.Unlock()
	if item.closed || item.handle == nil || item.callbackID != 0 {
		return ErrNativeStatusItem
	}
	id := uintptr(nativeClickSequence.Add(1))
	nativeClickCallbacks.Store(id, callback)
	item.callbackID = id
	C.cp_tray_set_click_handler(item.handle, C.uintptr_t(id), C.double(width), C.double(offset))
	return nil
}

func (item *NativeStatusItem) CapturePNG(path string) error {
	if item == nil || path == "" {
		return ErrNativeStatusItem
	}
	item.mu.Lock()
	defer item.mu.Unlock()
	if item.closed || item.handle == nil {
		return ErrNativeStatusItem
	}
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	if C.cp_tray_capture_png(item.handle, cPath) == 0 {
		return ErrNativeStatusItem
	}
	return nil
}
