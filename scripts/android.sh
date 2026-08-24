#!/usr/bin/env bash
# Сборка приложения для Android.
#
# Шагов два: ядро на Go превращается в библиотеку, приложение на Kotlin
# собирается вместе с ней. Первый шаг долгий и нужен только когда менялся Go,
# поэтому его можно пропустить: scripts/android.sh --skip-core
set -euo pipefail

cd "$(dirname "$0")/.."

: "${ANDROID_HOME:?не задан ANDROID_HOME — путь к Android SDK}"

# NDK берём тот, что стоит, если не указали явно.
if [ -z "${ANDROID_NDK_HOME:-}" ]; then
    found="$(ls -d "$ANDROID_HOME"/ndk/* 2>/dev/null | sort -V | tail -1 || true)"
    if [ -z "$found" ]; then
        echo "не нашёл NDK в $ANDROID_HOME/ndk" >&2
        exit 1
    fi
    export ANDROID_NDK_HOME="$found"
fi

export PATH="$PATH:$(go env GOPATH)/bin"

if [ "${1:-}" != "--skip-core" ]; then
    echo "== ядро -> android/app/libs/veil.aar"
    gomobile bind \
        -target=android/arm64,android/arm \
        -androidapi 24 \
        -javapkg=io.veil \
        -o android/app/libs/veil.aar \
        ./mobile
fi

echo "== приложение"
(cd android && ./gradlew assembleDebug)

echo
echo "готово: android/app/build/outputs/apk/debug/app-debug.apk"
