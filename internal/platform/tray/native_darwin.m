#import <Cocoa/Cocoa.h>
#import "native_darwin.h"

extern void cpTrayHandleClick(uintptr_t callbackID, double x, double y, int valid);
extern void cpTrayHandleMenu(uintptr_t callbackID, int action);
extern void cpTrayHandlePlatform(uintptr_t callbackID, int change);

static const CGFloat CPStatusWidth = 126.0;

int cp_tray_calculate_popover_origin(double anchorMidX, double anchorMinY,
                                     double screenMinX, double screenMaxX, double screenVisibleHeight,
                                     double primaryHeight, double popoverWidth, double popoverHeight,
                                     double offset, double *x, double *y) {
    if (x == NULL || y == NULL || popoverWidth <= 0 || popoverHeight <= 0 || primaryHeight <= 0 ||
        screenMaxX - screenMinX < popoverWidth || screenVisibleHeight < popoverHeight + offset) return 0;
    *x = MIN(MAX(anchorMidX - popoverWidth / 2.0, screenMinX), screenMaxX - popoverWidth);
    // Wails SetPosition uses global Y-down coordinates relative to the top of
    // the first (primary) NSScreen, while AppKit returns global Cocoa Y-up.
    *y = primaryHeight - anchorMinY + offset;
    return 1;
}

@interface CPStatusView : NSView
@property(nonatomic, copy) NSString *state;
@property(nonatomic, copy) NSString *health;
@property(nonatomic, copy) NSString *accessibilityText;
@property(nonatomic, copy) NSArray<NSDictionary *> *rows;
@property(nonatomic, copy) void (^onPress)(void);
@property(nonatomic, copy) void (^onShowMenu)(void);
@property(nonatomic, copy) void (^onAppearanceChange)(void);
@end

@implementation CPStatusView

- (BOOL)isFlipped { return YES; }
- (void)rightMouseDown:(NSEvent *)event { if (self.onShowMenu != nil) self.onShowMenu(); }
- (BOOL)isAccessibilityElement { return NO; }
- (void)viewDidChangeEffectiveAppearance {
    [super viewDidChangeEffectiveAppearance];
    [self setNeedsDisplay:YES];
    if (self.onAppearanceChange != nil) self.onAppearanceChange();
}

- (NSColor *)barColorForKind:(NSString *)kind progress:(CGFloat)progress {
    if ([self.state isEqualToString:@"conflict"]) return NSColor.systemOrangeColor;
    if ([self.state isEqualToString:@"stale"] || [self.state isEqualToString:@"unavailable"]) {
        return NSColor.systemGrayColor;
    }
    if ([self.state isEqualToString:@"exhausted"] && progress == 0) return NSColor.systemRedColor;
    return [kind isEqualToString:@"secondary"] ? NSColor.systemPurpleColor : NSColor.systemBlueColor;
}

- (void)drawRect:(NSRect)dirtyRect {
    [super drawRect:dirtyRect];
    CGFloat height = NSHeight(self.bounds);
    NSUInteger count = MIN(self.rows.count, 2);
    [NSColor.labelColor setStroke];
    NSBezierPath *glyph = [NSBezierPath bezierPath];
    glyph.lineWidth = 1.4;
    glyph.lineCapStyle = NSLineCapStyleRound;
    glyph.lineJoinStyle = NSLineJoinStyleRound;
    CGFloat centerY = height / 2.0;
    [glyph moveToPoint:NSMakePoint(3, centerY - 4)];
    [glyph lineToPoint:NSMakePoint(8, centerY)];
    [glyph lineToPoint:NSMakePoint(3, centerY + 4)];
    [glyph moveToPoint:NSMakePoint(9, centerY + 3)];
    [glyph lineToPoint:NSMakePoint(13, centerY + 3)];
    [glyph stroke];
    if ([self.health isEqualToString:@"blocked"] || [self.health isEqualToString:@"degraded"]) {
        NSColor *color = [self.health isEqualToString:@"blocked"] ? NSColor.systemRedColor : NSColor.systemOrangeColor;
        [color setFill];
        [[NSBezierPath bezierPathWithOvalInRect:NSMakeRect(121, (height - 4) / 2.0, 4, 4)] fill];
    }
    if (count == 0) return;

    NSDictionary *labelAttrs = @{
        NSFontAttributeName: [NSFont systemFontOfSize:7.0 weight:NSFontWeightMedium],
        NSForegroundColorAttributeName: NSColor.labelColor
    };
    NSDictionary *valueAttrs = @{
        NSFontAttributeName: [NSFont monospacedDigitSystemFontOfSize:7.0 weight:NSFontWeightSemibold],
        NSForegroundColorAttributeName: NSColor.labelColor
    };
    CGFloat rowHeight = count == 1 ? height : height / 2.0;
    for (NSUInteger index = 0; index < count; index++) {
        NSDictionary *row = self.rows[index];
        CGFloat y = index * rowHeight + MAX(0, (rowHeight - 8.0) / 2.0);
        [row[@"label"] drawInRect:NSMakeRect(16, y, 22, 9) withAttributes:labelAttrs];
        NSRect track = NSMakeRect(39, y + 3, 54, 3);
        [NSColor.quaternaryLabelColor setFill];
        [[NSBezierPath bezierPathWithRoundedRect:track xRadius:1.5 yRadius:1.5] fill];
        BOOL known = [row[@"known"] boolValue];
        CGFloat progress = known ? [row[@"progress"] doubleValue] : 0;
        if (known) {
            NSRect fill = track;
            fill.size.width = MAX(progress > 0 ? 1.0 : 0, track.size.width * MIN(MAX(progress, 0), 1));
            [[self barColorForKind:row[@"kind"] progress:progress] setFill];
            [[NSBezierPath bezierPathWithRoundedRect:fill xRadius:1.5 yRadius:1.5] fill];
        }
        [row[@"value"] drawInRect:NSMakeRect(96, y, 24, 9) withAttributes:valueAttrs];
    }
}
@end

@interface CPStatusItemHolder : NSObject
@property(nonatomic, strong) NSStatusItem *statusItem;
@property(nonatomic, strong) CPStatusView *view;
@property(nonatomic, assign) uintptr_t callbackID;
@property(nonatomic, assign) CGFloat popoverWidth;
@property(nonatomic, assign) CGFloat popoverHeight;
@property(nonatomic, assign) CGFloat popoverOffset;
@property(nonatomic, strong) NSMenu *menu;
@property(nonatomic, assign) uintptr_t menuCallbackID;
@property(nonatomic, assign) uintptr_t platformCallbackID;
@property(nonatomic, assign) NSRect lastAccessibilityFrame;
@property(nonatomic, copy) NSArray *applicationObserverTokens;
@property(nonatomic, copy) NSArray *workspaceObserverTokens;
- (void)showMenu;
- (void)handlePlatformChange:(int)change;
- (void)removePlatformObservers;
@end
@implementation CPStatusItemHolder
- (void)handleClick:(id)sender {
    NSEvent *event = NSApp.currentEvent;
    if (event.type == NSEventTypeRightMouseUp && self.menu != nil && self.menuCallbackID != 0) {
        [self showMenu];
        return;
    }
    if (self.callbackID == 0) return;
    NSStatusBarButton *button = self.statusItem.button;
    NSWindow *window = button.window;
    NSScreen *screen = window.screen;
    NSScreen *primaryScreen = NSScreen.screens.firstObject;
    if (window == nil || screen == nil || primaryScreen == nil || self.popoverWidth <= 0 || self.popoverHeight <= 0) {
        cpTrayHandleClick(self.callbackID, 0, 0, 0);
        return;
    }
    NSRect local = [button convertRect:button.bounds toView:nil];
    NSRect anchor = [window convertRectToScreen:local];
    double x = 0;
    double y = 0;
    int valid = cp_tray_calculate_popover_origin(
        NSMidX(anchor), NSMinY(anchor), NSMinX(screen.visibleFrame), NSMaxX(screen.visibleFrame),
        NSHeight(screen.visibleFrame), NSHeight(primaryScreen.frame), self.popoverWidth,
        self.popoverHeight, self.popoverOffset, &x, &y
    );
    if (valid == 0) {
        cpTrayHandleClick(self.callbackID, 0, 0, 0);
        return;
    }
    cpTrayHandleClick(self.callbackID, x, y, 1);
}
- (void)showMenu {
    if (self.menu == nil || self.menuCallbackID == 0) return;
    [self.menu popUpMenuPositioningItem:nil atLocation:NSMakePoint(0, NSHeight(self.statusItem.button.bounds)) inView:self.statusItem.button];
}
- (void)handlePlatformChange:(int)change {
    if (self.platformCallbackID == 0) return;
    NSStatusBarButton *button = self.statusItem.button;
    if (button != nil && self.view != nil) {
        self.view.frame = NSMakeRect(0, 0, CPStatusWidth, NSHeight(button.bounds));
        [self.view setNeedsDisplay:YES];
        NSRect accessibilityFrame = button.accessibilityFrame;
        if (!NSEqualRects(accessibilityFrame, self.lastAccessibilityFrame)) {
            self.lastAccessibilityFrame = accessibilityFrame;
            NSAccessibilityPostNotification(button, NSAccessibilityLayoutChangedNotification);
        }
    }
    cpTrayHandlePlatform(self.platformCallbackID, change);
}
- (void)removePlatformObservers {
    NSNotificationCenter *applicationCenter = NSNotificationCenter.defaultCenter;
    for (id token in self.applicationObserverTokens) [applicationCenter removeObserver:token];
    NSNotificationCenter *workspaceCenter = NSWorkspace.sharedWorkspace.notificationCenter;
    for (id token in self.workspaceObserverTokens) [workspaceCenter removeObserver:token];
    self.applicationObserverTokens = @[];
    self.workspaceObserverTokens = @[];
}
- (void)handleOpenOverview:(id)sender { if (self.menuCallbackID != 0) cpTrayHandleMenu(self.menuCallbackID, 0); }
- (void)handleRefresh:(id)sender { if (self.menuCallbackID != 0) cpTrayHandleMenu(self.menuCallbackID, 1); }
- (void)handleOpenSettings:(id)sender { if (self.menuCallbackID != 0) cpTrayHandleMenu(self.menuCallbackID, 2); }
- (void)handleAbout:(id)sender { if (self.menuCallbackID != 0) cpTrayHandleMenu(self.menuCallbackID, 3); }
- (void)handleQuit:(id)sender { if (self.menuCallbackID != 0) cpTrayHandleMenu(self.menuCallbackID, 4); }
@end

static void cp_on_main_sync(dispatch_block_t block) {
    if ([NSThread isMainThread]) block();
    else dispatch_sync(dispatch_get_main_queue(), block);
}

static void cp_on_main_async(dispatch_block_t block) {
    if ([NSThread isMainThread]) block();
    else dispatch_async(dispatch_get_main_queue(), block);
}

void *cp_tray_create(void) {
    __block CPStatusItemHolder *holder = nil;
    cp_on_main_sync(^{
        holder = [[CPStatusItemHolder alloc] init];
        holder.statusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:CPStatusWidth];
        NSStatusBarButton *button = holder.statusItem.button;
        if (button == nil) { holder = nil; return; }
        button.target = holder;
        button.action = @selector(handleClick:);
        [button sendActionOn:NSEventMaskLeftMouseUp | NSEventMaskRightMouseUp];
        holder.view = [[CPStatusView alloc] initWithFrame:NSMakeRect(0, 0, CPStatusWidth, NSHeight(button.bounds))];
        __weak CPStatusItemHolder *weakHolder = holder;
        holder.view.onPress = ^{ [weakHolder handleClick:nil]; };
        holder.view.onShowMenu = ^{ [weakHolder showMenu]; };
        holder.view.onAppearanceChange = ^{ [weakHolder handlePlatformChange:3]; };
        holder.view.autoresizingMask = NSViewHeightSizable;
        button.image = nil;
        button.accessibilityLabel = @"Codex Pulse 额度状态";
        button.accessibilityTitle = @"Codex Pulse 额度状态";
        button.accessibilityHelp = @"左键打开额度概览，右键打开应用菜单";
        [button addSubview:holder.view];
        holder.lastAccessibilityFrame = button.accessibilityFrame;
    });
    return holder == nil ? NULL : (__bridge_retained void *)holder;
}

static NSString *cp_string(const char *value) {
    return value == NULL ? @"" : [NSString stringWithUTF8String:value];
}

void cp_tray_update(void *raw, const char *state, const char *health, const char *accessibility,
                    int rowCount, const char *kind0, const char *label0, const char *value0, double progress0, int known0,
                    const char *kind1, const char *label1, const char *value1, double progress1, int known1) {
    if (raw == NULL) return;
    CPStatusItemHolder *holder = (__bridge CPStatusItemHolder *)raw;
    NSString *stateValue = cp_string(state);
    NSString *healthValue = cp_string(health);
    NSString *accessibilityValue = cp_string(accessibility);
    NSMutableArray *rows = [NSMutableArray arrayWithCapacity:2];
    if (rowCount > 0) [rows addObject:@{@"kind": cp_string(kind0), @"label": cp_string(label0), @"value": cp_string(value0), @"progress": @(progress0), @"known": @(known0)}];
    if (rowCount > 1) [rows addObject:@{@"kind": cp_string(kind1), @"label": cp_string(label1), @"value": cp_string(value1), @"progress": @(progress1), @"known": @(known1)}];
    cp_on_main_async(^{
        BOOL accessibilityChanged = ![holder.view.accessibilityText isEqualToString:accessibilityValue];
        holder.view.state = stateValue;
        holder.view.health = healthValue;
        holder.view.accessibilityText = accessibilityValue;
        holder.view.rows = rows;
        [holder.view setNeedsDisplay:YES];
        if (accessibilityChanged) {
            holder.statusItem.button.accessibilityLabel = accessibilityValue;
            holder.statusItem.button.accessibilityTitle = accessibilityValue;
            NSAccessibilityPostNotification(holder.statusItem.button, NSAccessibilityTitleChangedNotification);
            NSAccessibilityPostNotificationWithUserInfo(
                NSApp,
                NSAccessibilityAnnouncementRequestedNotification,
                @{
                    NSAccessibilityAnnouncementKey: accessibilityValue,
                    NSAccessibilityPriorityKey: @(NSAccessibilityPriorityMedium),
                }
            );
        }
    });
}

void cp_tray_close(void *raw) {
    if (raw == NULL) return;
    CPStatusItemHolder *holder = (__bridge_transfer CPStatusItemHolder *)raw;
    cp_on_main_sync(^{
        [holder removePlatformObservers];
        holder.platformCallbackID = 0;
        holder.view.onAppearanceChange = nil;
        holder.view.onPress = nil;
        holder.view.onShowMenu = nil;
        [[NSStatusBar systemStatusBar] removeStatusItem:holder.statusItem];
        holder.view = nil;
        holder.statusItem = nil;
    });
}

int cp_tray_capture_png(void *raw, const char *rawPath) {
    if (raw == NULL || rawPath == NULL) return 0;
    CPStatusItemHolder *holder = (__bridge CPStatusItemHolder *)raw;
    __block BOOL success = NO;
    NSString *path = cp_string(rawPath);
    cp_on_main_sync(^{
        NSRect bounds = holder.view.bounds;
        NSBitmapImageRep *bitmap = [holder.view bitmapImageRepForCachingDisplayInRect:bounds];
        if (bitmap == nil) return;
        [holder.view cacheDisplayInRect:bounds toBitmapImageRep:bitmap];
        NSData *png = [bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
        success = png != nil && [png writeToFile:path options:NSDataWritingAtomic error:nil];
    });
    return success ? 1 : 0;
}

void cp_tray_set_click_handler(void *raw, uintptr_t callbackID, double width, double height, double offset) {
    if (raw == NULL) return;
    CPStatusItemHolder *holder = (__bridge CPStatusItemHolder *)raw;
    // The Go caller may hold its item mutex. Never make that mutex wait for
    // the AppKit main queue: the block retains holder until it is applied.
    cp_on_main_async(^{
        holder.callbackID = callbackID;
        holder.popoverWidth = width;
        holder.popoverHeight = height;
        holder.popoverOffset = offset;
    });
}

void cp_tray_set_menu_handler(void *raw, uintptr_t callbackID) {
    if (raw == NULL) return;
    CPStatusItemHolder *holder = (__bridge CPStatusItemHolder *)raw;
    cp_on_main_async(^{
        holder.menuCallbackID = callbackID;
        if (callbackID == 0) {
            holder.menu = nil;
            return;
        }
        NSMenu *menu = [[NSMenu alloc] initWithTitle:@"Codex Pulse"];
        NSArray<NSDictionary *> *items = @[
            @{@"title": @"打开概览", @"selector": NSStringFromSelector(@selector(handleOpenOverview:))},
            @{@"title": @"刷新", @"selector": NSStringFromSelector(@selector(handleRefresh:))},
            @{@"title": @"设置…", @"selector": NSStringFromSelector(@selector(handleOpenSettings:))},
            @{@"title": @"关于 Codex Pulse", @"selector": NSStringFromSelector(@selector(handleAbout:))},
            @{@"title": @"退出 Codex Pulse", @"selector": NSStringFromSelector(@selector(handleQuit:))},
        ];
        for (NSUInteger index = 0; index < items.count; index++) {
            if (index == 2 || index == 4) [menu addItem:NSMenuItem.separatorItem];
            NSDictionary *definition = items[index];
            SEL selector = NSSelectorFromString(definition[@"selector"]);
            NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:definition[@"title"] action:selector keyEquivalent:@""];
            item.target = holder;
            [menu addItem:item];
        }
        holder.menu = menu;
    });
}

void cp_tray_set_platform_handler(void *raw, uintptr_t callbackID) {
    if (raw == NULL) return;
    CPStatusItemHolder *holder = (__bridge CPStatusItemHolder *)raw;
    cp_on_main_async(^{
        [holder removePlatformObservers];
        holder.platformCallbackID = callbackID;
        if (callbackID == 0) return;
        __weak CPStatusItemHolder *weakHolder = holder;
        NSNotificationCenter *applicationCenter = NSNotificationCenter.defaultCenter;
        id displayToken = [applicationCenter addObserverForName:NSApplicationDidChangeScreenParametersNotification object:nil queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *note) {
            [weakHolder handlePlatformChange:0];
        }];
        NSNotificationCenter *workspaceCenter = NSWorkspace.sharedWorkspace.notificationCenter;
        id spaceToken = [workspaceCenter addObserverForName:NSWorkspaceActiveSpaceDidChangeNotification object:nil queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *note) {
            [weakHolder handlePlatformChange:1];
        }];
        id wakeToken = [workspaceCenter addObserverForName:NSWorkspaceDidWakeNotification object:nil queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *note) {
            [weakHolder handlePlatformChange:2];
        }];
        holder.applicationObserverTokens = @[displayToken];
        holder.workspaceObserverTokens = @[spaceToken, wakeToken];
    });
}
