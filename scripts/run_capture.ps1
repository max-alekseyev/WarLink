param(
    [switch]$Help
)

$ErrorActionPreference = "Stop"

try {
    # Check Administrator privileges
    $currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
    if (-not $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        Write-Host "[UAC] Requesting Administrator privileges..." -ForegroundColor Yellow
        Start-Process powershell.exe -ArgumentList "-NoProfile -ExecutionPolicy Bypass -File `"$PSCommandPath`"" -Verb RunAs
        exit
    }

    $Host.UI.RawUI.WindowTitle = "WarLink 2.0 - WARDOGS Traffic Capture Session"

    Write-Host "==========================================================" -ForegroundColor Cyan
    Write-Host "   WARLINK 2.0 - WARDOGS NETWORK TRAFFIC CAPTURE SESSION   " -ForegroundColor Cyan
    Write-Host "==========================================================" -ForegroundColor Cyan

    $root = Split-Path -Parent $PSScriptRoot
    $sbExe = "$root\warlink_core\singbox\sing-box.exe"
    $sbDir = "$root\warlink_core\singbox"
    $logFile = "$sbDir\singbox.log"
    $cfgFile = "$sbDir\capture_config.json"

    # Clean old log
    if (Test-Path $logFile) {
        Remove-Item $logFile -Force
        Write-Host "[OK] Old capture log cleared." -ForegroundColor DarkGray
    }

    Write-Host "`n[1/3] Acquiring gateway session and building config..." -ForegroundColor Yellow
    Set-Location $root

    $serverIP = $env:WARLINK_SERVER_IP
    if (-not $serverIP -and (Test-Path "$root\warlink_core\config.json")) {
        try {
            $coreCfg = Get-Content "$root\warlink_core\config.json" -Raw | ConvertFrom-Json
            $serverIP = $coreCfg.server_ip
        } catch {}
    }
    if (-not $serverIP) {
        throw "Server IP not found. Set WARLINK_SERVER_IP environment variable or server_ip in warlink_core/config.json."
    }

    # Acquire session token and obfs dynamically via API helper
    $sessRaw = python "$root\scripts\get_token.py" "$serverIP"
    $sessObj = $sessRaw | ConvertFrom-Json
    $token = $sessObj.token
    $obfsPass = $sessObj.obfs
    $serverHost = $serverIP
    if ($serverEndpoint) {
        $epParts = $serverEndpoint.Split(':')
        if ($epParts[0] -and $epParts[0].Trim() -ne "") {
            $serverHost = $epParts[0].Trim()
        }
    }
    if (-not $token) {
        throw "Failed to acquire session token: $sessRaw"
    }
    Write-Host "[OK] Gateway session token successfully acquired." -ForegroundColor Green

    # Ensure gateway IP is excluded from Zapret WinDivert
    $zapretExclude = "$root\warlink_core\zapret\lists\ipset-exclude-user.txt"
    if (Test-Path $zapretExclude) {
        $excLines = @(Get-Content $zapretExclude | Where-Object { $_ -notmatch "# warlink-gateway" -and $_.Trim() -ne "" })
        $excLines += "$serverHost/32 # warlink-gateway"
        $excLines | Set-Content $zapretExclude -Encoding UTF8
        Write-Host "[OK] Gateway IP ($serverHost) added to Zapret WinDivert exclusion list." -ForegroundColor Green
    }

    # Fetch game profiles dynamically from server
    $targetProcs = @(
        "WardogsClient-Win64-Shipping.exe",
        "WardogsLauncher-Shipping.exe",
        "wardogs.exe",
        "wardogs-win64-shipping.exe",
        "wardogslauncher.exe",
        "elytra-launcher.exe",
        "elytraclient.exe",
        "crashpad_handler.exe",
        "service.exe",
        "control.exe",
        "Elytra-Setup.exe"
    )
    try {
        $profJson = (Invoke-RestMethod -Uri "http://$serverHost/api/v1/profiles" -TimeoutSec 3 -ErrorAction SilentlyContinue)
        if ($profJson.profiles) {
            foreach ($prof in $profJson.profiles) {
                if ($prof.id -eq "wardogs") {
                    foreach ($p in $prof.processes) {
                        if ($targetProcs -notcontains $p) { $targetProcs += $p }
                    }
                }
            }
            Write-Host "[OK] WARDOGS profile loaded from server ($($targetProcs.Count) target processes)." -ForegroundColor Green
        }
    } catch {}

    $procsFormatted = ($targetProcs | ForEach-Object { '          "' + $_ + '"' }) -join ",`n"

    $logPathClean = $logFile.Replace('\', '/')

    $cfgJson = @"
{
  "log": {
    "level": "info",
    "output": "$logPathClean",
    "timestamp": true
  },
  "dns": {
    "servers": [
      {
        "tag": "dns-fakeip",
        "type": "fakeip",
        "inet4_range": "198.18.0.0/15"
      },
      {
        "tag": "dns-remote",
        "type": "tcp",
        "server": "1.1.1.1",
        "detour": "hy2-stockholm"
      },
      {
        "tag": "dns-local",
        "type": "local",
        "detour": "direct"
      }
    ],
    "rules": [
      {
        "domain_suffix": [
          "digicert.com",
          "verisign.com",
          "sectigo.com",
          "globalsign.com",
          "identrust.com",
          "microsoft.com",
          "symcd.com",
          "entrust.net",
          "amazontrust.com",
          "letsencrypt.org"
        ],
        "server": "dns-local"
      },
      {
        "process_name": [
$procsFormatted
        ],
        "server": "dns-fakeip"
      },
      {
        "domain_suffix": [
          "pragmaengine.com",
          "wardogs.com",
          "bulkhead.net",
          "elytra.ac",
          "anybrain.gg",
          "vivox.com",
          "epicgames.dev",
          "dynamodb.eu-north-1.amazonaws.com",
          "steampowered.com",
          "steamcommunity.com",
          "steamstatic.com",
          "steamserver.net",
          "t.me",
          "telegram.org",
          "telegra.ph",
          "telegram.me",
          "telesco.pe",
          "tdesktop.com",
          "instagram.com",
          "cdninstagram.com",
          "facebook.com",
          "fbcdn.net",
          "threads.net",
          "whatsapp.com",
          "whatsapp.net",
          "x.com",
          "twitter.com",
          "twimg.com",
          "t.co"
        ],
        "server": "dns-fakeip"
      }
    ],
    "final": "dns-local"
  },
  "inbounds": [
    {
      "type": "tun",
      "tag": "tun-in",
      "interface_name": "WarLink-Tun",
      "address": [
        "172.19.0.1/30"
      ],
      "auto_route": true,
      "strict_route": false,
      "stack": "mixed"
    }
  ],
  "outbounds": [
    {
      "type": "hysteria2",
      "tag": "hy2-stockholm",
      "server": "$serverHost",
      "server_ports": [
        "443:443",
        "20000:30000"
      ],
      "hop_interval": "30s",
      "up_mbps": 50,
      "down_mbps": 100,
      "password": "$token",
      "obfs": {
        "type": "salamander",
        "password": "$obfsPass"
      },
      "tls": {
        "enabled": true,
        "server_name": "gateway.warlink.network",
        "insecure": true
      }
    },
    {
      "type": "direct",
      "tag": "direct"
    }
  ],
  "route": {
    "default_domain_resolver": "dns-local",
    "rules": [
      {
        "action": "sniff"
      },
      {
        "protocol": [
          "dns"
        ],
        "action": "hijack-dns"
      },
      {
        "ip_cidr": [
          "198.18.0.0/15"
        ],
        "outbound": "hy2-stockholm"
      },
      {
        "process_name": [
          "sing-box.exe",
          "winws2.exe",
          "winws.exe"
        ],
        "outbound": "direct"
      },
      {
        "ip_cidr": [
          "127.0.0.0/8",
          "::1/128",
          "10.0.0.0/8",
          "172.16.0.0/12",
          "192.168.0.0/16",
          "169.254.0.0/16",
          "fc00::/7",
          "fe80::/10"
        ],
        "outbound": "direct"
      },
      {
        "process_name": [
          "service.exe",
          "control.exe",
          "Elytra-Setup.exe",
          "crashpad_handler.exe"
        ],
        "port": [
          80
        ],
        "outbound": "direct"
      },
      {
        "domain_suffix": [
          "digicert.com",
          "verisign.com",
          "sectigo.com",
          "globalsign.com",
          "identrust.com",
          "microsoft.com",
          "symcd.com",
          "entrust.net",
          "amazontrust.com",
          "letsencrypt.org"
        ],
        "port": [
          80
        ],
        "outbound": "direct"
      },
      {
        "process_name": [
$procsFormatted
        ],
        "outbound": "hy2-stockholm"
      }
    ],
    "final": "direct",
    "find_process": true,
    "auto_detect_interface": true
  }
}
"@

    $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($cfgFile, $cfgJson, $utf8NoBom)
    Write-Host "[OK] Sing-box configuration written (UTF-8 no BOM): $cfgFile" -ForegroundColor Green

    # Validate config
    & $sbExe check -c $cfgFile
    if ($LASTEXITCODE -ne 0) {
        throw "Sing-box configuration validation failed."
    }

    Write-Host "`n[2/3] Starting sing-box TUN gateway..." -ForegroundColor Yellow
    # Kill any existing sing-box
    Get-Process sing-box -ErrorAction SilentlyContinue | Stop-Process -Force
    $errLog = "$sbDir\singbox_stderr.log"
    $outLog = "$sbDir\singbox_stdout.log"
    if (Test-Path $errLog) { Remove-Item $errLog -Force }
    if (Test-Path $outLog) { Remove-Item $outLog -Force }

    $proc = Start-Process -FilePath $sbExe -ArgumentList "run", "-c", "capture_config.json" -WorkingDirectory $sbDir -RedirectStandardError $errLog -RedirectStandardOutput $outLog -PassThru -WindowStyle Hidden
    Start-Sleep -Seconds 1

    if ($proc.HasExited) {
        $detail = ""
        if (Test-Path $errLog) { $detail += (Get-Content $errLog -Raw) }
        if (Test-Path $outLog) { $detail += "`n" + (Get-Content $outLog -Raw) }
        throw "sing-box exited with code $($proc.ExitCode)! Details:`n$detail"
    }
    Write-Host "[OK] WarLink Gateway is ACTIVE (PID $($proc.Id))." -ForegroundColor Green
    Write-Host "[OK] All WARDOGS game process traffic is routed to Stockholm." -ForegroundColor Green

    Write-Host "`n----------------------------------------------------------" -ForegroundColor Cyan
    Write-Host "INSTRUCTIONS FOR WARDOGS MATCH:" -ForegroundColor White
    Write-Host "1. Open Steam and start WARDOGS." -ForegroundColor Yellow
    Write-Host "2. Log into account, wait for lobby to load." -ForegroundColor Yellow
    Write-Host "3. Ensure in-game voice chat (Vivox) is enabled." -ForegroundColor Yellow
    Write-Host "4. Find and play 1 match (5-10 minutes)." -ForegroundColor Yellow
    Write-Host "5. Exit the game." -ForegroundColor Yellow
    Write-Host "6. Return to this console window and press ENTER." -ForegroundColor Cyan
    Write-Host "----------------------------------------------------------`n" -ForegroundColor Cyan

    Read-Host "Press ENTER after exiting the game..."

    Write-Host "`n[3/3] Stopping sing-box and analyzing traffic log..." -ForegroundColor Yellow
    if (-not $proc.HasExited) {
        Stop-Process -Id $proc.Id -Force
    }

    Start-Sleep -Seconds 1
    python "$root\scripts\analyze_capture.py" "$logFile"

    Write-Host "`nCapture completed! You can copy the results above." -ForegroundColor Green
    Read-Host "Press ENTER to exit..."
}
catch {
    Write-Host "`n[CRITICAL ERROR]: $($_.Exception.Message)" -ForegroundColor Red
    Write-Host $_.ScriptStackTrace -ForegroundColor DarkRed
    Read-Host "`nPress ENTER to exit..."
}
