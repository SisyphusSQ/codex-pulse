#import <Cocoa/Cocoa.h>
#import "native_darwin.h"

extern void cpTrayHandleClick(uintptr_t callbackID, double x, double y, int valid);

static const CGFloat CPStatusWidth = 126.0;

@interface CPStatusView : NSView
@property(nonatomic, copy) NSString *state;
@property(nonatomic, copy) NSString *health;
@property(nonatomic, copy) NSString *accessibilityText;
@property(nonatomic, copy) NSArray<NSDictionary *> *rows;
@property(nonatomic, copy) void (^onPress)(void);
@end

@implementation CPStatusView

- (BOOL)isFlipped { return YES; }
- (BOOL)isAccessibilityElement { return YES; }
- (NSString *)accessibilityRole { return NSAccessibilityButtonRole; }
- (NSString *)accessibilityLabel { return self.accessibilityText ?: @"Codex Pulse 额度状态"; }
- (BOOL)accessibilityPerformPress { if (self.onPress == nil) return NO; self.onPress(); return YES; }

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
@property(nonatomic, assign) CGFloat popoverOffset;
@end
@implementation CPStatusItemHolder
- (void)handleClick:(id)sender {
    if (self.callbackID == 0) return;
    NSStatusBarButton *button = self.statusItem.button;
    NSWindow *window = button.window;
    NSScreen *screen = window.screen;
    if (window == nil || screen == nil || self.popoverWidth <= 0) {
        cpTrayHandleClick(self.callbackID, 0, 0, 0);
        return;
    }
    NSRect local = [button convertRect:button.bounds toView:nil];
    NSRect anchor = [window convertRectToScreen:local];
    CGFloat minX = NSMinX(screen.visibleFrame);
    CGFloat maxX = NSMaxX(screen.visibleFrame) - self.popoverWidth;
    CGFloat x = MIN(MAX(NSMidX(anchor) - self.popoverWidth / 2.0, minX), maxX);
    CGFloat y = NSMaxY(screen.frame) - NSMinY(anchor) + self.popoverOffset;
    cpTrayHandleClick(self.callbackID, x, y, 1);
}
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
        [button sendActionOn:NSEventMaskLeftMouseUp];
        holder.view = [[CPStatusView alloc] initWithFrame:NSMakeRect(0, 0, CPStatusWidth, NSHeight(button.bounds))];
        __weak CPStatusItemHolder *weakHolder = holder;
        holder.view.onPress = ^{ [weakHolder handleClick:nil]; };
        holder.view.autoresizingMask = NSViewHeightSizable;
        button.image = nil;
        [button addSubview:holder.view];
        button.accessibilityElement = NO;
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
        holder.view.state = stateValue;
        holder.view.health = healthValue;
        holder.view.accessibilityText = accessibilityValue;
        holder.view.rows = rows;
        [holder.view setNeedsDisplay:YES];
    });
}

void cp_tray_close(void *raw) {
    if (raw == NULL) return;
    CPStatusItemHolder *holder = (__bridge_transfer CPStatusItemHolder *)raw;
    cp_on_main_sync(^{
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

void cp_tray_set_click_handler(void *raw, uintptr_t callbackID, double width, double offset) {
    if (raw == NULL) return;
    CPStatusItemHolder *holder = (__bridge CPStatusItemHolder *)raw;
    // The Go caller may hold its item mutex. Never make that mutex wait for
    // the AppKit main queue: the block retains holder until it is applied.
    cp_on_main_async(^{
        holder.callbackID = callbackID;
        holder.popoverWidth = width;
        holder.popoverOffset = offset;
    });
}
