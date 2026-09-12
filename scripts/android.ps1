<#
    Сборка приложения Veil для Android.

    Шагов два: ядро на Go превращается в библиотеку, приложение на Kotlin
    собирается вместе с ней. Первый шаг долгий и нужен только когда менялся
    Go, поэтому его можно пропустить ключом -SkipCore.
#>
[CmdletBinding()]
param(
    [switch]$SkipCore,

    # Собрать ядро ещё и под x86_64 — для эмулятора. Под ARM в трансляторе
    # эмулятора ядро на Go падает с SIGILL, и без этой библиотеки приложение
    # там не запустить. В релиз x86_64 не попадает никогда: фильтр ABI в
    # gradle пускает его только в отладочную сборку.
    [switch]$Emulator
)

$ErrorActionPreference = 'Stop'
Set-Location (Join-Path $PSScriptRoot '..')

# SDK ищем сам, а не требуем переменную.
#
# Она нужна только Gradle, и на машине, где Android Studio ставилась обычным
# путём, SDK лежит в предсказуемом месте. Падать с «не задан ANDROID_HOME»,
# когда SDK стоит в двух шагах, — это отправлять человека искать то, что
# программа могла найти сама.
# Заданной переменной не верим на слово, а проверяем.
#
# Она переживает запуск скрипта: $env:ANDROID_HOME остаётся в окне PowerShell
# до его закрытия. Однажды туда попал мусор от сломанной версии этого же
# скрипта, и все следующие запуски в том же окне молча брали его и падали
# дальше по дороге — с сообщением про NDK, хотя виноват был SDK.
$sdkHere = { param($p) $p -and (Test-Path (Join-Path $p 'platform-tools')) }

if (-not (& $sdkHere $env:ANDROID_HOME)) {
    if ($env:ANDROID_HOME) {
        Write-Host "== ANDROID_HOME указывает не на SDK ($env:ANDROID_HOME), ищу сам"
    }
    # Скобки @() снаружи обязательны, и это не украшение.
    #
    # Where-Object с одним совпадением возвращает не список из одного пути, а
    # саму строку. Тогда $where[0] берёт из неё первый символ, и от
    # «D:\android-sdk» остаётся «D»: сборка падала на «Не нашёл NDK в D\ndk»,
    # хотя NDK стоял на месте.
    $where = @(
        @(
            $env:ANDROID_SDK_ROOT
            "$env:LOCALAPPDATA\Android\Sdk"
            "$env:USERPROFILE\Android\Sdk"
            'C:\Android\Sdk'
            'D:\android-sdk'
        ) | Where-Object { $_ -and (Test-Path (Join-Path $_ 'platform-tools')) }
    )

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

# NDK проверяем так же, как SDK, и по той же причине.
$ndkHere = { param($p) $p -and (Test-Path (Join-Path $p 'source.properties')) }

if (-not (& $ndkHere $env:ANDROID_NDK_HOME)) {
    $ndkRoot = Join-Path $env:ANDROID_HOME 'ndk'
    # Сортируем как версии, а не как текст: по тексту «9.0» больше «28.2», и
    # свежий NDK проиграл бы старому, оставшемуся рядом с ним.
    $ndk = Get-ChildItem $ndkRoot -Directory -ErrorAction SilentlyContinue |
        Sort-Object { $v = $null; if ([version]::TryParse($_.Name, [ref]$v)) { $v } else { [version]'0.0' } } |
        Select-Object -Last 1
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
    $targets = 'android/arm64,android/arm'
    if ($Emulator) { $targets += ',android/amd64' }
    gomobile bind `
        "-target=$targets" `
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
