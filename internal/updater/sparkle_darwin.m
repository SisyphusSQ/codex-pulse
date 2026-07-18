#import <Cocoa/Cocoa.h>
#import "sparkle_darwin.h"
#include <stdlib.h>
#include <string.h>

extern void cpSparkleHandleEvent(uintptr_t callbackID, int kind, char *version, char *displayVersion,
                                 uint64_t contentLength, int signatureStatus, uint64_t received,
                                 uint64_t total, double fraction, long errorCode, char *errorMessage);

static char *cp_sparkle_copy_string(NSString *value) {
    const char *utf8 = value == nil ? "" : value.UTF8String;
    return strdup(utf8 == NULL ? "" : utf8);
}

static void cp_sparkle_set_error(char **target, NSString *message) {
    if (target != NULL) *target = cp_sparkle_copy_string(message);
}

#ifdef CODEX_PULSE_SPARKLE

#import <Sparkle/Sparkle.h>

enum {
    CPEventUpdateFound = 1,
    CPEventNoUpdate = 2,
    CPEventDownloadStarted = 3,
    CPEventDownloadProgress = 4,
    CPEventExtractionProgress = 5,
    CPEventReadyToInstall = 6,
    CPEventInstallStarted = 7,
    CPEventDownloadCancelled = 8,
    CPEventCycleFinished = 9,
    CPEventFailed = 10,
    CPEventCheckCancelled = 11,
};

static void cp_sparkle_emit(uintptr_t callbackID, int kind, SUAppcastItem *item,
                            uint64_t received, uint64_t total, double fraction, NSError *error) {
    NSString *version = item == nil ? @"" : item.versionString;
    NSString *displayVersion = item == nil ? @"" : item.displayVersionString;
    uint64_t contentLength = item == nil ? 0 : item.contentLength;
    NSInteger signatureStatus = item == nil ? 0 : item.signingValidationStatus;
    NSString *message = error == nil ? @"" : error.localizedDescription;
    cpSparkleHandleEvent(callbackID, kind, (char *)version.UTF8String, (char *)displayVersion.UTF8String,
                         contentLength, (int)signatureStatus, received, total, fraction,
                         error == nil ? 0 : error.code, (char *)message.UTF8String);
}

@interface CPUpdaterUserDriver : NSObject <SPUUserDriver>
@property(nonatomic, assign) uintptr_t callbackID;
@property(nonatomic, copy) void (^checkCancellation)(void);
@property(nonatomic, copy) void (^downloadCancellation)(void);
@property(nonatomic, copy) void (^updateChoiceReply)(SPUUserUpdateChoice);
@property(nonatomic, copy) void (^installChoiceReply)(SPUUserUpdateChoice);
@property(nonatomic, assign) uint64_t expectedLength;
@property(nonatomic, assign) uint64_t receivedLength;
- (void)clearCallbacks;
@end

@implementation CPUpdaterUserDriver
- (void)showUpdatePermissionRequest:(SPUUpdatePermissionRequest *)request reply:(void (^)(SUUpdatePermissionResponse *))reply {
    (void)request;
    reply([[SUUpdatePermissionResponse alloc] initWithAutomaticUpdateChecks:NO automaticUpdateDownloading:@NO sendSystemProfile:NO]);
}
- (void)showUserInitiatedUpdateCheckWithCancellation:(void (^)(void))cancellation {
    self.checkCancellation = cancellation;
}
- (void)showUpdateFoundWithAppcastItem:(SUAppcastItem *)appcastItem state:(SPUUserUpdateState *)state reply:(void (^)(SPUUserUpdateChoice))reply {
    (void)appcastItem;
    self.updateChoiceReply = reply;
    if (state.stage == SPUUserUpdateStageInstalling) {
        cp_sparkle_emit(self.callbackID, CPEventInstallStarted, nil, 0, 0, 0, nil);
    }
}
- (void)showUpdateReleaseNotesWithDownloadData:(SPUDownloadData *)downloadData { (void)downloadData; }
- (void)showUpdateReleaseNotesFailedToDownloadWithError:(NSError *)error { (void)error; }
- (void)showUpdateNotFoundWithError:(NSError *)error acknowledgement:(void (^)(void))acknowledgement {
    (void)error;
    acknowledgement();
}
- (void)showUpdaterError:(NSError *)error acknowledgement:(void (^)(void))acknowledgement {
    cp_sparkle_emit(self.callbackID, CPEventFailed, nil, 0, 0, 0, error);
    acknowledgement();
}
- (void)showDownloadInitiatedWithCancellation:(void (^)(void))cancellation {
    self.downloadCancellation = cancellation;
    self.expectedLength = 0;
    self.receivedLength = 0;
    cp_sparkle_emit(self.callbackID, CPEventDownloadStarted, nil, 0, 0, 0, nil);
}
- (void)showDownloadDidReceiveExpectedContentLength:(uint64_t)expectedContentLength {
    self.expectedLength = expectedContentLength;
    cp_sparkle_emit(self.callbackID, CPEventDownloadProgress, nil, self.receivedLength, self.expectedLength, 0, nil);
}
- (void)showDownloadDidReceiveDataOfLength:(uint64_t)length {
    self.receivedLength = UINT64_MAX - self.receivedLength < length ? UINT64_MAX : self.receivedLength + length;
    cp_sparkle_emit(self.callbackID, CPEventDownloadProgress, nil, self.receivedLength, self.expectedLength, 0, nil);
}
- (void)showDownloadDidStartExtractingUpdate {
    self.downloadCancellation = nil;
    cp_sparkle_emit(self.callbackID, CPEventExtractionProgress, nil, 0, 0, 0, nil);
}
- (void)showExtractionReceivedProgress:(double)progress {
    cp_sparkle_emit(self.callbackID, CPEventExtractionProgress, nil, 0, 0, progress, nil);
}
- (void)showReadyToInstallAndRelaunch:(void (^)(SPUUserUpdateChoice))reply {
    self.installChoiceReply = reply;
    cp_sparkle_emit(self.callbackID, CPEventReadyToInstall, nil, 0, 0, 0, nil);
}
- (void)showInstallingUpdateWithApplicationTerminated:(BOOL)applicationTerminated retryTerminatingApplication:(void (^)(void))retryTerminatingApplication {
    (void)applicationTerminated;
    (void)retryTerminatingApplication;
    cp_sparkle_emit(self.callbackID, CPEventInstallStarted, nil, 0, 0, 0, nil);
}
- (void)showUpdateInstalledAndRelaunched:(BOOL)relaunched acknowledgement:(void (^)(void))acknowledgement {
    (void)relaunched;
    cp_sparkle_emit(self.callbackID, CPEventCycleFinished, nil, 0, 0, 0, nil);
    acknowledgement();
}
- (void)dismissUpdateInstallation {}
- (void)showUpdateInFocus {}
- (void)clearCallbacks {
    self.checkCancellation = nil;
    self.downloadCancellation = nil;
    self.updateChoiceReply = nil;
    self.installChoiceReply = nil;
}
@end

@interface CPUpdaterDelegate : NSObject <SPUUpdaterDelegate>
@property(nonatomic, assign) uintptr_t callbackID;
@property(nonatomic, assign) BOOL ignoreNextAbort;
@end

@implementation CPUpdaterDelegate
- (void)updater:(SPUUpdater *)updater didFindValidUpdate:(SUAppcastItem *)item {
    (void)updater;
    cp_sparkle_emit(self.callbackID, CPEventUpdateFound, item, 0, 0, 0, nil);
}
- (void)updaterDidNotFindUpdate:(SPUUpdater *)updater error:(NSError *)error {
    (void)updater;
    (void)error;
    cp_sparkle_emit(self.callbackID, CPEventNoUpdate, nil, 0, 0, 0, nil);
}
- (void)userDidCancelDownload:(SPUUpdater *)updater {
    (void)updater;
    cp_sparkle_emit(self.callbackID, CPEventDownloadCancelled, nil, 0, 0, 0, nil);
}
- (void)updater:(SPUUpdater *)updater willInstallUpdate:(SUAppcastItem *)item {
    (void)updater;
    (void)item;
    cp_sparkle_emit(self.callbackID, CPEventInstallStarted, nil, 0, 0, 0, nil);
}
- (void)updater:(SPUUpdater *)updater didAbortWithError:(NSError *)error {
    (void)updater;
    if (self.ignoreNextAbort) {
        self.ignoreNextAbort = NO;
        return;
    }
    // Sparkle reports SUNoUpdateError after updaterDidNotFindUpdate, so emitting
    // a second terminal event would overwrite the successful no-update result.
    // SUInstallationCanceledError remains a typed installation failure; a
    // duplicate EventFailed is state-machine idempotent and retains that truth.
    if (cp_sparkle_should_ignore_abort(error.code, error.domain.UTF8String)) return;
    cp_sparkle_emit(self.callbackID, CPEventFailed, nil, 0, 0, 0, error);
}
@end

@interface CPUpdaterHolder : NSObject
@property(nonatomic, strong) CPUpdaterUserDriver *userDriver;
@property(nonatomic, strong) CPUpdaterDelegate *delegate;
@property(nonatomic, strong) SPUUpdater *updater;
@end
@implementation CPUpdaterHolder
@end

static void cp_on_main_sync(dispatch_block_t block) {
    if (NSThread.isMainThread) block();
    else dispatch_sync(dispatch_get_main_queue(), block);
}

int cp_sparkle_compiled(void) {
    // Keep a strong reference to an exported Sparkle symbol. Objective-C class
    // messages alone can otherwise be dead-stripped by Go's external linker,
    // leaving a bridge that compiles but has no Sparkle LC_LOAD_DYLIB entry.
    return SUSparkleErrorDomain.length > 0 ? 1 : 0;
}

int cp_sparkle_should_ignore_abort(long errorCode, const char *errorDomain) {
    const char *sparkleDomain = SUSparkleErrorDomain.UTF8String;
    return errorCode == SUNoUpdateError && errorDomain != NULL && sparkleDomain != NULL &&
           strcmp(errorDomain, sparkleDomain) == 0;
}

void *cp_sparkle_create(uintptr_t callbackID, int *errorCode, char **errorMessage) {
    __block CPUpdaterHolder *holder = nil;
    __block NSError *startError = nil;
    cp_on_main_sync(^{
        NSBundle *bundle = NSBundle.mainBundle;
        NSString *feedURL = [bundle objectForInfoDictionaryKey:@"SUFeedURL"];
        NSString *publicKey = [bundle objectForInfoDictionaryKey:@"SUPublicEDKey"];
        if (feedURL.length == 0 || publicKey.length == 0) {
            startError = [NSError errorWithDomain:@"CodexPulseSparkle" code:2 userInfo:@{NSLocalizedDescriptionKey: @"SUFeedURL and SUPublicEDKey are required"}];
            return;
        }
        holder = [[CPUpdaterHolder alloc] init];
        holder.userDriver = [[CPUpdaterUserDriver alloc] init];
        holder.userDriver.callbackID = callbackID;
        holder.delegate = [[CPUpdaterDelegate alloc] init];
        holder.delegate.callbackID = callbackID;
        holder.updater = [[SPUUpdater alloc] initWithHostBundle:bundle applicationBundle:bundle userDriver:holder.userDriver delegate:holder.delegate];
        if (![holder.updater startUpdater:&startError]) holder = nil;
    });
    if (holder == nil) {
        if (errorCode != NULL) *errorCode = 2;
        cp_sparkle_set_error(errorMessage, startError.localizedDescription ?: @"Sparkle failed to start");
        return NULL;
    }
    return (__bridge_retained void *)holder;
}

int cp_sparkle_check(void *raw, char **errorMessage) {
    if (raw == NULL) {
        cp_sparkle_set_error(errorMessage, @"Sparkle adapter is not started");
        return 0;
    }
    CPUpdaterHolder *holder = (__bridge CPUpdaterHolder *)raw;
    cp_on_main_sync(^{
        holder.delegate.ignoreNextAbort = NO;
        [holder.updater checkForUpdateInformation];
    });
    return 1;
}

int cp_sparkle_download(void *raw, char **errorMessage) {
    if (raw == NULL) {
        cp_sparkle_set_error(errorMessage, @"Sparkle adapter is not started");
        return 0;
    }
    CPUpdaterHolder *holder = (__bridge CPUpdaterHolder *)raw;
    __block void (^reply)(SPUUserUpdateChoice) = nil;
    cp_on_main_sync(^{
        reply = holder.userDriver.updateChoiceReply;
        holder.userDriver.updateChoiceReply = nil;
        if (reply != nil) reply(SPUUserUpdateChoiceInstall);
    });
    if (reply == nil) {
        cp_sparkle_set_error(errorMessage, @"No Sparkle update is awaiting download confirmation");
        return 0;
    }
    return 1;
}

int cp_sparkle_cancel(void *raw, char **errorMessage) {
    if (raw == NULL) {
        cp_sparkle_set_error(errorMessage, @"Sparkle adapter is not started");
        return 0;
    }
    CPUpdaterHolder *holder = (__bridge CPUpdaterHolder *)raw;
    __block void (^cancellation)(void) = nil;
    __block BOOL cancelledCheck = NO;
    cp_on_main_sync(^{
        if (holder.userDriver.downloadCancellation != nil) {
            cancellation = holder.userDriver.downloadCancellation;
            holder.userDriver.downloadCancellation = nil;
        } else {
            cancellation = holder.userDriver.checkCancellation;
            holder.userDriver.checkCancellation = nil;
            if (cancellation != nil) {
                holder.delegate.ignoreNextAbort = YES;
                cancelledCheck = YES;
            }
        }
        if (cancellation != nil) cancellation();
        if (cancelledCheck) {
            cp_sparkle_emit(holder.delegate.callbackID, CPEventCheckCancelled, nil, 0, 0, 0, nil);
        }
    });
    if (cancellation == nil) {
        cp_sparkle_set_error(errorMessage, @"No cancellable Sparkle operation is active");
        return 0;
    }
    return 1;
}

void cp_sparkle_close(void *raw) {
    if (raw == NULL) return;
    CPUpdaterHolder *holder = (__bridge_transfer CPUpdaterHolder *)raw;
    cp_on_main_sync(^{
        [holder.userDriver clearCallbacks];
        holder.updater = nil;
        holder.delegate = nil;
        holder.userDriver = nil;
    });
}

#else

int cp_sparkle_compiled(void) { return 0; }
int cp_sparkle_should_ignore_abort(long errorCode, const char *errorDomain) {
    (void)errorCode;
    (void)errorDomain;
    return 0;
}
void *cp_sparkle_create(uintptr_t callbackID, int *errorCode, char **errorMessage) {
    (void)callbackID;
    if (errorCode != NULL) *errorCode = 1;
    cp_sparkle_set_error(errorMessage, @"Sparkle support was not compiled");
    return NULL;
}
int cp_sparkle_check(void *handle, char **errorMessage) {
    (void)handle;
    cp_sparkle_set_error(errorMessage, @"Sparkle support was not compiled");
    return 0;
}
int cp_sparkle_download(void *handle, char **errorMessage) {
    (void)handle;
    cp_sparkle_set_error(errorMessage, @"Sparkle support was not compiled");
    return 0;
}
int cp_sparkle_cancel(void *handle, char **errorMessage) {
    (void)handle;
    cp_sparkle_set_error(errorMessage, @"Sparkle support was not compiled");
    return 0;
}
void cp_sparkle_close(void *handle) { (void)handle; }

#endif
