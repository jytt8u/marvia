<#
    Скриншоты для карточки в Google Play — с эмулятора, из настоящего
    приложения. Четыре экрана: подключение, страны, тема, «Ещё».

    Экран подключения и страны имеют смысл только подключёнными: с тестовым
    ключом там «не подключилось» и пустой список. Поэтому перед запуском
    вставь в приложение на эмуляторе настоящий ключ и подключись — скрипт
    снимает то, что видит, и не притворяется.

    Эмулятор «marvia» и сборка под x86_64 — из scripts/android.ps1 -Emulator.
#>
[CmdletBinding()]
param(
    [string]$Avd = 'marvia',
    [string]$Out = 'dist/play-shots',
    # Не ставить apk заново: снимать то, что уже стоит и настроено.
    [switch]$SkipInstall
)
$ErrorActionPreference = 'Stop'
Set-Location (Join-Path $PSScriptRoot '..')

$sdk = $env:ANDROID_HOME
if (-not $sdk -or -not (Test-Path (Join-Path $sdk 'platform-tools'))) {
    foreach ($p in @("$env:LOCALAPPDATA\Android\Sdk", 'D:\android-sdk')) {
        if (Test-Path (Join-Path $p 'platform-tools')) { $sdk = $p; break }
    }
}
if (-not $sdk) { throw 'не нашёл Android SDK: задай ANDROID_HOME' }
$adb = Join-Path $sdk 'platform-tools\adb.exe'
$emu = Join-Path $sdk 'emulator\emulator.exe'

# Эмулятор поднимаем, только если его нет: перезапуск стирает то, что человек
# в нём настроил, — ключ, тему, подключение.
$devices = & $adb devices | Select-String 'emulator-\d+\s+device'
if (-not $devices) {
    Start-Process -FilePath $emu -ArgumentList "-avd $Avd -no-snapshot-load -no-boot-anim" -WindowStyle Minimized
    $i = 0
    do { Start-Sleep -Seconds 5; $b = (& $adb shell getprop sys.boot_completed 2>$null); $i++ } while ($b -ne '1' -and $i -lt 48)
    if ($b -ne '1') { throw 'эмулятор не загрузился за четыре минуты' }
}

if (-not $SkipInstall) {
    $apk = 'android\app\build\outputs\apk\debug\app-debug.apk'
    if (-not (Test-Path $apk)) { throw "нет $apk — собери: scripts\android.ps1 -Emulator" }
    & $adb install -r $apk | Out-Null
}

New-Item -ItemType Directory -Force $Out | Out-Null
& $adb shell am start -n io.marvia.android/.MainActivity | Out-Null
Start-Sleep -Seconds 3

# Экран 1080×2400: вкладки внизу. Снимок — через файл на устройстве:
# exec-out в PowerShell портит двоичный вывод кодировкой.
function Shot([string]$name) {
    & $adb shell screencap -p /sdcard/shot.png
    & $adb pull /sdcard/shot.png (Join-Path $Out "$name.png") | Out-Null
    & $adb shell rm /sdcard/shot.png
}
function Tab([int]$x) { & $adb shell input tap $x 2244; Start-Sleep -Seconds 2 }

Tab 146; Shot '1-connect'
Tab 408; Shot '2-countries'
# Вкладка «Тема» помнит прокрутку — возвращаем к готовым видам.
Tab 670; 1..8 | ForEach-Object { & $adb shell input swipe 540 600 540 1900 150 }; Start-Sleep -Seconds 1; Shot '3-theme'
Tab 932; Shot '4-more'

Write-Host "готово: $Out — 1080×2400, как просит консоль Play"
