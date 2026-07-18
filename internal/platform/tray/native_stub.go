//go:build !darwin || !cgo

package tray

import "errors"

var ErrNativeStatusItem = errors.New("native status item is unavailable")

type NativeStatusItem struct{}

func NewNativeStatusItem() (*NativeStatusItem, error)  { return nil, ErrNativeStatusItem }
func (*NativeStatusItem) Update(StatusViewModel) error { return ErrNativeStatusItem }
func (*NativeStatusItem) Close() error                 { return nil }
func (*NativeStatusItem) CapturePNG(string) error      { return ErrNativeStatusItem }
