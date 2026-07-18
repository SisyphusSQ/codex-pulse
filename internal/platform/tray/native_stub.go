//go:build !darwin || !cgo

package tray

import "errors"

var ErrNativeStatusItem = errors.New("native status item is unavailable")

type NativeStatusItem struct{}
type PopoverOrigin struct {
	X int
	Y int
}

func NewNativeStatusItem() (*NativeStatusItem, error)  { return nil, ErrNativeStatusItem }
func (*NativeStatusItem) Update(StatusViewModel) error { return ErrNativeStatusItem }
func (*NativeStatusItem) Close() error                 { return nil }
func (*NativeStatusItem) CapturePNG(string) error      { return ErrNativeStatusItem }
func (*NativeStatusItem) SetClickHandler(float64, float64, func(PopoverOrigin, bool)) error {
	return ErrNativeStatusItem
}
