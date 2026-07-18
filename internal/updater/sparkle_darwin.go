//go:build darwin && cgo

package updater

/*
#cgo CFLAGS: -x objective-c -fmodules -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#cgo sparkle CFLAGS: -DCODEX_PULSE_SPARKLE=1 -F${SRCDIR}/../../.cache/sparkle/2.9.4/framework
#cgo sparkle LDFLAGS: -F${SRCDIR}/../../.cache/sparkle/2.9.4/framework -framework Sparkle
#include <stdlib.h>
#include "sparkle_darwin.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"unicode/utf8"
	"unsafe"
)

const maximumReleaseNotesBytes = 16 * 1024

const (
	nativeEventUpdateFound = iota + 1
	nativeEventNoUpdate
	nativeEventDownloadStarted
	nativeEventDownloadProgress
	nativeEventExtractionProgress
	nativeEventReadyToInstall
	nativeEventInstallStarted
	nativeEventDownloadCancelled
	nativeEventCycleFinished
	nativeEventFailed
	nativeEventCheckCancelled
	nativeEventReleaseNotes
	nativeEventInstallFailed
)

type NativeError struct {
	Code    FaultCode
	Message string
}

func (err *NativeError) Error() string {
	return fmt.Sprintf("sparkle %s: %s", err.Code, err.Message)
}

type SparkleAdapter struct {
	mu           sync.Mutex
	handle       unsafe.Pointer
	callbackID   uintptr
	registration *sparkleCallbackRegistration
	started      bool
	closed       bool
}

type sparkleCallbackRegistration struct {
	mu     sync.Mutex
	closed bool
	sink   EventSink
}

var (
	sparkleCallbacks sync.Map
	sparkleSequence  atomic.Uint64
)

func NewSparkleAdapter() *SparkleAdapter {
	return &SparkleAdapter{}
}

func nativeAbortIgnored(code int64, domain string) bool {
	rawDomain := C.CString(domain)
	defer C.free(unsafe.Pointer(rawDomain))
	return C.cp_sparkle_should_ignore_abort(C.long(code), rawDomain) != 0
}

func (adapter *SparkleAdapter) Start(sink EventSink) error {
	if sink == nil {
		return &NativeError{Code: FaultConfiguration, Message: "event sink is required"}
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return ErrClosed
	}
	if adapter.started {
		return ErrAlreadyStarted
	}
	if C.cp_sparkle_compiled() == 0 {
		return &NativeError{Code: FaultUnavailable, Message: "Sparkle support was not compiled; use the sparkle build tag"}
	}

	id := uintptr(sparkleSequence.Add(1))
	registration := &sparkleCallbackRegistration{sink: sink}
	sparkleCallbacks.Store(id, registration)
	var rawMessage *C.char
	var rawCode C.int
	handle := C.cp_sparkle_create(C.uintptr_t(id), &rawCode, &rawMessage)
	message := takeCString(rawMessage)
	if handle == nil {
		sparkleCallbacks.Delete(id)
		registration.close()
		code := nativeStartFault(int(rawCode))
		return &NativeError{Code: code, Message: message}
	}
	adapter.handle = handle
	adapter.callbackID = id
	adapter.registration = registration
	adapter.started = true
	return nil
}

func nativeStartFault(raw int) FaultCode {
	switch raw {
	case 1:
		return FaultUnavailable
	case 2:
		return FaultConfiguration
	default:
		return FaultNative
	}
}

func (adapter *SparkleAdapter) Check() error {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if err := adapter.ready(); err != nil {
		return err
	}
	var rawMessage *C.char
	if C.cp_sparkle_check(adapter.handle, &rawMessage) == 0 {
		return errors.New(takeCString(rawMessage))
	}
	return nil
}

func (adapter *SparkleAdapter) Download() error {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if err := adapter.ready(); err != nil {
		return err
	}
	var rawMessage *C.char
	if C.cp_sparkle_download(adapter.handle, &rawMessage) == 0 {
		return errors.New(takeCString(rawMessage))
	}
	return nil
}

func (adapter *SparkleAdapter) Install() error {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if err := adapter.ready(); err != nil {
		return err
	}
	var rawMessage *C.char
	if C.cp_sparkle_install(adapter.handle, &rawMessage) == 0 {
		return errors.New(takeCString(rawMessage))
	}
	return nil
}

func (adapter *SparkleAdapter) Choose(choice UpdateChoice) error {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if err := adapter.ready(); err != nil {
		return err
	}
	if choice != UpdateChoiceSkip && choice != UpdateChoiceDismiss {
		return ErrCannotChoose
	}
	var rawMessage *C.char
	if C.cp_sparkle_choose(adapter.handle, C.int(choice), &rawMessage) == 0 {
		return errors.New(takeCString(rawMessage))
	}
	return nil
}

func (adapter *SparkleAdapter) Cancel() error {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if err := adapter.ready(); err != nil {
		return err
	}
	var rawMessage *C.char
	if C.cp_sparkle_cancel(adapter.handle, &rawMessage) == 0 {
		return errors.New(takeCString(rawMessage))
	}
	return nil
}

func (adapter *SparkleAdapter) Close() error {
	adapter.mu.Lock()
	if adapter.closed {
		adapter.mu.Unlock()
		return nil
	}
	adapter.closed = true
	handle := adapter.handle
	id := adapter.callbackID
	registration := adapter.registration
	adapter.handle = nil
	adapter.callbackID = 0
	adapter.registration = nil
	adapter.started = false
	if id != 0 {
		sparkleCallbacks.Delete(id)
	}
	if registration != nil {
		registration.close()
	}
	adapter.mu.Unlock()
	if handle != nil {
		C.cp_sparkle_close(handle)
	}
	return nil
}

func (adapter *SparkleAdapter) ready() error {
	if adapter.closed {
		return ErrClosed
	}
	if !adapter.started || adapter.handle == nil {
		return ErrNotStarted
	}
	return nil
}

func (registration *sparkleCallbackRegistration) dispatch(event Event) {
	registration.mu.Lock()
	if registration.closed {
		registration.mu.Unlock()
		return
	}
	sink := registration.sink
	registration.mu.Unlock()
	sink(event)
}

func (registration *sparkleCallbackRegistration) close() {
	registration.mu.Lock()
	registration.closed = true
	registration.sink = nil
	registration.mu.Unlock()
}

//export cpSparkleHandleEvent
func cpSparkleHandleEvent(callbackID C.uintptr_t, rawKind C.int, version, displayVersion, releaseNotes *C.char, contentLength C.uint64_t, rawSignature C.int, received, total C.uint64_t, fraction C.double, rawErrorCode C.long, errorMessage *C.char) {
	value, ok := sparkleCallbacks.Load(uintptr(callbackID))
	if !ok {
		return
	}
	event := nativeEvent(
		int(rawKind),
		C.GoString(version),
		C.GoString(displayVersion),
		boundedReleaseNotes(C.GoString(releaseNotes)),
		uint64(contentLength),
		int(rawSignature),
		uint64(received),
		uint64(total),
		float64(fraction),
		int64(rawErrorCode),
		C.GoString(errorMessage),
	)
	value.(*sparkleCallbackRegistration).dispatch(event)
}

func nativeEvent(kind int, version, displayVersion, releaseNotes string, contentLength uint64, rawSignature int, received, total uint64, fraction float64, rawErrorCode int64, errorMessage string) Event {
	switch kind {
	case nativeEventUpdateFound:
		return Event{Kind: EventUpdateFound, Update: &Update{
			Version:             version,
			DisplayVersion:      displayVersion,
			ReleaseNotes:        boundedReleaseNotes(releaseNotes),
			ContentLength:       contentLength,
			Architecture:        "arm64",
			FeedSignatureStatus: signatureStatus(rawSignature),
		}}
	case nativeEventNoUpdate:
		return Event{Kind: EventNoUpdate}
	case nativeEventReleaseNotes:
		return Event{Kind: EventReleaseNotes, Update: &Update{ReleaseNotes: boundedReleaseNotes(releaseNotes)}}
	case nativeEventCheckCancelled:
		return Event{Kind: EventCheckCancelled}
	case nativeEventDownloadStarted:
		return Event{Kind: EventDownloadStarted}
	case nativeEventDownloadProgress:
		return Event{Kind: EventDownloadProgress, Received: received, Total: total}
	case nativeEventExtractionProgress:
		return Event{Kind: EventExtractionProgress, Fraction: fraction}
	case nativeEventReadyToInstall:
		return Event{Kind: EventReadyToInstall}
	case nativeEventInstallStarted:
		return Event{Kind: EventInstallStarted}
	case nativeEventInstallFailed:
		if errorMessage == "" {
			errorMessage = "Sparkle install reply is unavailable"
		}
		return Event{Kind: EventFailed, Fault: &Fault{Code: FaultInstall, Message: errorMessage}}
	case nativeEventDownloadCancelled:
		return Event{Kind: EventDownloadCancelled}
	case nativeEventCycleFinished:
		return Event{Kind: EventCycleFinished}
	default:
		if errorMessage == "" {
			errorMessage = "unknown Sparkle failure"
		}
		return Event{Kind: EventFailed, Fault: &Fault{Code: faultCodeForSparkleError(rawErrorCode), Message: errorMessage}}
	}
}

func boundedReleaseNotes(value string) string {
	if len(value) <= maximumReleaseNotesBytes {
		return value
	}
	value = value[:maximumReleaseNotesBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func signatureStatus(raw int) SignatureStatus {
	switch raw {
	case 1:
		return SignatureSucceeded
	case 2:
		return SignatureFailed
	default:
		return SignatureSkipped
	}
}

func faultCodeForSparkleError(code int64) FaultCode {
	switch {
	case code == 3001 || code == 3002:
		return FaultInvalidSignature
	case code >= 2000 && code < 3000:
		return FaultDownload
	case code >= 4000 && code < 5000:
		return FaultInstall
	case code > 0 && code < 1000:
		return FaultConfiguration
	case code >= 1000 && code < 2000:
		return FaultCheck
	default:
		return FaultNative
	}
}

func takeCString(value *C.char) string {
	if value == nil {
		return "unknown native error"
	}
	defer C.free(unsafe.Pointer(value))
	return C.GoString(value)
}
