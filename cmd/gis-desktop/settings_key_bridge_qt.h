#pragma once

#include <stdbool.h>

#ifdef __cplusplus
extern "C" {
#endif

int gogis_settings_int(void *settings, const char *key, int length, bool *present);
void gogis_settings_set_int(void *settings, const char *key, int length, int value);
void gogis_settings_remove(void *settings, const char *key, int length);

#ifdef __cplusplus
}
#endif
