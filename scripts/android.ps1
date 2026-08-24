<#
    Сборка приложения Veil для Android.

    Шагов два: ядро на Go превращается в библиотеку, приложение на Kotlin
    собирается вместе с ней. Первый шаг долгий и нужен только когда менялся
    Go, поэтому его можно пропустить ключом -SkipCore.
#>
[CmdletBinding()]
param(
    [switch]$SkipCore
)

$ErrorActionPreference = 'Stop'
Set-Location (Join-Path $PSScriptRoot '..')

if (-not $env:ANDROID_HOME) {
    throw 'Не задан ANDROID_HOME — путь к Android SDK'
}

if (-not $env:ANDROID_NDK_HOME) {
    $ndkRoot = Join-Path $env:ANDROID_HOME 'ndk'
    $ndk = Get-ChildItem $ndkRoot -Directory -ErrorAction SilentlyContinue |
        Sort-Object Name | Select-Object -Last 1
    if (-not $ndk) {
        throw "Не нашёл NDK в $ndkRoot"
    }
    $env:ANDROID_NDK_HOME = $ndk.FullName
}

$env:PATH = "$env:PATH;$(go env GOPATH)\bin"

if (-not $SkipCore) {
    Write-Host '== ядро -> android/app/libs/veil.aar'
    gomobile bind `
        -target=android/arm64,android/arm `
        -androidapi 24 `
        -javapkg=io.veil `
        -o android/app/libs/veil.aar `
        ./mobile
    if ($LASTEXITCODE -ne 0) { throw 'Не собралась библиотека ядра' }
}

Write-Host '== приложение'
Push-Location android
try {
    .\gradlew.bat assembleDebug
    if ($LASTEXITCODE -ne 0) { throw 'Не собралось приложение' }
}
finally {
    Pop-Location
}

Write-Host ''
Write-Host 'готово: android\app\build\outputs\apk\debug\app-debug.apk'
