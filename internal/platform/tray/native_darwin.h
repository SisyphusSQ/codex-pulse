#ifndef CODEX_PULSE_TRAY_NATIVE_DARWIN_H
#define CODEX_PULSE_TRAY_NATIVE_DARWIN_H
#include <stdint.h>

void *cp_tray_create(void);
void cp_tray_update(
    void *handle,
    const char *state,
    const char *health,
    const char *accessibility_label,
    int row_count,
    const char *kind0,
    const char *label0,
    const char *value0,
    double progress0,
    int known0,
    const char *kind1,
    const char *label1,
    const char *value1,
    double progress1,
    int known1
);
void cp_tray_close(void *handle);
int cp_tray_capture_png(void *handle, const char *path);
void cp_tray_set_click_handler(void *handle, uintptr_t callback_id, double width, double height, double offset);
void cp_tray_set_menu_handler(void *handle, uintptr_t callback_id);
void cp_tray_set_platform_handler(void *handle, uintptr_t callback_id);
int cp_tray_calculate_popover_origin(
    double anchor_mid_x,
    double anchor_min_y,
    double screen_min_x,
    double screen_max_x,
    double screen_visible_height,
    double primary_height,
    double popover_width,
    double popover_height,
    double offset,
    double *x,
    double *y
);

#endif
