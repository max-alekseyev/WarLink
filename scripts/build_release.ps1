param (
    [string]$GatewayIP = $env:WARLINK_SERVER_IP,
    [string]$GatewayPorts = $env:WARLINK_SERVER_PORTS,
    [string]$ObfsPassword = $env:WARLINK_OBFS_PASSWORD,
    [string]$HMACSecret = $env:WARLINK_HMAC_SECRET,
    [string]$UpdateRepo = $env:WARLINK_UPDATE_REPO
)

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

if (Test-Path "$root\warlink_core\config.json") {
    try {
        $cfg = Get-Content "$root\warlink_core\config.json" -Raw | ConvertFrom-Json
        if (-not $GatewayIP) { $GatewayIP = $cfg.server_ip }
        if (-not $HMACSecret) { $HMACSecret = $cfg.hmac_secret }
        if (-not $ObfsPassword) { $ObfsPassword = $cfg.obfs_password }
    } catch {}
}

if (-not $GatewayIP) {
    Write-Host "[WARN] GatewayIP not specified. Using WARLINK_SERVER_IP or config.json at runtime." -ForegroundColor Yellow
} else {
    Write-Host "[INFO] Building with embedded gateway IP: $GatewayIP" -ForegroundColor Cyan
}

$outDir = "$root\dist"
if (-not (Test-Path $outDir)) {
    New-Item -ItemType Directory -Path $outDir | Out-Null
}

$ldflags = "-H windowsgui -s -w"
if ($GatewayIP) {
    $ldflags += " -X warlink/internal/singbox.DefaultServerIP=$GatewayIP -X warlink/internal/singbox.DefaultServerAPI=http://$GatewayIP -X warlink/internal/config.DefaultServerIP=$GatewayIP"
}
if ($GatewayPorts) {
    $ldflags += " -X warlink/internal/singbox.DefaultServerPorts=$GatewayPorts"
}
if ($ObfsPassword) {
    $ldflags += " -X warlink/internal/singbox.DefaultObfsPassword=$ObfsPassword"
}
if ($HMACSecret) {
    $ldflags += " -X warlink/internal/singbox.DefaultHMACSecret=$HMACSecret"
}
if ($UpdateRepo) {
    $ldflags += " -X warlink/internal/updater.UpdateRepo=$UpdateRepo"
}

Write-Host "[BUILD] Compiling dist\WarLink.exe with -trimpath..." -ForegroundColor Yellow
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$cmd = "go build -trimpath -tags release -ldflags `"$ldflags`" -o dist\WarLink.exe ."
Invoke-Expression $cmd

if ($LASTEXITCODE -eq 0) {
    $sizeMb = [math]::Round((Get-Item "$outDir\WarLink.exe").Length / 1MB, 2)
    Write-Host "[OK] Release binary built: dist\WarLink.exe ($sizeMb MB)" -ForegroundColor Green
    Copy-Item -Force "$outDir\WarLink.exe" "$root\WarLink.exe"
    Write-Host "[OK] Synced to root: WarLink.exe" -ForegroundColor Green
} else {
    Write-Host "[ERR] Failed to build WarLink.exe" -ForegroundColor Red
    exit 1
}

