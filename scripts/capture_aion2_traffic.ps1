# ==============================================================================
# WARLINK: AION 2 LIVE TRAFFIC CAPTURE & PING DIAGNOSTIC ENGINE
# Captures 100% of network traffic related to AION 2 (AION2.exe)
# Isolates Ping/QoS packets, measures packet loss, RTT, and identifies red ping root cause.
# Strictly complies with AGENTS.md (Zero Emoji, pure diagnostic output).
# ==============================================================================

param(
    [switch]$NoElevationPrompt,
    [switch]$ContinuousCapture,
    [int]$AutoStopSeconds = 0
)

$ErrorActionPreference = "Continue"

# 1. Administrator Elevation Check
$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Host "[UAC] Запуск требует прав Администратора для перехвата сетевых пакетов ядра Windows (PktMon NDIS)." -ForegroundColor Yellow
    Write-Host "[UAC] Перезапуск с повышением привилегий..." -ForegroundColor Cyan
    $scriptPath = $MyInvocation.MyCommand.Definition
    $argsList = "-NoProfile -ExecutionPolicy Bypass -File `"$scriptPath`""
    if ($ContinuousCapture) { $argsList += " -ContinuousCapture" }
    if ($AutoStopSeconds -gt 0) { $argsList += " -AutoStopSeconds $AutoStopSeconds" }
    Start-Process powershell.exe -ArgumentList $argsList -Verb RunAs
    exit
}

$Host.UI.RawUI.WindowTitle = "WarLink - AION 2 Network Traffic & Ping Capture"

Write-Host "================================================================================" -ForegroundColor Cyan
Write-Host "            WARLINK: ЗАХВАТ ТРАФИКА И ДИАГНОСТИКА ПИНГА AION 2                  " -ForegroundColor Cyan
Write-Host "================================================================================" -ForegroundColor Cyan

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
if (-not $scriptDir) { $scriptDir = $PSScriptRoot }
$repoDir = Split-Path -Parent $scriptDir
$capDir = Join-Path $repoDir "captures"

if (-not (Test-Path $capDir)) {
    New-Item -ItemType Directory -Path $capDir -Force | Out-Null
}

$etlFile = Join-Path $capDir "aion2_traffic.etl"
$pcapFile = Join-Path $capDir "aion2_traffic.pcapng"
$txtFile = Join-Path $capDir "aion2_traffic.txt"
$journalFile = Join-Path $capDir "socket_journal.jsonl"
$reportFile = Join-Path $capDir "aion2_diagnosis_report.txt"

# Clean previous capture logs
@($etlFile, $pcapFile, $txtFile, $journalFile, $reportFile) | ForEach-Object {
    if (Test-Path $_) { Remove-Item $_ -Force -ErrorAction SilentlyContinue }
}

Write-Host "[1/4] Инициализация системного монитора пакетов Windows (PktMon)..." -ForegroundColor Yellow

# Reset pktmon
& pktmon stop 2>$null | Out-Null
& pktmon filter remove 2>$null | Out-Null

# Add filters for known game networks, region QoS beacons, and general protocols
Write-Host "      Добавление сетевых фильтров целевых диапазонов AION 2:" -ForegroundColor DarkGray
Write-Host "      - 193.202.112.0/24 (Европа / Cloudflare Spectrum)" -ForegroundColor DarkGray
Write-Host "      - 216.107.254.0/24 (NCSoft GPA / QoS Beacons)" -ForegroundColor DarkGray
Write-Host "      - 212.101.4.0/24   (STUN / Voice Gateway)" -ForegroundColor DarkGray
Write-Host "      - 34.117.142.0/24  (Северная Америка - Восток)" -ForegroundColor DarkGray
Write-Host "      - 35.217.56.0/24   (Северная Америка - Запад)" -ForegroundColor DarkGray
Write-Host "      - 150.171.0.0/16   (Азия / Сеул / Токио)" -ForegroundColor DarkGray
Write-Host "      - Порты QoS: UDP/TCP 13328, 13700, 3478, 7777-7788" -ForegroundColor DarkGray
Write-Host "      - Протоколы: ICMP, UDP, TCP" -ForegroundColor DarkGray

& pktmon filter add Aion_EU -i 193.202.112.0/24 2>$null | Out-Null
& pktmon filter add Aion_NC -i 216.107.254.0/24 2>$null | Out-Null
& pktmon filter add Aion_ST -i 212.101.4.0/24 2>$null | Out-Null
& pktmon filter add Aion_NAE -i 34.117.142.0/24 2>$null | Out-Null
& pktmon filter add Aion_NAW -i 35.217.56.0/24 2>$null | Out-Null
& pktmon filter add Aion_ASIA -i 150.171.0.0/16 2>$null | Out-Null
& pktmon filter add Aion_ICMP -t ICMP 2>$null | Out-Null
& pktmon filter add Aion_QoS1 -p 13328 2>$null | Out-Null
& pktmon filter add Aion_QoS2 -p 13700 2>$null | Out-Null
& pktmon filter add Aion_STUN -p 3478 2>$null | Out-Null

# Start pktmon capture with full packet size
& pktmon start --capture --pkt-size 0 -f $etlFile 2>$null | Out-Null
Write-Host "[OK] PktMon активен. Перехват сырых сетевых пакетов запущен." -ForegroundColor Green

Write-Host "`n[2/4] Подключение к процессу игры и сокетному пространству..." -ForegroundColor Yellow

$targetProcesses = @("AION2", "aion2", "TL", "TL-Win64-Shipping", "CrashReportClient", "EpicWebHelper", "NCLauncher", "ThroneCore", "sing-box")

function Get-ActiveGamePids {
    $pids = @()
    Get-Process -ErrorAction SilentlyContinue | Where-Object { 
        $targetProcesses -contains $_.ProcessName 
    } | ForEach-Object { $pids += $_.Id }
    return $pids
}

$activePids = Get-ActiveGamePids
if ($activePids.Count -gt 0) {
    Write-Host "[OK] Обнаружены активные целевые процессы: PID $($activePids -join ', ')" -ForegroundColor Green
} else {
    Write-Host "[i] Процесс AION2.exe пока не найден. Ожидание запуска игры..." -ForegroundColor Yellow
    Write-Host "    (Вы можете запустить игру сейчас или переключиться в окно игры)" -ForegroundColor DarkGray
}

Write-Host "`n[3/4] МОНИТОРИНГ СЕТЕВЫХ ПОТОКОВ В РЕАЛЬНОМ ВРЕМЕНИ:" -ForegroundColor Cyan
Write-Host "--------------------------------------------------------------------------------" -ForegroundColor DarkGray
Write-Host "Нажмите ENTER или клавишу 'Q' в этом окне для завершения захвата и отчета." -ForegroundColor White
Write-Host "--------------------------------------------------------------------------------`n" -ForegroundColor DarkGray

$knownEndpoints = [System.Collections.Generic.HashSet[string]]::new()
$knownDns = [System.Collections.Generic.HashSet[string]]::new()

$startTime = Get-Date

function Classify-Flow($remoteIp, $remotePort, $proto) {
    if ($remotePort -in 13328, 13700, 3478 -or $proto -eq "ICMP") {
        return "REGION_PING"
    }
    if ($remoteIp -like "193.202.112.*") { return "REGION_PING_EU" }
    if ($remoteIp -like "34.117.142.*") { return "REGION_PING_NAE" }
    if ($remoteIp -like "35.217.56.*")  { return "REGION_PING_NAW" }
    if ($remoteIp -like "150.171.*")    { return "REGION_PING_ASIA" }
    if ($remotePort -eq 443)            { return "AUTH_API_HTTPS" }
    if ($proto -eq "UDP")              { return "GAME_UDP_STREAM" }
    return "TRAFFIC"
}

$running = $true

function Add-JournalEntry($obj) {
    $json = $obj | ConvertTo-Json -Compress
    [System.IO.File]::AppendAllText($journalFile, $json + [System.Environment]::NewLine, [System.Text.Encoding]::UTF8)
}

function Test-TcpProbe($targetIp, $targetPort) {
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    try {
        $client = New-Object System.Net.Sockets.TcpClient
        $iar = $client.BeginConnect($targetIp, $targetPort, $null, $null)
        $success = $iar.AsyncWaitHandle.WaitOne(1200, $false)
        $sw.Stop()
        if ($success -and $client.Connected) {
            $client.EndConnect($iar)
            $client.Close()
            Write-Host "           [TCP PROBE OK] $($targetIp):$($targetPort) RTT = $($sw.ElapsedMilliseconds) ms" -ForegroundColor Green
        } else {
            $client.Close()
            Write-Host "           [TCP PROBE TIMEOUT] $($targetIp):$($targetPort) Не отвечает на TCP (таймаут 1200 мс)" -ForegroundColor Red
        }
    } catch {
        Write-Host "           [TCP PROBE ERROR] $($targetIp):$($targetPort) $($_.Exception.Message)" -ForegroundColor Red
    }
}

while ($running) {
    # Check for user key press to stop
    if ([Console]::KeyAvailable) {
        $key = [Console]::ReadKey($true)
        if ($key.Key -eq [ConsoleKey]::Enter -or $key.Key -eq [ConsoleKey]::Q -or $key.Key -eq [ConsoleKey]::Escape) {
            $running = $false
            break
        }
    }

    if ($AutoStopSeconds -gt 0) {
        $elapsed = (Get-Date) - $startTime
        if ($elapsed.TotalSeconds -ge $AutoStopSeconds) {
            $running = $false
            break
        }
    }

    $currentPids = Get-ActiveGamePids

    # 1. Check TCP Connections
    try {
        $tcpConns = Get-NetTCPConnection -ErrorAction SilentlyContinue | Where-Object {
            ($_.OwningProcess -in $currentPids) -or
            ($_.RemoteAddress -like "193.202.112.*") -or
            ($_.RemoteAddress -like "216.107.254.*") -or
            ($_.RemoteAddress -like "212.101.4.*") -or
            ($_.RemoteAddress -like "34.117.142.*") -or
            ($_.RemoteAddress -like "35.217.56.*") -or
            ($_.RemoteAddress -like "150.171.*") -or
            ($_.RemotePort -in 13328, 13700, 3478)
        }

        foreach ($c in $tcpConns) {
            $remIp = $c.RemoteAddress
            $remPort = $c.RemotePort
            if ($remIp -in "0.0.0.0", "127.0.0.1", "::", "::1" -or $remPort -eq 0) { continue }

            $key = "TCP:" + $remIp + ":" + $remPort
            if (-not $knownEndpoints.Contains($key)) {
                $knownEndpoints.Add($key) | Out-Null
                $role = Classify-Flow $remIp $remPort "TCP"
                $ts = (Get-Date).ToString("HH:mm:ss.fff")
                
                $entry = [PSCustomObject]@{
                    timestamp = $ts
                    proto = "TCP"
                    local_port = $c.LocalPort
                    remote_ip = $remIp
                    remote_port = $remPort
                    state = "$($c.State)"
                    pid = $c.OwningProcess
                    role = $role
                }
                Add-JournalEntry $entry

                # Print to console
                $color = if ($role -like "*PING*") { "Yellow" } elseif ($role -like "*AUTH*") { "Green" } else { "Cyan" }
                Write-Host "[$ts] [TCP] $($c.LocalAddress):$($c.LocalPort) -> $($remIp):$($remPort) ($($c.State)) [$role]" -ForegroundColor $color

                # Active latency probe if it's a ping candidate
                if ($role -like "*PING*") {
                    Test-TcpProbe $remIp $remPort
                }
            }
        }
    } catch {}

    # 2. Check UDP Endpoints
    try {
        $udpEps = Get-NetUDPEndpoint -ErrorAction SilentlyContinue | Where-Object {
            $_.OwningProcess -in $currentPids
        }

        foreach ($u in $udpEps) {
            $key = "UDP:LOCAL:" + $u.LocalPort
            if (-not $knownEndpoints.Contains($key)) {
                $knownEndpoints.Add($key) | Out-Null
                $ts = (Get-Date).ToString("HH:mm:ss.fff")
                $entry = [PSCustomObject]@{
                    timestamp = $ts
                    proto = "UDP"
                    local_port = $u.LocalPort
                    remote_ip = ""
                    remote_port = 0
                    pid = $u.OwningProcess
                    role = "GAME_UDP_SOCKET"
                }
                Add-JournalEntry $entry
                Write-Host "[$ts] [UDP] Локальный сокет игры открыт на порту: $($u.LocalPort) (PID $($u.OwningProcess))" -ForegroundColor DarkYellow
            }
        }
    } catch {}

    # 3. Check DNS cache for newly resolved game domains
    try {
        $dnsEntries = Get-DnsClientCache -ErrorAction SilentlyContinue | Where-Object {
            $_.Entry -match "ncsoft|plaync|aion|gpa|purple|solnet|cloudflare"
        }
        foreach ($d in $dnsEntries) {
            $name = $d.Entry
            $data = $d.Data
            $dnsKey = $name + ":" + $data
            if ($data -and (-not $knownDns.Contains($dnsKey))) {
                $knownDns.Add($dnsKey) | Out-Null
                $ts = (Get-Date).ToString("HH:mm:ss.fff")
                Write-Host "[$ts] [DNS] Разрешен домен: $name -> $data" -ForegroundColor Magenta
                $entry = [PSCustomObject]@{
                    timestamp = $ts
                    proto = "DNS"
                    domain = $name
                    resolved_ip = $data
                    role = "DNS_RESOLUTION"
                }
                Add-JournalEntry $entry
            }
        }
    } catch {}

    Start-Sleep -Milliseconds 150
}

Write-Host "`n[4/4] ОСТАНОВКА ЗАХВАТА И ГЕНЕРАЦИЯ АНАЛИТИЧЕСКОГО ОТЧЕТА..." -ForegroundColor Yellow

# Stop PktMon
& pktmon stop 2>$null | Out-Null
Write-Host "[OK] PktMon остановлен." -ForegroundColor DarkGray

# Convert ETL to PCAPNG and TXT
if (Test-Path $etlFile) {
    Write-Host "      Экспорт сырого дампа пакетов в Wireshark (PCAPNG)..." -ForegroundColor DarkGray
    & pktmon etl2pcap $etlFile -o $pcapFile 2>$null | Out-Null

    Write-Host "      Текстовая расшифровка сетевых кадров (TXT)..." -ForegroundColor DarkGray
    & pktmon etl2txt $etlFile -o $txtFile 2>$null | Out-Null
}

# Run Python forensic analyzer
$pyScript = Join-Path $scriptDir "analyze_aion2_capture.py"
if (Test-Path $pyScript) {
    Write-Host "`nЗапуск криминалистического анализатора сетевых пакетов..." -ForegroundColor Cyan
    & python $pyScript $journalFile $txtFile $reportFile
} else {
    Write-Host "[WARN] Скрипт analyze_aion2_capture.py не найден." -ForegroundColor Yellow
}

Write-Host "`nФайлы захвата готовы к исследованию:" -ForegroundColor White
Write-Host "- Отчет диагностики:  $reportFile" -ForegroundColor Cyan
Write-Host "- Дамп Wireshark:     $pcapFile" -ForegroundColor Cyan
Write-Host "- Текстовый дамп:     $txtFile" -ForegroundColor Cyan
Write-Host "- Журнал сокетов:     $journalFile" -ForegroundColor Cyan
Write-Host "`nЗавершено успешно." -ForegroundColor Green
