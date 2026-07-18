#ifndef CODEX_PULSE_SPARKLE_DARWIN_H
#define CODEX_PULSE_SPARKLE_DARWIN_H

#include <stdint.h>

int cp_sparkle_compiled(void);
int cp_sparkle_should_ignore_abort(long error_code, const char *error_domain);
void *cp_sparkle_create(uintptr_t callback_id, int *error_code, char **error_message);
int cp_sparkle_check(void *handle, char **error_message);
int cp_sparkle_download(void *handle, char **error_message);
int cp_sparkle_cancel(void *handle, char **error_message);
void cp_sparkle_close(void *handle);

#endif
