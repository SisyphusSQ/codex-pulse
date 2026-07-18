#ifndef CODEX_PULSE_TRAY_NATIVE_DARWIN_H
#define CODEX_PULSE_TRAY_NATIVE_DARWIN_H

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

#endif
