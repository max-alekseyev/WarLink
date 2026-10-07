//go:build windows

package system

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

// ApplyCompetitiveGamingTweaks optimizes the Windows networking stack for low-latency
// competitive gaming, eliminating multimedia throttling and enabling DSCP 46 Expedited Forwarding.
func ApplyCompetitiveGamingTweaks(logFn func(string)) {
	if logFn == nil {
		logFn = func(string) {}
	}

	// 1. Multimedia & Network Throttling Elimination
	// Disables 10,000 pps limit and gives 100% responsiveness to real-time network interrupts
	if err := setDwordValue(
		registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion\Multimedia\SystemProfile`,
		"NetworkThrottlingIndex",
		0xFFFFFFFF,
	); err == nil {
		_ = setDwordValue(
			registry.LOCAL_MACHINE,
			`SOFTWARE\Microsoft\Windows NT\CurrentVersion\Multimedia\SystemProfile`,
			"SystemResponsiveness",
			0x00000000,
		)
		logFn("[SYS] Троттлинг сетевых прерываний Windows отключен (NetworkThrottlingIndex = 0xFFFFFFFF)")
	}

	// 2. Remove 20% QoS bandwidth reserve
	_ = setDwordValue(
		registry.LOCAL_MACHINE,
		`SOFTWARE\Policies\Microsoft\Windows\Psched`,
		"NonBestEffortLimit",
		0x00000000,
	)

	// 3. Bypass NLA (Network Location Awareness) bug for DSCP in home/public networks
	if err := setStringValue(
		registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Services\Tcpip\QoS`,
		"Do not use NLA",
		"1",
	); err == nil {
		_ = setDwordValue(
			registry.LOCAL_MACHINE,
			`SYSTEM\CurrentControlSet\Services\Tcpip\Parameters`,
			"DisableUserTOSSetting",
			0x00000000,
		)
		logFn("[SYS] Баг NLA устранен, аппаратная маркировка DSCP разблокирована в домашних сетях")
	}

	// 4. Winsock Fast Datagram Send Threshold (Afd Parameters)
	// Bypasses intermediate I/O request packet buffering for UDP datagrams under MTU
	if err := setDwordValue(
		registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Services\Afd\Parameters`,
		"FastSendDatagramThreshold",
		1500,
	); err == nil {
		logFn("[SYS] Включена прямая передача дейтаграмм Winsock (FastSendDatagramThreshold = 1500)")
	}

	// 5. Configure NetQoS Policy for WARDOGS and sing-box processes (DSCP 46 Expedited Forwarding)
	go applyNetQoSPolicies(logFn)
}

func setDwordValue(root registry.Key, path, name string, value uint32) error {
	k, _, err := registry.CreateKey(root, path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetDWordValue(name, value)
}

func setStringValue(root registry.Key, path, name, value string) error {
	k, _, err := registry.CreateKey(root, path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(name, value)
}

func applyNetQoSPolicies(logFn func(string)) {
	// Register QoS policy for WARDOGS client and sing-box tunnel daemon to mark UDP packets with DSCP 46 (EF)
	psScript := `
Get-NetQosPolicy -Name "WardogsQoS" -ErrorAction SilentlyContinue | Remove-NetQosPolicy -Confirm:$false -ErrorAction SilentlyContinue
New-NetQosPolicy -Name "WardogsQoS" -AppPathNameMatchCondition "WardogsClient-Win64-Shipping.exe" -IPProtocolMatchCondition UDP -DSCPAction 46 -NetworkProfile All -ErrorAction SilentlyContinue | Out-Null
Get-NetQosPolicy -Name "WarLinkTunnelQoS" -ErrorAction SilentlyContinue | Remove-NetQosPolicy -Confirm:$false -ErrorAction SilentlyContinue
New-NetQosPolicy -Name "WarLinkTunnelQoS" -AppPathNameMatchCondition "sing-box.exe" -IPProtocolMatchCondition UDP -DSCPAction 46 -NetworkProfile All -ErrorAction SilentlyContinue | Out-Null
`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if err := cmd.Run(); err == nil {
		logFn("[SYS] Назначена политика качества обслуживания QoS (DSCP 46 / Expedited Forwarding для WARDOGS и туннеля)")
	}
}
