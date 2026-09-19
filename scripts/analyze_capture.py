import re
import sys
from collections import defaultdict

def analyze_log(log_path):
    # Regex patterns for sing-box logs
    # +0300 2026-09-19 00:57:29 INFO [3301278953 0ms] router: found process path: C:\...
    proc_pat = re.compile(r'\[(\d+)\s+[\d\w]+\] router: found process path:\s*(.+)')
    conn_pat = re.compile(r'\[(\d+)\s+[\d\w]+\] (inbound|outbound)/[\w\-]+\[([\w\-]+)\]:\s*(inbound|outbound)\s*(?:packet|connection)\s*(?:from|to)\s*([a-zA-Z0-9\.\-_:]+)')
    sniff_pat = re.compile(r'\[(\d+)\s+[\d\w]+\] router: sniffed protocol:\s*(\w+),\s*domain:\s*([a-zA-Z0-9\.\-_]+)')
    
    conn_processes = {}
    conn_domains = {}
    conn_endpoints = defaultdict(lambda: {"outbound": "", "proto": "tcp", "domain": "", "process": ""})
    
    stats = defaultdict(lambda: {"count": 0, "outbounds": set()})

    with open(log_path, 'r', encoding='utf-8', errors='ignore') as f:
        for line in f:
            # Process match
            pm = proc_pat.search(line)
            if pm:
                cid, ppath = pm.groups()
                pname = ppath.replace('\\', '/').split('/')[-1]
                conn_processes[cid] = pname
                continue
                
            # Sniff match
            sm = sniff_pat.search(line)
            if sm:
                cid, proto, domain = sm.groups()
                conn_domains[cid] = domain
                continue
                
            # Connection match
            cm = conn_pat.search(line)
            if cm:
                cid, direction, tag, action, target = cm.groups()
                pname = conn_processes.get(cid, "system/other")
                domain = conn_domains.get(cid, "-")
                
                # Check if target has IP and port
                if ':' in target:
                    parts = target.rsplit(':', 1)
                    host = parts[0]
                    port = parts[1]
                else:
                    host = target
                    port = ""
                    
                key = (pname, domain, host, port, tag)
                stats[key]["count"] += 1

    print("\n" + "="*80)
    print("WARLINK CAPTURE ANALYSIS SUMMARY")
    print("="*80)
    
    # Filter for game processes
    game_keywords = ["wardog", "elytra", "service", "control", "shipping"]
    
    print("\n### 1. Game Processes Connections (Target for GPN & ACL):")
    print("| Process | Sniffed Domain | Destination Host/IP | Port | Outbound | Connections |")
    print("| :--- | :--- | :--- | :--- | :--- | :--- |")
    
    game_rows = []
    other_rows = []
    
    for (pname, domain, host, port, tag), data in sorted(stats.items(), key=lambda x: -x[1]["count"]):
        row = f"| `{pname}` | {domain} | `{host}` | `{port}` | `{tag}` | {data['count']} |"
        if any(kw in pname.lower() for kw in game_keywords):
            game_rows.append(row)
        else:
            other_rows.append(row)
            
    if game_rows:
        for r in game_rows:
            print(r)
    else:
        print("| *No game connections recorded yet* | - | - | - | - | 0 |")

    print("\n### 2. Other System / Background Connections (Bypassed Direct):")
    print("| Process | Sniffed Domain | Destination Host/IP | Port | Outbound | Connections |")
    print("| :--- | :--- | :--- | :--- | :--- | :--- |")
    for r in other_rows[:20]:
        print(r)
    if len(other_rows) > 20:
        print(f"| ... and {len(other_rows) - 20} more | | | | | |")
        
    print("\n" + "="*80)

if __name__ == "__main__":
    path = sys.argv[1] if len(sys.argv) > 1 else "warlink_core/singbox/singbox.log"
    analyze_log(path)
