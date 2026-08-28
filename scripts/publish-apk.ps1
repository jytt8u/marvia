<#
    Сборка и выкладка приложения для Android.

    Отдельно от общей сборки, и это осознанно: подписывается APK ключом, а
    ключ подписи лежит на машине разработчика и в облачную сборку не уезжает.
    Ключ у приложения ровно один навсегда — потеряешь или подменишь, и
    обновление не встанет ни у одного покупателя, придётся ставить заново.

    После каждого релиза APK нужно выложить этой командой: установщик панели
    берёт его из последнего релиза и кладёт продавцу в dist, откуда панель
    раздаёт его покупателям.

    Пример:
        .\scripts\publish-apk.ps1 -Tag v0.2.0
#>
[CmdletBinding()]
param(
    # Метка релиза, к которому прикладываем файл.
    [Parameter(Mandatory = $true)]
    [string]$Tag,

    # Репозиторий сборок. Открытый: покупатель качает без токенов и логинов.
    [string]$Repo = 'jytt8u/marvia-releases',

    # Пропустить пересборку ядра на Go — она долгая и нужна, только когда
    # менялся Go, а не Kotlin.
    [switch]$SkipCore
)

$ErrorActionPreference = 'Stop'
Set-Location (Join-Path $PSScriptRoot '..')

if (-not $env:ANDROID_HOME) { $env:ANDROID_HOME = 'D:\android-sdk' }
if (-not $env:VEIL_RELEASE_KEYS) { $env:VEIL_RELEASE_KEYS = 'D:\veil-keys\veil-release.properties' }

if (-not (Test-Path $env:VEIL_RELEASE_KEYS)) {
    throw "нет ключа подписи: $env:VEIL_RELEASE_KEYS. Неподписанный APK не поставится."
}

if (-not $SkipCore) {
    $env:PATH = "$env:PATH;$(go env GOPATH)\bin"
    if (-not $env:ANDROID_NDK_HOME) {
        $ndk = Get-ChildItem (Join-Path $env:ANDROID_HOME 'ndk') -Directory |
            Sort-Object Name | Select-Object -Last 1
        if (-not $ndk) { throw "не нашёл NDK в $env:ANDROID_HOME\ndk" }
        $env:ANDROID_NDK_HOME = $ndk.FullName
    }

    Write-Host '== ядро -> android/app/libs/veil.aar'
    gomobile bind '-target=android/arm64,android/arm' '-androidapi' '24' `
        '-javapkg=io.veil' '-o' 'android/app/libs/veil.aar' './mobile'
    if ($LASTEXITCODE -ne 0) { throw 'не собралась библиотека ядра' }
}

Write-Host '== приложение'
Push-Location android
try {
    .\gradlew.bat assembleRelease --console=plain
    if ($LASTEXITCODE -ne 0) { throw 'не собралось приложение' }
}
finally {
    Pop-Location
}

$apk = 'android\app\build\outputs\apk\release\app-release.apk'
$out = Join-Path ([System.IO.Path]::GetTempPath()) 'marvia-android.apk'
Copy-Item $apk $out -Force

# Подпись проверяем до выкладки: неподписанный или подписанный отладочным
# ключом APK встанет только поверх такого же, а у покупателей стоит боевой.
$signature = & "$env:ANDROID_HOME\build-tools\36.0.0\apksigner.bat" verify --print-certs $out
if ($LASTEXITCODE -ne 0) { throw 'APK не подписан' }
Write-Host $signature[1]

$sum = (Get-FileHash $out -Algorithm SHA256).Hash.ToLower()
"$sum  marvia-android.apk" | Out-File -FilePath "$out.sha256" -Encoding ascii -NoNewline

Write-Host "== выкладываю в $Repo, метка $Tag"
gh release upload $Tag $out "$out.sha256" --repo $Repo --clobber
if ($LASTEXITCODE -ne 0) { throw 'не выложилось' }

Write-Host ''
Write-Host "готово: marvia-android.apk, sha256 $sum"
