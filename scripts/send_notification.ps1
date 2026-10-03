param(
    [string]$Title = "",
    [string]$Message = "",
    [ValidateSet("info", "update", "warning", "urgent")]
    [string]$Severity = "update",
    [ValidateSet("broadcast", "account", "device")]
    [string]$TargetType = "broadcast",
    [string]$TargetId = "",
    [string]$ActionLabel = "",
    [string]$ActionUrl = "",
    [int64]$DeleteId = 0,
    [switch]$List,
    [string]$SshKey = "$HOME\.ssh\id_ed25519",
    [string]$ServerHost = $(if ($env:WARLINK_SERVER_HOST) { $env:WARLINK_SERVER_HOST } elseif ([Environment]::GetEnvironmentVariable("WARLINK_SERVER_HOST", "User")) { [Environment]::GetEnvironmentVariable("WARLINK_SERVER_HOST", "User") } else { "127.0.0.1" })
)

$bindArgs = @()
try {
    $defIf = (Get-NetRoute -DestinationPrefix "0.0.0.0/0" -ErrorAction SilentlyContinue | Where-Object NextHop -ne "0.0.0.0" | Select-Object -First 1 -ExpandProperty ifIndex)
    if ($defIf) {
        $physIp = (Get-NetIPAddress -InterfaceIndex $defIf -AddressFamily IPv4 -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty IPAddress)
        if ($physIp) { $bindArgs = @("-b", $physIp) }
    }
} catch {}

if ($List) {
    Write-Host "Fetching active notifications from $ServerHost..." -ForegroundColor Cyan
    $res = ssh -i $SshKey $bindArgs -o StrictHostKeyChecking=no "root@$ServerHost" "curl -s http://127.0.0.1:8081/api/v1/admin/notifications"
    $res | ConvertFrom-Json | Select-Object -ExpandProperty notifications | Format-Table id, target_type, severity, title, action_label, created_at
    exit 0
}

if ($DeleteId -gt 0) {
    Write-Host "Deleting notification #$DeleteId on $ServerHost..." -ForegroundColor Yellow
    $res = ssh -i $SshKey -o StrictHostKeyChecking=no "root@$ServerHost" "curl -s -X DELETE 'http://127.0.0.1:8081/api/v1/admin/notifications?id=$DeleteId'"
    Write-Host "Response: $res" -ForegroundColor Green
    exit 0
}

if (-not $Title -or -not $Message) {
    Write-Host "Usage:" -ForegroundColor Yellow
    Write-Host "  .\scripts\send_notification.ps1 -List"
    Write-Host "  .\scripts\send_notification.ps1 -DeleteId <ID>"
    Write-Host "  .\scripts\send_notification.ps1 -Title '...' -Message '...' [-Severity update|info|warning|urgent] [-ActionLabel '...'] [-ActionUrl '#view-progression']"
    exit 1
}

$payload = @{
    title        = $Title
    message      = $Message
    severity     = $Severity
    target_type  = $TargetType
    target_id    = $TargetId
    action_label = $ActionLabel
    action_url   = $ActionUrl
} | ConvertTo-Json -Compress

Write-Host "Dispatching notification to $ServerHost..." -ForegroundColor Cyan
$b64 = [Convert]::ToBase64String([System.Text.Encoding]::UTF8.GetBytes($payload))
$remoteCmd = "echo $b64 | base64 -d | curl -s -X POST http://127.0.0.1:8081/api/v1/admin/notifications -H 'Content-Type: application/json' -d @-"
$res = ssh -i $SshKey $bindArgs -o StrictHostKeyChecking=no "root@$ServerHost" $remoteCmd
Write-Host "Response: $res" -ForegroundColor Green
