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

# SDK ищем сам, а не требуем переменную.
#
# Она нужна только Gradle, и на машине, где Android Studio ставилась обычным
# путём, SDK лежит в предсказуемом месте. Падать с «не задан ANDROID_HOME»,
# когда SDK стоит в двух шагах, — это отправлять человека искать то, что
# программа могла найти сама.
if (-not $env:ANDROID_HOME) {
    $where = @(
        $env:ANDROID_SDK_ROOT
        "$env:LOCALAPPDATA\Android\Sdk"
        "$env:USERPROFILE\Android\Sdk"
        'C:\Android\Sdk'
        'D:\android-sdk'
    ) | Where-Object { $_ -and (Test-Path (Join-Path $_ 'platform-tools')) }

    if (-not $where) {
        throw @'
Не нашёл Android SDK.

Задай путь к нему и повтори:

    $env:ANDROID_HOME = 'путь\к\sdk'

Чтобы не задавать каждый раз:

    [Environment]::SetEnvironmentVariable('ANDROID_HOME', 'путь\к\sdk', 'User')
'@
    }

    $env:ANDROID_HOME = $where[0]
    Write-Host "== SDK: $env:ANDROID_HOME"
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
    Write-Host '== ядро -> android/app/libs/marvia.aar'
    # Аргументы в кавычках, и это обязательно.
    #
    # Без них PowerShell видит запятую в -target=android/arm64,android/arm как
    # свой разделитель списка и разбирает строку как два аргумента, падая
    # ещё до запуска gomobile: «Отсутствует аргумент в списке параметров».
    gomobile bind `
        '-target=android/arm64,android/arm' `
        '-androidapi' '24' `
        '-javapkg=io.marvia' `
        '-o' 'android/app/libs/marvia.aar' `
        './mobile'
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
