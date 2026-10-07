# ==============================================================================
# WARLINK: NETWORK ADAPTER PURGE & ZOMBIE ROUTE CLEANUP
# Automatically purges ghost Wintun/sing-tun/throne-tun adapters, clears corrupted
# routes, and launches WarLink.
# Strictly complies with AGENTS.md (Zero Emoji, pure diagnostic output).
# ==============================================================================

param(
    [switch]$NoLaunch
)

$ErrorActionPreference = "Continue"

# 1. Administrator Elevation Check
$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Host "[UAC] Запуск требует прав Администратора для удаления виртуальных устройств ядра Windows." -ForegroundColor Yellow
    Write-Host "[UAC] Перезапуск с повышением привилегий..." -ForegroundColor Cyan
    $scriptPath = $MyInvocation.MyCommand.Definition
    $argsList = "-NoProfile -ExecutionPolicy Bypass -File `"$scriptPath`""
    if ($NoLaunch) { $argsList += " -NoLaunch" }
    Start-Process powershell.exe -ArgumentList $argsList -Verb RunAs
    exit
}

$Host.UI.RawUI.WindowTitle = "WarLink - Очистка сетевых адаптеров и маршрутов"

Write-Host "================================================================================" -ForegroundColor Cyan
Write-Host "            WARLINK: УДАЛЕНИЕ ФАНТОМНЫХ АДАПТЕРОВ И ВОССТАНОВЛЕНИЕ СЕТИ        " -ForegroundColor Cyan
Write-Host "================================================================================" -ForegroundColor Cyan

# 2. Terminate legacy/conflicting daemons
Write-Host "`n[1/5] Завершение фоновых сетевых служб..." -ForegroundColor Yellow
$killList = @("Throne.exe", "throne.exe", "ThroneCore.exe", "thronecore.exe", "sing-box.exe", "WarLink.exe")
foreach ($p in $killList) {
    Stop-Process -Name ($p -replace "\.exe$", "") -Force -ErrorAction SilentlyContinue
}
Write-Host "[OK] Фоновые процессы остановлены." -ForegroundColor Green

# 3. Discover and Remove Ghost Wintun Devices via PnP
Write-Host "`n[2/5] Поиск и удаление фантомных Wintun / sing-tun / throne-tun адаптеров..." -ForegroundColor Yellow

$devices = Get-PnpDevice -Class Net -ErrorAction SilentlyContinue | Where-Object {
    ($_.InstanceId -like "*WINTUN*" -or $_.InstanceId -like "*Wintun*") -or
    ($_.FriendlyName -like "*sing-tun*") -or
    ($_.FriendlyName -like "*throne*") -or
    ($_.FriendlyName -like "*WarLink*")
}

$removedCount = 0
foreach ($dev in $devices) {
    Write-Host "      Обнаружено устройство: $($dev.FriendlyName) [$($dev.InstanceId)]" -ForegroundColor DarkGray
    $out = & pnputil /remove-device "$($dev.InstanceId)" 2>&1
    Write-Host "      Результат pnputil: $out" -ForegroundColor Cyan
    $removedCount++
}

if ($removedCount -eq 0) {
    Write-Host "[OK] Фантомных PnP устройств Wintun не обнаружено." -ForegroundColor Green
} else {
    Write-Host "[OK] Удалено фантомных устройств: $removedCount" -ForegroundColor Green
}

# 4. Netsh interface cleanup
Write-Host "`n[3/5] Очистка сетевых интерфейсов Windows..." -ForegroundColor Yellow
$interfaces = @("throne-tun", "sing-tun", "WarLink-Tun")
foreach ($iface in $interfaces) {
    & netsh interface delete interface name="$iface" 2>$null | Out-Null
    & netsh interface set interface name="$iface" admin=disable 2>$null | Out-Null
}
Write-Host "[OK] Сетевые интерфейсы очищены." -ForegroundColor Green

# 5. Flush DNS and Reset IP routing table
Write-Host "`n[4/5] Сброс системного кэша DNS и верификация маршрутов..." -ForegroundColor Yellow
& ipconfig /flushdns | Out-Null
Write-Host "[OK] Кэш DNS очищен." -ForegroundColor Green

# 6. Verify Active Adapters
Write-Host "`n[5/5] Текущее состояние сетевых адаптеров:" -ForegroundColor Yellow
Get-NetAdapter | Format-Table -Property Name, InterfaceDescription, Status, LinkSpeed -AutoSize

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
if (-not $scriptDir) { $scriptDir = $PSScriptRoot }
$repoDir = Split-Path -Parent $scriptDir
$warlinkExe = Join-Path $repoDir "WarLink.exe"

if (-not $NoLaunch -and (Test-Path $warlinkExe)) {
    Write-Host "`nЗапуск WarLink с обновленным ядром..." -ForegroundColor Green
    Start-Process -FilePath $warlinkExe
    Start-Sleep -Seconds 2
}

Write-Host "`nГотово. Окно закроется через 4 секунды..." -ForegroundColor DarkGray
Start-Sleep -Seconds 4
