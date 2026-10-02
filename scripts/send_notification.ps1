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
    [string]$ServerHost = $(if ($env:WARLINK_SERVER_HOST) { $env:WARLINK_SERVER_HOST } else { "127.0.0.1" })
)

if ($List) {
    Write-Host "Fetching active notifications from $ServerHost..." -ForegroundColor Cyan
    $res = ssh -i $SshKey -o StrictHostKeyChecking=no "root@$ServerHost" "curl -s http://127.0.0.1:8081/api/v1/admin/notifications"
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

$escapedPayload = $payload.Replace('"', '\"')
$remoteCmd = "curl -s -X POST http://127.0.0.1:8081/api/v1/admin/notifications -H 'Content-Type: application/json' -d '$escapedPayload'"

Write-Host "Dispatching notification to $ServerHost..." -ForegroundColor Cyan
$res = ssh -i $SshKey -o StrictHostKeyChecking=no "root@$ServerHost" $remoteCmd
Write-Host "Response: $res" -ForegroundColor Green
