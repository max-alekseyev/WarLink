param (
    [string]$GatewayIP = $env:WARLINK_SERVER_IP,
    [string]$GatewayPorts = $env:WARLINK_SERVER_PORTS,
    [string]$ObfsPassword = $env:WARLINK_OBFS_PASSWORD,
    [string]$HMACSecret = $env:WARLINK_HMAC_SECRET,
    [string]$UpdateRepo = $env:WARLINK_UPDATE_REPO
)

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

if (-not $GatewayIP -and (Test-Path "$root\warlink_core\config.json")) {
    try {
        $cfg = Get-Content "$root\warlink_core\config.json" -Raw | ConvertFrom-Json
        $GatewayIP = $cfg.server_ip
    } catch {}
}

if (-not $GatewayIP) {
    Write-Host "[WARN] GatewayIP не указан. Бинарник будет требовать WARLINK_SERVER_IP или config.json во время работы." -ForegroundColor Yellow
} else {
    Write-Host "[INFO] Сборка с зашитым адресом шлюза: $GatewayIP" -ForegroundColor Cyan
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

Write-Host "[BUILD] Компиляция dist\WarLink.exe с флагом -trimpath..." -ForegroundColor Yellow
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$cmd = "go build -trimpath -tags release -ldflags `"$ldflags`" -o dist\WarLink.exe ."
Invoke-Expression $cmd

if ($LASTEXITCODE -eq 0) {
    $sizeMb = [math]::Round((Get-Item "$outDir\WarLink.exe").Length / 1MB, 2)
    Write-Host "[OK] Релизный бинарник собран: dist\WarLink.exe ($sizeMb MB)" -ForegroundColor Green
} else {
    Write-Host "[ERR] Ошибка компиляции WarLink.exe" -ForegroundColor Red
    exit 1
}
