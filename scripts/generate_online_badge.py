#!/usr/bin/env python3
import os
import sys
import json
import urllib.request
import urllib.error

def fetch_status(status_url):
    try:
        req = urllib.request.Request(
            status_url,
            headers={"User-Agent": "WarLink-Badge-Updater/1.0"}
        )
        with urllib.request.urlopen(req, timeout=10) as resp:
            if resp.status == 200:
                data = json.loads(resp.read().decode("utf-8"))
                return data
    except Exception as e:
        print(f"Warning: Failed to fetch status from {status_url}: {e}", file=sys.stderr)
    return None

def build_svg(active_sessions=0, max_sessions=61, is_online=True):
    if max_sessions <= 0:
        max_sessions = 61
    pct = round((active_sessions / max_sessions) * 100)
    
    # 10 segments calculation
    active_segs = round((active_sessions / max_sessions) * 10)
    active_segs = max(0, min(10, active_segs))
    
    segments_svg = []
    for i in range(10):
        color = "#FF5E1F" if i < active_segs else "#262626"
        segments_svg.append(f'<rect x="{i*11}" width="8" height="10" rx="1" fill="{color}"/>')
    
    segs_joined = "\n      ".join(segments_svg)
    dot_color = "#2EA44F" if is_online else "#666666"
    
    svg = f"""<svg xmlns="http://www.w3.org/2000/svg" width="310" height="28" viewBox="0 0 310 28" fill="none">
  <rect width="310" height="28" rx="3" fill="#111111" stroke="#262626"/>
  <rect width="90" height="28" rx="3" fill="#181818"/>
  <circle cx="15" cy="14" r="3.5" fill="{dot_color}"/>
  <text x="26" y="17.5" fill="#CCCCCC" font-family="-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif" font-size="10.5" font-weight="700" letter-spacing="0.5">ОНЛАЙН</text>
  <g transform="translate(100, 9)">
    {segs_joined}
  </g>
  <text x="218" y="17.5" fill="#FF5E1F" font-family="-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif" font-size="11" font-weight="700">{active_sessions}/{max_sessions}</text>
  <text x="262" y="17.5" fill="#777777" font-family="-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif" font-size="10" font-weight="600">({pct}%)</text>
</svg>"""
    return svg

def main():
    output_path = "assets/badge_online.svg"
    if len(sys.argv) > 1:
        output_path = sys.argv[1]
        
    status_url = os.environ.get("WARLINK_STATUS_URL")
    if not status_url:
        gw_ip = os.environ.get("OFFICIAL_GATEWAY_IP")
        if gw_ip:
            status_url = f"http://{gw_ip}/api/v1/status"
        else:
            status_url = "http://127.0.0.1:8081/api/v1/status"
            
    status_data = fetch_status(status_url)
    
    if status_data:
        active = int(status_data.get("active_sessions", 0))
        max_s = int(status_data.get("max_sessions", 61))
        is_online = status_data.get("status") == "online" or active > 0
    else:
        # Fallback to reading existing file or default values
        if os.path.exists(output_path):
            print(f"Preserving existing {output_path} due to fetch failure.")
            return
        active = 0
        max_s = 61
        is_online = False

    svg_content = build_svg(active, max_s, is_online)
    
    os.makedirs(os.path.dirname(os.path.abspath(output_path)), exist_ok=True)
    with open(output_path, "w", encoding="utf-8") as f:
        f.write(svg_content)
        
    print(f"Generated {output_path}: {active}/{max_s} ({round(active/max_s*100)}%)")

if __name__ == "__main__":
    main()
