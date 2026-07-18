//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c -fmodules -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

static void cp_probe_platform_events(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSNotificationCenter.defaultCenter postNotificationName:NSApplicationDidChangeScreenParametersNotification object:NSApp];
        [NSWorkspace.sharedWorkspace.notificationCenter postNotificationName:NSWorkspaceActiveSpaceDidChangeNotification object:NSWorkspace.sharedWorkspace];
        [NSWorkspace.sharedWorkspace.notificationCenter postNotificationName:NSWorkspaceDidWakeNotification object:NSWorkspace.sharedWorkspace];
        NSApp.appearance = [NSAppearance appearanceNamed:NSAppearanceNameDarkAqua];
        NSApp.appearance = [NSAppearance appearanceNamed:NSAppearanceNameAqua];
    });
}
*/
import "C"

func postPlatformProbeEvents() { C.cp_probe_platform_events() }
