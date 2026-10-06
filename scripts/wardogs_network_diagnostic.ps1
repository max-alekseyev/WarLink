# ==============================================================================
# WARDOGS Network & Connectivity Diagnostics Utility
# Bulkhead Interactive / Pragma Engine / Elytra AC Diagnostics
# Strict Rule 3: Zero Emoji in console output and source code
# ==============================================================================

[CmdletBinding()]
param (
    [switch]$Detailed = $false,
    [string]$OutputFile = ""
)

# Output buffer for saving report
$reportLines = [System.Collections.Generic.List[string]]::new()

function Write-Diag([string]$Level, [string]$Title, [string]$Message = "", [string]$Detail = "") {
    $timestamp = Get-Date -Format "HH:mm:ss"
    $tag = switch ($Level.ToUpper()) {
        "OK"    { "[OK]  " }
        "WARN"  { "[WARN]" }
        "FAIL"  { "[FAIL]" }
        "INFO"  { "[INFO]" }
        default { "[INFO]" }
    }

    $color = switch ($Level.ToUpper()) {
        "OK"    { "Green" }
        "WARN"  { "Yellow" }
        "FAIL"  { "Red" }
        "INFO"  { "Cyan" }
        default { "White" }
    }

    $line = "$tag $Title"
    if ($Message) { $line += ": $Message" }
    
    Write-Host $line -ForegroundColor $color
    $reportLines.Add("[$timestamp] $line")

    if ($Detail) {
        Write-Host "       Детали: $Detail" -ForegroundColor Gray
        $reportLines.Add("[$timestamp]        Детали: $Detail")
    }
}

function Write-Section([string]$Title) {
    Write-Host ""
    Write-Host ("=" * 78) -ForegroundColor DarkCyan
    Write-Host "  $Title" -ForegroundColor White
    Write-Host ("=" * 78) -ForegroundColor DarkCyan
    $reportLines.Add("")
    $reportLines.Add("=== $Title ===")
}

[System.Net.ServicePointManager]::SecurityProtocol = [System.Net.SecurityProtocolType]::Tls12 -bor [System.Net.SecurityProtocolType]::Tls11 -bor [System.Net.SecurityProtocolType]::Tls

# ------------------------------------------------------------------------------
# 1. Header & Privileges
# ------------------------------------------------------------------------------
Clear-Host
Write-Host "==============================================================================" -ForegroundColor Cyan
Write-Host "   WARDOGS (Bulkhead Interactive) - КОМПЛЕКСНАЯ ДИАГНОСТИКА СЕТИ И СИСТЕМЫ     " -ForegroundColor White
Write-Host "   Диагностика ошибок: WD-L004, Access Denied, сбоев лобби и конфликтов ПО    " -ForegroundColor Gray
Write-Host "==============================================================================" -ForegroundColor Cyan

$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) {
    Write-Diag "WARN" "Права администратора" "Скрипт запущен БЕЗ прав Администратора" "Некоторые проверки служб и драйверов ядра могут вернуть ограниченный результат. Для полного аудита запустите PowerShell от Администратора."
} else {
    Write-Diag "OK" "Права администратора" "Запущен с повышенными привилегиями (Administrator)"
}

# ------------------------------------------------------------------------------
# 2. System Time Drift Audit (Critical for TLS and Auth Tokens)
# ------------------------------------------------------------------------------
Write-Section "1. СИСТЕМНОЕ ВРЕМЯ И СИНХРОНИЗАЦИЯ ЧАСОВ"

try {
    $req = [System.Net.HttpWebRequest]::Create("https://www.cloudflare.com")
    $req.Method = "HEAD"
    $req.Timeout = 3000
    $resp = $req.GetResponse()
    $serverDateHeader = $resp.Headers["Date"]
    $resp.Close()

    if ($serverDateHeader) {
        $serverTimeUtc = [DateTime]::Parse($serverDateHeader).ToUniversalTime()
        $localTimeUtc = [DateTime]::UtcNow
        $driftSeconds = [Math]::Abs(($localTimeUtc - $serverTimeUtc).TotalSeconds)

        if ($driftSeconds -gt 30) {
            Write-Diag "FAIL" "Рассинхронизация часов" "Отклонение $driftSeconds сек!" "Серверы Bulkhead сбрасывают TLS/OAuth сессии при расхождении >30 сек. Откройте 'Параметры -> Время и язык -> Дата и время -> Синхронизировать'."
        } elseif ($driftSeconds -gt 5) {
            Write-Diag "WARN" "Смещение часов" "Отклонение $driftSeconds сек" "Рекомендуется синхронизировать системные часы Windows."
        } else {
            Write-Diag "OK" "Системные часы" "Точное время (рассинхронизация: $([Math]::Round($driftSeconds, 2)) сек)"
        }
    }
} catch {
    Write-Diag "WARN" "Проверка времени" "Не удалось сверить часы с эталонным NTP/TLS сервером" $_.Exception.Message
}

# ------------------------------------------------------------------------------
# 3. Windows Proxy & Hosts File Audit
# ------------------------------------------------------------------------------
Write-Section "2. СИСТЕМНЫЕ ПРОКСИ И ФАЙЛ HOSTS"

# WinINet Registry Proxy
$proxyEnable = (Get-ItemProperty -Path "HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings" -Name "ProxyEnable" -ErrorAction SilentlyContinue).ProxyEnable
$proxyServer = (Get-ItemProperty -Path "HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings" -Name "ProxyServer" -ErrorAction SilentlyContinue).ProxyServer

if ($proxyEnable -eq 1) {
    Write-Diag "WARN" "WinINet Прокси" "ВКЛЮЧЕН ($proxyServer)" "Если программа-прокси отключена, сетевой трафик Windows может заблокироваться. Проверьте 'Параметры -> Сеть -> Прокси'."
} else {
    Write-Diag "OK" "WinINet Прокси" "Выключен (Прямое подключение)"
}

# WinHTTP System Proxy (used by Windows Services and Steam/Wardogs launcher)
$winHttpOut = (netsh winhttp show proxy) -join " "
if ($winHttpOut -match "Direct access" -or $winHttpOut -match "Прямой доступ") {
    Write-Diag "OK" "WinHTTP Системный прокси" "Прямой доступ (Без прокси)"
} else {
    Write-Diag "FAIL" "WinHTTP Системный прокси" "ОБНАРУЖЕН ПРОКСИ" "Лаунчер Wardogs использует WinHTTP (WinHttpOpen). Зависший прокси вызывает ошибку WD-L003 и WD-L004. Сброс: 'netsh winhttp reset proxy'."
}

# Hosts File Audit
$hostsPath = "$env:SystemRoot\System32\drivers\etc\hosts"
if (Test-Path $hostsPath) {
    $hostsContent = Get-Content $hostsPath -ErrorAction SilentlyContinue
    $suspiciousHosts = $hostsContent | Where-Object { 
        $_ -notmatch "^\s*#" -and $_ -match "(bulkhead|pragma|elytra|steam|epic|discord|cloudflare|fastly)" 
    }

    if ($suspiciousHosts) {
        Write-Diag "FAIL" "Файл hosts" "Обнаружены переопределения игровых доменов!" ($suspiciousHosts -join ", ")
    } else {
        Write-Diag "OK" "Файл hosts" "Чист (нет вмешательств в игровые домены)"
    }
}

# ------------------------------------------------------------------------------
# 4. Conflicting Software & Drivers Audit
# ------------------------------------------------------------------------------
Write-Section "3. АУДИТ КОНФЛИКТУЮЩЕГО ПО, ДРАЙВЕРОВ И DPI-УТИЛИТ"

# Conflicting processes map
$knownConflicts = @{
    # DPI Bypass Tools
    "winws"                 = "Zapret (winws) - модифицирует TLS рукопожатия, вызывает WD-L004"
    "goodbyedpi"            = "GoodbyeDPI - WinDivert перехватчик, сбрасывает TCP пакеты игры"
    "byedpi"                = "ByeDPI - локальный SOCKS прокси"
    "ciadpi"                = "ByeDPI (ciadpi)"
    "spoof-dpi"             = "SpoofDPI"
    
    # Game Boosters & Multi-routing
    "gearup"                = "GearUP Booster - конкурирующий сетевой драйвер/маршрутизатор"
    "gearupbooster"         = "GearUP Booster"
    "exitlag"               = "ExitLag - WFP/LSP фильтр сетевого стека"
    "outfox"                = "Outfox Gaming VPN"
    
    # VPN / TUN Clients
    "warp-svc"              = "Cloudflare WARP Service - глобальный Wintun перехват"
    "cloudflare-warp"       = "Cloudflare WARP GUI"
    "wireguard"             = "WireGuard"
    "openvpn"               = "OpenVPN"
    "openvpnserv"           = "OpenVPN Service"
    "tailscale"             = "Tailscale"
    "tailscaled"            = "Tailscale Service"
    "zerotier-one_x64"      = "ZeroTier"
    "amneziavpn"            = "Amnezia VPN"
    "amneziawg"             = "AmneziaWG"
    "clash"                 = "Clash Core"
    "clash-verge"           = "Clash Verge"
    "clash-meta"            = "Clash Meta"
    "mihomo"                = "Mihomo Core"
    "v2ray"                 = "v2ray"
    "v2rayn"                = "v2rayN"
    "xray"                  = "Xray Core"
    "sing-box"              = "sing-box (активен TUN туннель)"
    
    # Packet Filters & Network Shapers
    "adguard"               = "AdGuard (WFP драйвер фильтрации)"
    "adguardsvc"            = "AdGuard Service"
    "cfosspeed"             = "cFosSpeed (формирователь трафика)"
    "killernetworkservice"  = "Killer Network Service"
}

$runningProcesses = Get-Process -ErrorAction SilentlyContinue
$foundConflicts = @()

foreach ($proc in $runningProcesses) {
    $procName = $proc.ProcessName.ToLower()
    if ($knownConflicts.ContainsKey($procName)) {
        $foundConflicts += $knownConflicts[$procName]
    }
}

if ($foundConflicts.Count -gt 0) {
    Write-Diag "FAIL" "Конфликтующие процессы" "Обнаружены запущенные программы ($($foundConflicts.Count))"
    foreach ($item in $foundConflicts) {
        Write-Host "       [!] $item" -ForegroundColor Yellow
        $reportLines.Add("       [!] $item")
    }
    Write-Host "       ВАЖНО: Одновременный запуск Запрета, VPN, GearUP и ExitLag приводит к борьбе сетевых драйверов," -ForegroundColor DarkYellow
    Write-Host "       сбросу TCP-рукопожатий и блокировке IP бэкендом игры (Access Denied / WD-L004)." -ForegroundColor DarkYellow
} else {
    Write-Diag "OK" "Конфликтующие процессы" "Фоновые DPI-утилиты и конкурирующие ускорители не запущены"
}

# WinDivert Driver Service Check
$windivertSvc = Get-Service -Name "WinDivert" -ErrorAction SilentlyContinue
if ($windivertSvc) {
    if ($windivertSvc.Status -eq "Running") {
        Write-Diag "WARN" "Служба WinDivert" "АКТИВНА (Драйвер ядра перехватывает трафик)" "Если параллельно включен VPN, пакеты игры могут сбрасываться или зацикливаться."
    } else {
        Write-Diag "OK" "Служба WinDivert" "Установлена, но остановлена ($($windivertSvc.Status))"
    }
} else {
    Write-Diag "OK" "Служба WinDivert" "Не активна в реестре служб"
}

# Network Adapters Check (Virtual TUN/TAP interfaces)
$adapters = Get-NetAdapter -ErrorAction SilentlyContinue | Where-Object { $_.Status -eq "Up" }
$virtualAdapters = $adapters | Where-Object { 
    $_.InterfaceDescription -match "(Wintun|TAP|Virtual|WireGuard|WARP|Tailscale|VPN)" 
}

if ($virtualAdapters.Count -gt 1) {
    $names = ($virtualAdapters | ForEach-Object { "$($_.Name) ($($_.InterfaceDescription))" }) -join ", "
    Write-Diag "WARN" "Виртуальные адаптеры" "Активно несколько VPN/TUN интерфейсов ($($virtualAdapters.Count))" "$names. Возможен конфликт маршрутизации (Route Metric collision)."
} elseif ($virtualAdapters.Count -eq 1) {
    Write-Diag "INFO" "Виртуальный адаптер" "Активен туннельный интерфейс: $($virtualAdapters[0].Name)"
} else {
    Write-Diag "OK" "Сетевые адаптеры" "Все активные интерфейсы являются физическими"
}

# ------------------------------------------------------------------------------
# 5. Elytra Anti-Cheat Service Integrity Audit
# ------------------------------------------------------------------------------
Write-Section "4. СТАТУС СЛУЖБЫ АНТИЧИТА ELYTRA"

$elytraSvc = Get-Service -Name "Elytra" -ErrorAction SilentlyContinue
$elytraExe = "$env:ProgramFiles\Elytra\service.exe"

if ($elytraSvc) {
    if ($elytraSvc.Status -eq "Running") {
        Write-Diag "OK" "Служба Elytra" "Работает штатно (Status: Running)"
    } else {
        Write-Diag "WARN" "Служба Elytra" "Остановлена (Status: $($elytraSvc.Status))" "Служба должна стартовать автоматически или по требованию лаунчера."
    }
} else {
    Write-Diag "FAIL" "Служба Elytra" "НЕ УСТАНОВЛЕНА В СИСТЕМЕ" "Причина ошибки WD-L006. Запустите 'Elytra-Setup.exe' от Администратора из папки игры в Steam."
}

if (Test-Path $elytraExe) {
    Write-Diag "OK" "Бинарник Elytra" "Обнаружен: $elytraExe"
} else {
    Write-Diag "WARN" "Бинарник Elytra" "Файл $elytraExe не найден" "Если служба не установлена, восстановите ее через свойства игры в Steam."
}

# ------------------------------------------------------------------------------
# 6. Bulkhead & Pragma Endpoints DNS Resolution
# ------------------------------------------------------------------------------
Write-Section "5. DNS РАЗРЕШЕНИЕ СЕРВЕРОВ BULKHEAD И PRAGMA"

$testDomains = @(
    @{ Host = "faa.bulkhead.net"; Role = "Авторизация и конфигурация (Лаунчер / WD-L004)" },
    @{ Host = "game.live.wardogs.bulkhead.pragmaengine.com"; Role = "Координация матчей и матчмейкинг" },
    @{ Host = "social.live.wardogs.bulkhead.pragmaengine.com"; Role = "Лобби, группы и социальный бэкенд" },
    @{ Host = "elytra.ac"; Role = "CDN модулей античита Elytra" },
    @{ Host = "api.steampowered.com"; Role = "Steam Web API (Аутентификация билетов игрока)" }
)

foreach ($item in $testDomains) {
    $h = $item.Host
    $role = $item.Role
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    try {
        $ips = [System.Net.Dns]::GetHostAddresses($h)
        $sw.Stop()
        if ($ips.Count -gt 0) {
            $ipList = ($ips | ForEach-Object { $_.IPAddressToString }) -join ", "
            Write-Diag "OK" "DNS: $h" "Разрешен за $($sw.ElapsedMilliseconds) мс" "IP: $ipList ($role)"
        } else {
            Write-Diag "WARN" "DNS: $h" "Пустой ответ" "DNS-сервер не вернул IP адресов ($role)"
        }
    } catch {
        $sw.Stop()
        Write-Diag "FAIL" "DNS: $h" "СБОЙ РАЗРЕШЕНИЯ DNS" "Не удалось получить адрес за $($sw.ElapsedMilliseconds) мс. Роль: $role. Ошибка: $($_.Exception.Message)"
    }
}

# ------------------------------------------------------------------------------
# 7. Direct TCP & TLS Handshake Audit (Exact WD-L004 / 403 Simulation)
# ------------------------------------------------------------------------------
Write-Section "6. ПРОВЕРКА ПОДКЛЮЧЕНИЯ ПО HTTPS/TLS (СИМУЛЯЦИЯ ЛАУНЧЕРА И ИГРЫ)"

function Test-HttpsEndpoint([string]$HostName, [int]$Port = 443, [string]$Role = "") {
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    $tcpClient = New-Object System.Net.Sockets.TcpClient
    
    try {
        # 1. TCP Connect
        $connectTask = $tcpClient.ConnectAsync($HostName, $Port)
        $timeout = 4000
        if (-not $connectTask.Wait($timeout)) {
            $sw.Stop()
            $tcpClient.Close()
            return @{
                Status = "FAIL"
                Message = "ТАЙМАУТ ПОДКЛЮЧЕНИЯ (> $timeout мс)"
                Detail = "Прямой источник ошибки WD-L004. Провайдер отбрасывает SYN пакеты к узлу."
                Rtt = $sw.ElapsedMilliseconds
            }
        }
        $tcpRtt = $sw.ElapsedMilliseconds

        # 2. TLS Handshake via SslStream
        $netStream = $tcpClient.GetStream()
        $sslStream = New-Object System.Net.Security.SslStream($netStream, $false, ({ $true }))
        
        $sslSw = [System.Diagnostics.Stopwatch]::StartNew()
        $sslStream.AuthenticateAsClient($HostName)
        $sslSw.Stop()
        $tlsRtt = $sslSw.ElapsedMilliseconds

        $tlsVersion = $sslStream.SslProtocol.ToString()
        $cipher = $sslStream.CipherAlgorithm.ToString()
        $sslStream.Close()
        $tcpClient.Close()
        $sw.Stop()

        # 3. HTTP Status Check (Verify if IP is banned / 403 / Access Denied)
        $httpStatus = "N/A"
        try {
            $httpReq = [System.Net.HttpWebRequest]::Create("https://$HostName")
            $httpReq.Method = "GET"
            $httpReq.Timeout = 3500
            $httpReq.UserAgent = "WardogsLauncher/1.2"
            $httpResp = $httpReq.GetResponse()
            $httpStatus = [int]$httpResp.StatusCode
            $httpResp.Close()
        } catch [System.Net.WebException] {
            $webEx = $_.Exception
            if ($webEx -and $webEx.Response) {
                $httpStatus = [int]$webEx.Response.StatusCode
                $webEx.Response.Close()
            } else {
                $httpStatus = "TIMEOUT/FAIL"
            }
        } catch {
            $httpStatus = "ERR"
        }

        $detailMsg = "TCP: ${tcpRtt}мс, TLS: ${tlsRtt}мс, Протокол: $tlsVersion, HTTP код: $httpStatus ($Role)"
        
        if ($httpStatus -eq 403) {
            if ($HostName -eq "faa.bulkhead.net") {
                return @{
                    Status = "OK"
                    Message = "Подключение успешно (Задержка: $($sw.ElapsedMilliseconds) мс)"
                    Detail = "HTTP 403 от Cloudflare (штатный ответ корневого эндпоинта без токена). $detailMsg"
                    Rtt = $sw.ElapsedMilliseconds
                }
            } else {
                return @{
                    Status = "FAIL"
                    Message = "HTTP 403 FORBIDDEN (ДОСТУП ЗАПРЕЩЕН)"
                    Detail = "Бэкенд Pragma отклоняет запросы с вашего IP! Причина ошибки 'Доступ запрещен' в лобби игры. Текущий IP (VPN/прокси) заблокирован или внесен в спам-лист AWS/Bulkhead."
                    Rtt = $sw.ElapsedMilliseconds
                }
            }
        }

        return @{
            Status = "OK"
            Message = "Подключение успешно (Задержка: $($sw.ElapsedMilliseconds) мс)"
            Detail = $detailMsg
            Rtt = $sw.ElapsedMilliseconds
        }

    } catch {
        $sw.Stop()
        $ex = $_.Exception
        $errMsg = $ex.Message
        
        # Detect TCP RST during TLS
        if ($errMsg -match "reset" -or $errMsg -match "сброшено" -or $errMsg -match "forcibly closed") {
            return @{
                Status = "FAIL"
                Message = "СБРОС СОЕДИНЕНИЯ (TCP RST) ВО ВРЕМЯ РУКОПОЖАТИЯ TLS"
                Detail = "ТСПУ провайдера или фильтр Zapret разорвал защищенный сеанс с сервером! Прямая причина WD-L004."
                Rtt = $sw.ElapsedMilliseconds
            }
        }

        return @{
            Status = "FAIL"
            Message = "Ошибка соединения ($errMsg)"
            Detail = "Не удалось завершить TLS рукопожатие с $HostName ($Role)"
            Rtt = $sw.ElapsedMilliseconds
        }
    } finally {
        $tcpClient.Dispose()
    }
}

foreach ($item in $testDomains) {
    $res = Test-HttpsEndpoint -HostName $item.Host -Port 443 -Role $item.Role
    Write-Diag $res.Status "$($item.Host)" $res.Message $res.Detail
}

# ------------------------------------------------------------------------------
# 8. MTU and Packet Fragmentation Audit
# ------------------------------------------------------------------------------
Write-Section "7. ПРОВЕРКА MTU И ФРАГМЕНТАЦИИ ПАКЕТОВ"

try {
    # Test ping with Don't Fragment flag (1472 bytes ICMP + 28 bytes header = 1500 MTU)
    $pingOut = (ping.exe -n 1 -l 1472 -f 1.1.1.1) -join " "
    if ($pingOut -match "fragmented" -or $pingOut -match "фрагментировать") {
        # Test lower MTU (1372 bytes ICMP = 1400 MTU)
        $pingOut1400 = (ping.exe -n 1 -l 1372 -f 1.1.1.1) -join " "
        if ($pingOut1400 -match "TTL=") {
            Write-Diag "WARN" "MTU Сети" "Обнаружен заниженный MTU (< 1500)" "Сеть работает с фрагментацией при стандартном MTU 1500. Характерно для активных VPN туннелей (WARP, WireGuard). Может дропать крупные TLS ClientHello."
        } else {
            Write-Diag "FAIL" "MTU Сети" "Критическая фрагментация пакетов" "Пакеты 1400 байт не проходят без фрагментации."
        }
    } else {
        Write-Diag "OK" "MTU Сети" "Штатный MTU 1500 (Пакеты проходят без принудительной фрагментации)"
    }
} catch {
    Write-Diag "INFO" "MTU Сети" "Тест MTU пропущен"
}

# ------------------------------------------------------------------------------
# 9. Summary & Remediation Guide
# ------------------------------------------------------------------------------
Write-Section "8. ИТОГОВЫЙ ДИАГНОЗ И ПОШАГОВЫЙ ПЛАН УСТРАНЕНИЯ"

Write-Host ""
Write-Host "ПОЧЕМУ ВОЗНИКАЕТ ОШИБКА 004 (WD-L004) И 'ДОСТУП ЗАПРЕЩЕН':" -ForegroundColor Cyan
Write-Host "1. Ошибка 004 (WD-L004) в лаунчере:" -ForegroundColor Yellow
Write-Host "   Лаунчер WardogsLauncher-Shipping.exe при запуске делает прямой HTTPS-запрос к faa.bulkhead.net." -ForegroundColor Gray
Write-Host "   - Если включен Запрет (Zapret/GoodbyeDPI): он подменяет TLS-заголовки (fake/split), и сервер Bulkhead" -ForegroundColor Gray
Write-Host "     мгновенно сбрасывает соединение (TCP RST). Результат -> WD-L004." -ForegroundColor Gray
Write-Host "   - Если включен VPN/WARP/GearUP: если лаунчер попадает в туннель с плохим маршрутом или зависшим" -ForegroundColor Gray
Write-Host "     WinHTTP-прокси, происходит таймаут 5 секунд. Результат -> WD-L004." -ForegroundColor Gray
Write-Host ""
Write-Host "2. Ошибка 'Доступ запрещен' при входе в лобби:" -ForegroundColor Yellow
Write-Host "   Бэкенд Pragma Engine (game.live.wardogs.bulkhead.pragmaengine.com) сверяет ваш IP с сессией Steam." -ForegroundColor Gray
Write-Host "   - Публичные серверы обычных VPN, а также некоторые перегруженные узлы GearUP/ExitLag находятся" -ForegroundColor Gray
Write-Host "     в черном списке датацентров AWS или под защитой от DDoS (возвращают HTTP 403 Forbidden)." -ForegroundColor Gray
Write-Host "   - Смешивание нескольких программ одновременно (Запрет + VPN + Гирап) ломает целостность сетевых сокетов." -ForegroundColor Gray
Write-Host ""
Write-Host "ИНСТРУКЦИЯ ПО ИСПРАВЛЕНИЮ ДЛЯ ИГРОКА (ПО ШАГАМ):" -ForegroundColor Green
Write-Host "1. ПОЛНОСТЬЮ ОТКЛЮЧИТЕ И ЗАКРОЙТЕ КОНФЛИКТУЮЩИЕ УТИЛИТЫ:" -ForegroundColor White
Write-Host "   - Закройте Zapret / GoodbyeDPI (убедитесь через Диспетчер задач, что процесс winws.exe завершен)." -ForegroundColor Gray
Write-Host "   - Отключите сторонние VPN, WARP, ExitLag и GearUP Booster." -ForegroundColor Gray
Write-Host ""
Write-Host "2. СБРОСЬТЕ СЕТЕВОЙ СТЕК WINDOWS (Очистка от зависших прокси):" -ForegroundColor White
Write-Host "   В командной строке от Администратора выполните:" -ForegroundColor Gray
Write-Host "     netsh winhttp reset proxy" -ForegroundColor Cyan
Write-Host "     ipconfig /flushdns" -ForegroundColor Cyan
Write-Host ""
Write-Host "3. СИНХРОНИЗИРУЙТЕ СИСТЕМНОЕ ВРЕМЯ WINDOWS:" -ForegroundColor White
Write-Host "   Откройте 'Параметры -> Время и язык -> Дата и время' и нажмите кнопку 'Синхронизировать'." -ForegroundColor Gray
Write-Host ""
Write-Host "4. ДЛЯ ОБХОДА БЛОКИРОВОК ИГРЫ ИСПОЛЬЗУЙТЕ СЕЛЕКТИВНЫЙ МАРШРУТИЗАТОР WARLINK:" -ForegroundColor White
Write-Host "   WarLink направляет через европейский шлюз исключительно трафик самой игры WARDOGS," -ForegroundColor Gray
Write-Host "   не трогает лаунчер (предотвращая WD-L004), не ломает браузер и не конфликтует с античитом Elytra." -ForegroundColor Gray
Write-Host "   Скачать: https://github.com/max-alekseyev/WarLink/releases/latest" -ForegroundColor Cyan
Write-Host ""

if ($OutputFile) {
    try {
        $reportLines | Out-File -FilePath $OutputFile -Encoding utf8
        Write-Host "[OK] Полный отчет сохранен в файл: $OutputFile" -ForegroundColor Green
    } catch {
        Write-Host "[WARN] Не удалось записать отчет в файл $OutputFile`: $_" -ForegroundColor Yellow
    }
}

Write-Host "==============================================================================" -ForegroundColor Cyan
Write-Host "Диагностика завершена." -ForegroundColor White
