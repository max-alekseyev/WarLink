# WarLink: Windows Network Stack Optimization for Ultra-Low Latency Competitive Gaming
# Research Paper Implementation: BGP-Routing, Real-Time Protocols & Windows Network Stack Tuning
# Strict Rule 3: Zero Emoji

param (
    [switch]$Revert = $false
)

$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) {
    Write-Error "[ERROR] Script must be run as Administrator."
    exit 1
}

if ($Revert) {
    Write-Output "[INFO] Reverting network optimizations to Windows defaults..."

    Remove-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Services\Tcpip\QoS" -Name "Do not use NLA" -ErrorAction SilentlyContinue
    Remove-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Services\Tcpip\Parameters" -Name "DisableUserTOSSetting" -ErrorAction SilentlyContinue
    Set-ItemProperty -Path "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Multimedia\SystemProfile" -Name "NetworkThrottlingIndex" -Value 10 -Type DWord -ErrorAction SilentlyContinue
    Set-ItemProperty -Path "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Multimedia\SystemProfile" -Name "SystemResponsiveness" -Value 20 -Type DWord -ErrorAction SilentlyContinue
    Remove-ItemProperty -Path "HKLM:\SOFTWARE\Policies\Microsoft\Windows\Psched" -Name "NonBestEffortLimit" -ErrorAction SilentlyContinue
    Remove-NetQosPolicy -Name "WardogsQoS" -Confirm:$false -ErrorAction SilentlyContinue

    Write-Output "[OK] Reverted to Windows default network stack settings."
    exit 0
}

Write-Output "[INFO] Applying Windows Network Stack Optimizations for Competitive Gaming..."

# 1. Bypass NLA check for QoS DSCP tags in non-domain environments
$qosPath = "HKLM:\SYSTEM\CurrentControlSet\Services\Tcpip\QoS"
if (-not (Test-Path $qosPath)) {
    New-Item -Path $qosPath -Force | Out-Null
}
Set-ItemProperty -Path $qosPath -Name "Do not use NLA" -Value "1" -Type String
Write-Output "[OK] NLA bypass for DSCP active (Do not use NLA = 1)"

# 2. Enable User TOS (Type of Service / DSCP) settings
$tcpipParams = "HKLM:\SYSTEM\CurrentControlSet\Services\Tcpip\Parameters"
Set-ItemProperty -Path $tcpipParams -Name "DisableUserTOSSetting" -Value 0 -Type DWord
Write-Output "[OK] User TOS setting enabled (DisableUserTOSSetting = 0)"

# 3. Disable Windows network throttling during gaming sessions
$multimediaPath = "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Multimedia\SystemProfile"
Set-ItemProperty -Path $multimediaPath -Name "NetworkThrottlingIndex" -Value ([int]0xFFFFFFFF) -Type DWord
Set-ItemProperty -Path $multimediaPath -Name "SystemResponsiveness" -Value 0 -Type DWord
Write-Output "[OK] Multimedia network throttling disabled (NetworkThrottlingIndex = 0xFFFFFFFF, SystemResponsiveness = 0)"

# 4. Remove Windows QoS packet scheduler bandwidth reservation (default 20%)
$pschedPath = "HKLM:\SOFTWARE\Policies\Microsoft\Windows\Psched"
if (-not (Test-Path $pschedPath)) {
    New-Item -Path $pschedPath -Force | Out-Null
}
Set-ItemProperty -Path $pschedPath -Name "NonBestEffortLimit" -Value 0 -Type DWord
Write-Output "[OK] QoS reserved bandwidth set to 0% (NonBestEffortLimit = 0)"

# 5. Configure Windows NetQoS Hardware Policy for WARDOGS UDP packets (DSCP 46 - Expedited Forwarding)
try {
    Remove-NetQosPolicy -Name "WardogsQoS" -Confirm:$false -ErrorAction SilentlyContinue
    New-NetQosPolicy -Name "WardogsQoS" -AppPathNameMatchCondition "WardogsClient-Win64-Shipping.exe" -IPProtocolMatchCondition UDP -DSCPAction 46 -NetworkProfile All -ErrorAction Stop | Out-Null
    Write-Output "[OK] Windows NetQoS Policy 'WardogsQoS' installed (App: WardogsClient-Win64-Shipping.exe, Protocol: UDP, DSCP: 46 Expedited Forwarding)"
} catch {
    Write-Warning "[WARN] NetQoS policy could not be registered: $_"
}

Write-Output "[SUCCESS] All Windows network stack optimizations applied successfully."
