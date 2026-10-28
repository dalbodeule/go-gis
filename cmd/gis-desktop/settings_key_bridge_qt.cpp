//go:build qt

#include "settings_key_bridge_qt.h"

#include <QSettings>
#include <QString>

extern "C" int gogis_settings_int(void *settings, const char *key, int length, bool *present) {
    auto *qtSettings = static_cast<QSettings *>(settings);
    const QString qtKey = QString::fromUtf8(key, length);
    *present = qtSettings->contains(qtKey);
    return *present ? qtSettings->value(qtKey).toInt() : 0;
}

extern "C" void gogis_settings_set_int(void *settings, const char *key, int length, int value) {
    static_cast<QSettings *>(settings)->setValue(QString::fromUtf8(key, length), value);
}

extern "C" void gogis_settings_remove(void *settings, const char *key, int length) {
    static_cast<QSettings *>(settings)->remove(QString::fromUtf8(key, length));
}
