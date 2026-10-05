package troubleshooter

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"warlink/internal/singbox"
)

// CheckResult represents the outcome of a single diagnostic check.
type CheckResult struct {
	ID         string `json:"id"`
	Category   string `json:"category"` // "driver", "network", "dns", "gateway"
	Title      string `json:"title"`
	Status     string `json:"status"` // "ok", "warning", "error"
	Message    string `json:"message"`
	Detail     string `json:"detail,omitempty"`
	CanAutoFix bool   `json:"can_auto_fix"`
}

// DiagnosticReport contains the full suite of diagnostic checks.
type DiagnosticReport struct {
	Timestamp     string        `json:"timestamp"`
	OverallStatus string        `json:"overall_status"` // "healthy", "warning", "critical"
	Checks        []CheckResult `json:"checks"`
	SummaryText   string        `json:"summary_text"`
}

// RunFullDiagnostics executes all diagnostic tests and returns a comprehensive report.
func RunFullDiagnostics() DiagnosticReport {
	report := DiagnosticReport{
		Timestamp:     time.Now().Format("2006-01-02 15:04:05"),
		OverallStatus: "healthy",
		Checks:        make([]CheckResult, 0, 8),
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	addCheck := func(res CheckResult) {
		mu.Lock()
		defer mu.Unlock()
		report.Checks = append(report.Checks, res)
		if res.Status == "error" {
			report.OverallStatus = "critical"
		} else if res.Status == "warning" && report.OverallStatus != "critical" {
			report.OverallStatus = "warning"
		}
	}

	// 1. Driver & Service checks (WinDivert, Wintun, third-party conflicts)
	wg.Add(1)
	go func() {
		defer wg.Done()
		res := checkDriversAndServices()
		for _, r := range res {
			addCheck(r)
		}
	}()

	// 2. DNS resolution check for game domains
	wg.Add(1)
	go func() {
		defer wg.Done()
		res := checkDNSResolution()
		for _, r := range res {
			addCheck(r)
		}
	}()

	// 3. Gateway and Edge Nodes reachability & RTT
	wg.Add(1)
	go func() {
		defer wg.Done()
		res := checkGatewaysReachability()
		for _, r := range res {
			addCheck(r)
		}
	}()

	wg.Wait()

	// Generate plain-text summary suitable for support tickets
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== ОТЧЕТ ДИАГНОСТИКИ СЕТИ WARLINK [%s] ===\n", report.Timestamp))
	sb.WriteString(fmt.Sprintf("Общее состояние: %s\n\n", strings.ToUpper(report.OverallStatus)))
	for _, c := range report.Checks {
		tag := "[OK]"
		if c.Status == "warning" {
			tag = "[WARN]"
		} else if c.Status == "error" {
			tag = "[FAIL]"
		}
		sb.WriteString(fmt.Sprintf("%s %s: %s\n", tag, c.Title, c.Message))
		if c.Detail != "" {
			sb.WriteString(fmt.Sprintf("       Детали: %s\n", c.Detail))
		}
	}
	report.SummaryText = sb.String()

	return report
}

// checkDNSResolution tests resolving key game and service endpoints.
func checkDNSResolution() []CheckResult {
	results := make([]CheckResult, 0, 2)
	domains := []struct {
		name string
		host string
	}{
		{"Авторизация WARDOGS", "faa.bulkhead.net"},
		{"Игровой бэкенд WARDOGS", "game.live.wardogs.bulkhead.pragmaengine.com"},
		{"Discord Gateway", "gateway.discord.gg"},
	}

	resolver := net.Resolver{
		PreferGo: true,
	}

	for _, d := range domains {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		start := time.Now()
		addrs, err := resolver.LookupHost(ctx, d.host)
		elapsed := time.Since(start)
		cancel()

		res := CheckResult{
			ID:       "dns_" + strings.ReplaceAll(d.host, ".", "_"),
			Category: "dns",
			Title:    "DNS: " + d.name,
		}

		if err != nil {
			res.Status = "error"
			res.Message = "Сбой разрешения имени"
			res.Detail = "DNS-сервер не ответил или заблокирован"
			res.CanAutoFix = true
		} else if len(addrs) == 0 {
			res.Status = "warning"
			res.Message = "Пустой ответ DNS сервера"
			res.Detail = "Сервер DNS вернул пустой список записей"
			res.CanAutoFix = true
		} else {
			res.Status = "ok"
			res.Message = fmt.Sprintf("Успешно (%d мс)", elapsed.Milliseconds())
			res.Detail = fmt.Sprintf("Штатное разрешение адресов (записей: %d)", len(addrs))
		}
		results = append(results, res)
	}

	return results
}

// checkGatewaysReachability measures reachability and application-layer RTT to core cluster nodes.
func checkGatewaysReachability() []CheckResult {
	results := make([]CheckResult, 0, 3)

	masterAPI := singbox.GetServerAPI()
	masterHost := strings.TrimPrefix(strings.TrimPrefix(masterAPI, "https://"), "http://")
	if idx := strings.Index(masterHost, ":"); idx != -1 {
		masterHost = masterHost[:idx]
	}
	if idx := strings.Index(masterHost, "/"); idx != -1 {
		masterHost = masterHost[:idx]
	}

	nodes := []struct {
		id   string
		name string
		url  string
		tcp  string
	}{
		{"gw_frankfurt", "Шлюз Франкфурт (Европа)", "https://" + singbox.FrankfurtEdgeIP + "/", singbox.FrankfurtEdgeIP + ":443"},
		{"gw_moscow", "Шлюз Москва (Транзит)", "http://" + singbox.MoscowIngressIP + "/", singbox.MoscowIngressIP + ":80"},
		{"gw_master", "Узел управления (Master API)", masterAPI + "/api/v1/status", masterHost + ":80"},
	}

	httpClient := &http.Client{
		Timeout: 2500 * time.Millisecond,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
			DisableKeepAlives: true,
		},
	}

	for _, n := range nodes {
		res := CheckResult{
			ID:       n.id,
			Category: "gateway",
			Title:    n.name,
		}

		start := time.Now()
		var pingMs int
		resp, err := httpClient.Get(n.url)
		if err == nil {
			_ = resp.Body.Close()
			pingMs = int(time.Since(start).Milliseconds())
		} else {
			// Fallback: TCP Dial
			startTCP := time.Now()
			conn, tcpErr := net.DialTimeout("tcp", n.tcp, 2000*time.Millisecond)
			if tcpErr == nil {
				_ = conn.Close()
				pingMs = int(time.Since(startTCP).Milliseconds())
				err = nil
			}
		}

		if err != nil {
			res.Status = "error"
			res.Message = "Узел недоступен по сети"
			res.Detail = "Превышено время ожидания ответа сервера"
		} else {
			if pingMs < 1 {
				pingMs = 1
			}
			res.Status = "ok"
			res.Message = fmt.Sprintf("Доступен (задержка %d мс)", pingMs)
			res.Detail = fmt.Sprintf("Штатный отклик сервера, задержка %d мс", pingMs)
		}
		results = append(results, res)
	}

	return results
}

// FixIssuesResult records the outcome of 1-click remediation.
type FixIssuesResult struct {
	Success   bool     `json:"success"`
	Actions   []string `json:"actions"`
	Timestamp string   `json:"timestamp"`
}

// FixAllIssues performs safe 1-click cleanup of Windows network stack, DNS and driver state.
func FixAllIssues() FixIssuesResult {
	res := FixIssuesResult{
		Success:   true,
		Actions:   make([]string, 0),
		Timestamp: time.Now().Format("2006-01-02 15:04:05"),
	}

	// 1. Flush DNS resolver cache
	cmdFlush := exec.Command("ipconfig", "/flushdns")
	cmdFlush.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmdFlush.Run(); err == nil {
		res.Actions = append(res.Actions, "Кэш DNS успешно очищен (ipconfig /flushdns)")
	} else {
		res.Actions = append(res.Actions, "Предупреждение: сброс кэша DNS завершился с кодом "+err.Error())
	}

	// 2. Platform-specific driver & adapter healing
	platformActions := fixPlatformIssues()
	res.Actions = append(res.Actions, platformActions...)

	return res
}

// QuickHTTPPing tests basic egress HTTP connectivity.
func QuickHTTPPing(url string) (int64, error) {
	client := http.Client{Timeout: 2 * time.Second}
	start := time.Now()
	resp, err := client.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return time.Since(start).Milliseconds(), nil
}
