package aclgen

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Profile represents a single game or service traffic specification.
type Profile struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Processes []string `json:"processes"`
	Domains   []string `json:"domains"`
	IPs       []string `json:"ips"`
	UDPRanges []string `json:"udp_ranges"`
}

// BannedPrivateCIDRs contains private and special purpose networks that must never be in whitelist.
var BannedPrivateCIDRs = []string{
	"127.0.0.0/8",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.0.0/16",
	"224.0.0.0/4",
	"240.0.0.0/4",
	"0.0.0.0/0",
}

// LoadProfiles reads and validates all *.txt list files in the specified directory.
func LoadProfiles(dir string) ([]Profile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read lists directory %s: %w", dir, err)
	}

	var profiles []Profile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}

		filePath := filepath.Join(dir, entry.Name())
		profile, err := parseProfileFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("validation failed in %s: %w", entry.Name(), err)
		}
		profiles = append(profiles, profile)
	}

	if len(profiles) == 0 {
		return nil, fmt.Errorf("no valid profile .txt files found in %s", dir)
	}

	return profiles, nil
}

func parseProfileFile(filePath string) (Profile, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return Profile{}, err
	}
	defer file.Close()

	base := filepath.Base(filePath)
	id := strings.TrimSuffix(base, "-hosts.txt")
	id = strings.TrimSuffix(id, ".txt")

	p := Profile{
		ID:        id,
		Name:      id,
		Processes: make([]string, 0),
		Domains:   make([]string, 0),
		IPs:       make([]string, 0),
		UDPRanges: make([]string, 0),
	}

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		idx := strings.Index(line, ":")
		if idx == -1 {
			return p, fmt.Errorf("line %d: invalid format (expected key: value, got '%s')", lineNum, line)
		}

		key := strings.ToLower(strings.TrimSpace(line[:idx]))
		val := strings.TrimSpace(line[idx+1:])

		// Check for forbidden wildcard keywords
		valLower := strings.ToLower(val)
		if valLower == "all" || valLower == "*" || strings.HasPrefix(valLower, "all,") || strings.HasPrefix(valLower, "*,") {
			return p, fmt.Errorf("line %d: forbidden wildcard 'all' or '*' in value '%s'", lineNum, val)
		}

		switch key {
		case "name":
			if val != "" {
				p.Name = val
			}

		case "process":
			val = strings.ToLower(val)
			if strings.Contains(val, " ") || strings.Contains(val, "/") || strings.Contains(val, "\\") {
				return p, fmt.Errorf("line %d: invalid process name '%s'", lineNum, val)
			}
			p.Processes = append(p.Processes, val)

		case "domain":
			val = strings.ToLower(val)
			if err := validateDomain(val); err != nil {
				return p, fmt.Errorf("line %d: %w", lineNum, err)
			}
			p.Domains = append(p.Domains, val)

		case "ip":
			if err := validateIPorCIDR(val); err != nil {
				return p, fmt.Errorf("line %d: %w", lineNum, err)
			}
			p.IPs = append(p.IPs, val)

		case "udp":
			if err := validateUDPRange(val); err != nil {
				return p, fmt.Errorf("line %d: %w", lineNum, err)
			}
			p.UDPRanges = append(p.UDPRanges, val)

		default:
			return p, fmt.Errorf("line %d: unknown key '%s'", lineNum, key)
		}
	}

	if err := scanner.Err(); err != nil {
		return p, err
	}

	return p, nil
}

func validateDomain(d string) error {
	if strings.Contains(d, " ") || strings.Contains(d, "/") || strings.Contains(d, ":") {
		return fmt.Errorf("invalid domain '%s'", d)
	}
	if strings.HasPrefix(d, ".") || strings.HasSuffix(d, ".") {
		return fmt.Errorf("domain cannot start or end with dot: '%s'", d)
	}
	return nil
}

func validateIPorCIDR(target string) error {
	filter := ""
	if strings.Contains(target, ",") {
		parts := strings.SplitN(target, ",", 2)
		target = strings.TrimSpace(parts[0])
		filter = strings.TrimSpace(parts[1])
		if strings.Contains(filter, "all") || strings.Contains(filter, "*") {
			return fmt.Errorf("filter '%s' contains forbidden wildcard", filter)
		}
	}

	// Forbidden open ports for all
	if strings.HasPrefix(target, "53") || strings.HasPrefix(target, "80") || strings.HasPrefix(target, "443") {
		return fmt.Errorf("broad port '%s' without IP address is forbidden", target)
	}

	if strings.Contains(target, "/") {
		// CIDR
		ip, ipNet, err := net.ParseCIDR(target)
		if err != nil {
			return fmt.Errorf("invalid CIDR '%s': %w", target, err)
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("private/internal CIDR '%s' is not allowed in public whitelist", target)
		}
		ones, _ := ipNet.Mask.Size()
		if ones < 12 {
			return fmt.Errorf("CIDR mask /%d in '%s' is too broad (minimum /12 allowed)", ones, target)
		}
		return nil
	}

	// Single IP
	ip := net.ParseIP(target)
	if ip == nil {
		return fmt.Errorf("invalid IP address '%s'", target)
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("private/internal IP '%s' is not allowed in public whitelist", target)
	}
	return nil
}

func validateUDPRange(val string) error {
	// Must be single port (e.g. "50000") or range (e.g. "54000-55000")
	if strings.Contains(val, "-") {
		parts := strings.Split(val, "-")
		if len(parts) != 2 {
			return fmt.Errorf("invalid UDP range syntax '%s'", val)
		}
		start, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		end, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil {
			return fmt.Errorf("non-numeric UDP range '%s'", val)
		}
		if start < 1 || start > 65535 || end < 1 || end > 65535 || start > end {
			return fmt.Errorf("out-of-bounds UDP range '%s'", val)
		}
		// Strict guard: max span is 1500 ports
		span := end - start + 1
		if span > 1500 {
			return fmt.Errorf("UDP range '%s' span (%d ports) exceeds safety maximum of 1500 ports", val, span)
		}
		// Deny dangerous standard ports in ranges
		if start <= 53 && end >= 53 {
			return fmt.Errorf("UDP port 53 cannot be opened via dynamic ranges")
		}
		return nil
	}

	port, err := strconv.Atoi(val)
	if err != nil {
		return fmt.Errorf("invalid UDP port '%s': %w", val, err)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("UDP port %d out of bounds", port)
	}
	if port == 53 {
		return fmt.Errorf("UDP port 53 cannot be opened generically")
	}
	return nil
}

func parseUDPRangePorts(val string) ([]int, error) {
	val = strings.TrimSpace(val)
	if strings.Contains(val, "-") {
		parts := strings.Split(val, "-")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid UDP range syntax '%s'", val)
		}
		start, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		end, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("non-numeric UDP range '%s'", val)
		}
		if start > end {
			return nil, fmt.Errorf("inverted UDP range '%s' (start %d > end %d)", val, start, end)
		}
		if start < 1 || end > 65535 {
			return nil, fmt.Errorf("UDP port range '%s' out of valid bounds (1-65535)", val)
		}
		span := end - start + 1
		if span > 1500 {
			return nil, fmt.Errorf("UDP range '%s' span (%d ports) exceeds safety maximum of 1500 ports", val, span)
		}
		if start <= 53 && end >= 53 {
			return nil, fmt.Errorf("UDP port 53 cannot be opened via dynamic ranges")
		}
		ports := make([]int, span)
		for i := 0; i < span; i++ {
			ports[i] = start + i
		}
		return ports, nil
	}

	port, err := strconv.Atoi(val)
	if err != nil {
		return nil, fmt.Errorf("invalid UDP port '%s': %w", val, err)
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("UDP port %d out of bounds (1-65535)", port)
	}
	if port == 53 {
		return nil, fmt.Errorf("UDP port 53 cannot be opened generically")
	}
	return []int{port}, nil
}

// GenerateACL produces the full Hysteria 2 acl.txt content with strict zero-trust ordering.
func GenerateACL(profiles []Profile) (string, error) {
	// Guard: Global unique UDP ports quota across all profiles
	uniqueUDPPorts := make(map[int]struct{})
	for _, p := range profiles {
		for _, u := range p.UDPRanges {
			ports, err := parseUDPRangePorts(u)
			if err != nil {
				return "", err
			}
			for _, port := range ports {
				uniqueUDPPorts[port] = struct{}{}
			}
		}
	}
	if len(uniqueUDPPorts) > 3000 {
		return "", fmt.Errorf("total unique UDP ports across all profiles (%d) exceeds global safety maximum of 3000 ports", len(uniqueUDPPorts))
	}

	var buf bytes.Buffer

	buf.WriteString("# ==============================================================================\n")
	buf.WriteString("# WARLINK STOCKHOLM GATEWAY - ZERO-TRUST AUTO-GENERATED ACL\n")
	buf.WriteString("# Generated at: " + time.Now().UTC().Format(time.RFC3339) + "\n")
	buf.WriteString("# DO NOT EDIT DIRECTLY. Managed by /opt/warlink-server/warlink-server\n")
	buf.WriteString("# ==============================================================================\n\n")

	// 1. Base Block: Private / RFC1918 / Loopback / Multicast
	buf.WriteString("# 1. Base Block: Private & Internal Networks\n")
	for _, cidr := range BannedPrivateCIDRs {
		if cidr != "0.0.0.0/0" {
			buf.WriteString(fmt.Sprintf("reject(%s)\n", cidr))
		}
	}
	buf.WriteString("\n")

	// 2. Base Block: BitTorrent & P2P Ports
	buf.WriteString("# 2. Base Block: BitTorrent & P2P Known Ports\n")
	buf.WriteString("reject(all, */6881-6889)\n")
	buf.WriteString("reject(all, */6969)\n")
	buf.WriteString("reject(all, */1337)\n")
	buf.WriteString("reject(all, */51413)\n")
	buf.WriteString("reject(all, */51419)\n")
	buf.WriteString("reject(all, */2710)\n")
	buf.WriteString("reject(all, */411)\n")
	buf.WriteString("reject(all, */1214)\n")
	buf.WriteString("reject(all, */6346)\n\n")

	// 3. Base Block: BitTorrent Trackers & Domains
	buf.WriteString("# 3. Base Block: BitTorrent Trackers & P2P Domains\n")
	buf.WriteString("reject(suffix:torrent)\n")
	buf.WriteString("reject(suffix:tracker)\n")
	buf.WriteString("reject(suffix:rutracker.org)\n")
	buf.WriteString("reject(suffix:nnmclub.to)\n")
	buf.WriteString("reject(suffix:rutor.info)\n")
	buf.WriteString("reject(suffix:thepiratebay.org)\n")
	buf.WriteString("reject(suffix:1337x.to)\n")
	buf.WriteString("reject(suffix:nyaa.si)\n\n")

	// 4. Base Block: Dangerous Outbound Ports (Spam / SMB / RPC / NTP)
	buf.WriteString("# 4. Base Block: Dangerous Outbound Ports (Spam / SMB / RPC / NTP)\n")
	buf.WriteString("reject(all, tcp/25)\n")
	buf.WriteString("reject(all, tcp/465)\n")
	buf.WriteString("reject(all, tcp/587)\n")
	buf.WriteString("reject(all, */135)\n")
	buf.WriteString("reject(all, */137-139)\n")
	buf.WriteString("reject(all, */445)\n")
	buf.WriteString("reject(all, udp/123)\n\n")

	// 5. Base Allow: Secure DNS
	buf.WriteString("# 5. Base Allow: Secure DNS\n")
	buf.WriteString("direct(1.1.1.1, */53)\n")
	buf.WriteString("direct(1.0.0.1, */53)\n")
	buf.WriteString("direct(8.8.8.8, */53)\n")
	buf.WriteString("direct(8.8.4.4, */53)\n\n")

	// 6. Profiles Whitelist
	buf.WriteString("# 6. Profile Whitelist: Games and Authorized Services\n")
	for _, p := range profiles {
		buf.WriteString(fmt.Sprintf("# --- Profile: %s (%s) ---\n", p.Name, p.ID))

		// Domains
		if len(p.Domains) > 0 {
			for _, d := range p.Domains {
				buf.WriteString(fmt.Sprintf("direct(suffix:%s)\n", d))
			}
		}

		// IPs / CIDRs
		if len(p.IPs) > 0 {
			buf.WriteString(fmt.Sprintf("# IP Subnets & Endpoints for %s\n", p.Name))
			for _, ip := range p.IPs {
				if strings.Contains(ip, ",") {
					buf.WriteString(fmt.Sprintf("direct(%s)\n", ip))
				} else if strings.Contains(ip, "/") {
					buf.WriteString(fmt.Sprintf("direct(%s)\n", ip))
				} else {
					buf.WriteString(fmt.Sprintf("direct(%s, tcp/443)\n", ip))
				}
			}
		}

		// UDP ranges
		if len(p.UDPRanges) > 0 {
			buf.WriteString(fmt.Sprintf("# Dedicated UDP Ports for %s\n", p.Name))
			for _, u := range p.UDPRanges {
				buf.WriteString(fmt.Sprintf("direct(all, udp/%s)\n", u))
			}
		}

		buf.WriteString("\n")
	}

	// 7. Strict Fallback
	buf.WriteString("# 7. Strict Fallback: Reject ALL other TCP and UDP traffic\n")
	buf.WriteString("reject(all)\n")

	return buf.String(), nil
}

// VerifyWithHysteria performs a test run of Hysteria with candidate ACL.
func VerifyWithHysteria(candidateACLPath, hysteriaBin, certPath, keyPath string) error {
	// Create temporary hysteria server config
	tempCfgPath := filepath.Join(os.TempDir(), fmt.Sprintf("hy_test_cfg_%d.yaml", time.Now().UnixNano()))
	defer os.Remove(tempCfgPath)

	cfgContent := fmt.Sprintf(`listen: 127.0.0.1:58999
tls:
  cert: %s
  key: %s
auth:
  type: password
  password: acl_test_dummy_pass
acl:
  file: %s
`, certPath, keyPath, candidateACLPath)

	if err := os.WriteFile(tempCfgPath, []byte(cfgContent), 0600); err != nil {
		return fmt.Errorf("failed to write test config: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, hysteriaBin, "server", "-c", tempCfgPath, "--disable-update-check")
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to launch test hysteria process: %w", err)
	}

	// Wait up to 1.5s: if process exits, it failed immediately due to bad ACL
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case waitErr := <-done:
		// Process exited early: this indicates failure (e.g. fatal ACL parse error)
		return fmt.Errorf("hysteria failed ACL test: %v (output: %s)", waitErr, outBuf.String())
	case <-time.After(1500 * time.Millisecond):
		// Process is still running and listening: ACL validation SUCCESSFUL!
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-done
		return nil
	}
}

// ACLRulesEqual checks if two ACL strings have the same functional rules (ignoring timestamp headers).
func ACLRulesEqual(a, b string) bool {
	normalize := func(s string) string {
		var lines []string
		for _, l := range strings.Split(s, "\n") {
			trimmed := strings.TrimSpace(l)
			if strings.HasPrefix(trimmed, "# Generated at:") {
				continue
			}
			lines = append(lines, trimmed)
		}
		return strings.Join(lines, "\n")
	}
	return normalize(a) == normalize(b)
}

// ApplyAndReload writes verified ACL and restarts hysteria-server.service only if rules changed.
func ApplyAndReload(candidateContent, targetACLPath, hysteriaBin, certPath, keyPath string) error {
	// If existing ACL rules are identical, avoid restarting Hysteria server!
	if existingContent, err := os.ReadFile(targetACLPath); err == nil {
		if ACLRulesEqual(string(existingContent), candidateContent) {
			return nil
		}
	}

	tempCandPath := filepath.Join(os.TempDir(), fmt.Sprintf("hy_acl_cand_%d.txt", time.Now().UnixNano()))
	if err := os.WriteFile(tempCandPath, []byte(candidateContent), 0644); err != nil {
		return fmt.Errorf("failed to write candidate ACL: %w", err)
	}
	defer os.Remove(tempCandPath)

	// Verify candidate ACL
	if err := VerifyWithHysteria(tempCandPath, hysteriaBin, certPath, keyPath); err != nil {
		return fmt.Errorf("ACL validation failed, keeping old ACL: %w", err)
	}

	// Atomically replace target ACL file
	backupPath := targetACLPath + ".bak"
	if _, err := os.Stat(targetACLPath); err == nil {
		_ = os.WriteFile(backupPath, nil, 0644)
		oldContent, rErr := os.ReadFile(targetACLPath)
		if rErr == nil {
			_ = os.WriteFile(backupPath, oldContent, 0644)
		}
	}

	if err := os.WriteFile(targetACLPath, []byte(candidateContent), 0644); err != nil {
		return fmt.Errorf("failed to write target ACL: %w", err)
	}

	// Restart hysteria-server service
	restartCmd := exec.Command("systemctl", "restart", "hysteria-server")
	if out, err := restartCmd.CombinedOutput(); err != nil {
		// Rollback to backup
		if oldContent, rErr := os.ReadFile(backupPath); rErr == nil {
			_ = os.WriteFile(targetACLPath, oldContent, 0644)
			_ = exec.Command("systemctl", "restart", "hysteria-server").Run()
		}
		return fmt.Errorf("failed to restart hysteria-server (rolled back): %v (output: %s)", err, string(out))
	}

	// Verify service is active
	statusCmd := exec.Command("systemctl", "is-active", "hysteria-server")
	if out, err := statusCmd.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "active" {
		// Rollback
		if oldContent, rErr := os.ReadFile(backupPath); rErr == nil {
			_ = os.WriteFile(targetACLPath, oldContent, 0644)
			_ = exec.Command("systemctl", "restart", "hysteria-server").Run()
		}
		return fmt.Errorf("hysteria-server not active after restart (rolled back): %s", string(out))
	}

	return nil
}
