<#
    Локальная сквозная проверка Veil для Windows.

    Поднимает ноду и клиента на localhost с самоподписанным сертификатом
    и проверяет три вещи:
      1. трафик пользователя ходит через туннель;
      2. мультиплексирование работает — соединений до ноды меньше, чем запросов;
      3. посторонний, пришедший на ноду, видит сайт-прикрытие, а не разрыв.

    Запуск из папки проекта:
      .\scripts\smoke.ps1
#>

$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$work      = Join-Path ([System.IO.Path]::GetTempPath()) ("veil-smoke-" + [guid]::NewGuid().ToString("N").Substring(0, 8))
$nodeLog   = Join-Path $work "node.log"
$nodeErr   = Join-Path $work "node.err"
$clientLog = Join-Path $work "client.log"
$clientErr = Join-Path $work "client.err"
$probe     = Join-Path $work "probe.html"
New-Item -ItemType Directory -Path $work -Force | Out-Null

$nodeAddr  = "127.0.0.1:8443"
$socksAddr = "127.0.0.1:1080"
$cover     = "https://example.com"
$sni       = "cover.local"

$node   = $null
$client = $null

function Stop-Started {
    if ($null -ne $script:client) { try { Stop-Process -Id $script:client.Id -Force -ErrorAction Stop } catch {} }
    if ($null -ne $script:node)   { try { Stop-Process -Id $script:node.Id   -Force -ErrorAction Stop } catch {} }
}

try {
    Write-Host "== сборка ==" -ForegroundColor Cyan
    go build -o bin/ ./...
    if ($LASTEXITCODE -ne 0) { throw "сборка не прошла" }

    $keys = & .\bin\veil-keygen.exe -quiet
    $priv = $keys[0]
    $pub  = $keys[1]

    Write-Host "== запуск ноды ==" -ForegroundColor Cyan
    $node = Start-Process -FilePath .\bin\veil-server.exe `
        -ArgumentList @("-listen", $nodeAddr, "-key", $priv, "-tls-self-signed", $sni, "-cover", $cover) `
        -RedirectStandardOutput $nodeLog -RedirectStandardError $nodeErr `
        -NoNewWindow -PassThru

    # Ждём, пока нода начнёт слушать. Повторы делает сам curl.
    & curl.exe -sk --retry 20 --retry-connrefused --retry-delay 1 --max-time 40 -o NUL "https://$nodeAddr/"
    if ($LASTEXITCODE -ne 0) {
        Get-Content $nodeErr -ErrorAction SilentlyContinue
        throw "нода не поднялась"
    }

    Write-Host "== запуск клиента ==" -ForegroundColor Cyan
    $client = Start-Process -FilePath .\bin\veil-client.exe `
        -ArgumentList @("-listen", $socksAddr, "-server", $nodeAddr, "-pubkey", $pub, "-sni", $sni, "-insecure") `
        -RedirectStandardOutput $clientLog -RedirectStandardError $clientErr `
        -NoNewWindow -PassThru

    Write-Host ""
    Write-Host "== 1. трафик через туннель ==" -ForegroundColor Cyan
    $code = & curl.exe -s --retry 20 --retry-connrefused --retry-delay 1 --max-time 40 `
        -o NUL -w "%{http_code}" -x "socks5h://$socksAddr" "$cover/"
    Write-Host "первый запрос: HTTP $code"
    if ($code -ne "200") {
        Get-Content $clientErr -ErrorAction SilentlyContinue
        throw "туннель не работает"
    }

    Write-Host ""
    Write-Host "== 2. мультиплексирование: 12 параллельных запросов ==" -ForegroundColor Cyan
    $curlArgs = @("-s", "--parallel", "--parallel-immediate", "--max-time", "30",
                  "-x", "socks5h://$socksAddr", "-w", "%{http_code} ")
    foreach ($i in 1..12) { $curlArgs += @("-o", "NUL", "$cover/?n=$i") }
    & curl.exe @curlArgs
    Write-Host ""

    # Go пишет журнал в stderr, а не в stdout — читаем оба потока.
    $log = @()
    foreach ($file in @($nodeLog, $nodeErr)) {
        if (Test-Path $file) { $log += Get-Content $file -Encoding UTF8 }
    }
    $requests = ($log | Select-String -Pattern " -> ").Count
    $sessions = ($log | Select-String -Pattern "сессия открыта").Count
    Write-Host "запросов пользователя: $requests"
    Write-Host "соединений до ноды:    $sessions  (без мультиплексирования было бы $requests)"

    Write-Host ""
    Write-Host "== 3. что видит посторонний ==" -ForegroundColor Cyan
    $probeCode = & curl.exe -sk --max-time 20 -o $probe -w "%{http_code}" "https://$nodeAddr/"
    Write-Host "HTTP $probeCode"
    $head = (Get-Content $probe -Raw -Encoding UTF8)
    Write-Host $head.Substring(0, [Math]::Min(100, $head.Length))

    Write-Host ""
    if (($sessions -lt $requests) -and ($head -match "(?i)example domain")) {
        Write-Host "ИТОГ: всё в порядке" -ForegroundColor Green
    } else {
        Write-Host "ИТОГ: что-то не так, логи в $work" -ForegroundColor Red
        exit 1
    }
}
finally {
    Stop-Started
    Remove-Item $work -Recurse -Force -ErrorAction SilentlyContinue
}
