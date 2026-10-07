#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
AION 2 Traffic & Ping Forensic Analyzer
Analyzes captured packets (from pktmon etl2txt / socket journal)
Identifies ping probe packets, region endpoints, protocols, packet loss, and root causes of red ping.
Zero Emoji compliant per AGENTS.md.
"""

import sys
import os
import re
import json
from collections import defaultdict
from datetime import datetime

# Ensure Windows stdout supports UTF-8
if sys.platform == "win32":
    try:
        sys.stdout.reconfigure(encoding="utf-8")
        sys.stderr.reconfigure(encoding="utf-8")
    except Exception:
        pass

# Known IP ranges and services for AION 2 / NCSoft / Cloudflare Spectrum
KNOWN_REGIONS = {
    "193.202.112": "Europe (Cloudflare Spectrum Gateway)",
    "216.107.254": "NCSoft Platform / GPA Auth & QoS",
    "212.101.4": "STUN / Voice Gateway (Solnet)",
    "34.117.142": "North America - East (GCP Cloud Gateway)",
    "35.217.56": "North America - West (GCP Cloud Gateway)",
    "150.171": "Asia - Seoul / Tokyo Gateway",
}

def guess_region(ip):
    for prefix, name in KNOWN_REGIONS.items():
        if ip.startswith(prefix):
            return name
    # Heuristics based on general cloud IP ranges
    parts = ip.split(".")
    if len(parts) == 4:
        first = int(parts[0])
        if first in (3, 18, 52, 54):
            return "AWS Game Datacenter (Regional)"
        if first in (34, 35):
            return "Google Cloud Game Datacenter (Regional)"
    return "Unknown Remote Endpoint"

def parse_socket_journal(journal_path):
    """
    Parses journal of live sockets recorded by capture_aion2_traffic.ps1
    """
    if not os.path.exists(journal_path):
        return []
    
    entries = []
    with open(journal_path, "r", encoding="utf-8", errors="ignore") as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            try:
                data = json.loads(line)
                entries.append(data)
            except Exception:
                pass
    return entries

def parse_pktmon_txt(txt_path):
    """
    Parses pktmon etl2txt output:
    Handles UTF-16 / UTF-8 automatically and extracts timestamp, protocols, and IP.port.
    """
    if not os.path.exists(txt_path):
        return []

    # Detect encoding (pktmon outputs UTF-16 LE)
    encoding = "utf-8"
    try:
        with open(txt_path, "rb") as bf:
            head = bf.read(4)
            if head.startswith(b"\xff\xfe") or b"\x00" in head:
                encoding = "utf-16"
    except Exception:
        pass

    packet_records = []
    current_ts = ""
    ts_re = re.compile(r'(\d{2}:\d{2}:\d{2}\.\d{3})')
    pkt_re = re.compile(r'(\d+\.\d+\.\d+\.\d+)\.(\d+)\s*>\s*(\d+\.\d+\.\d+\.\d+)\.(\d+)')

    with open(txt_path, "r", encoding=encoding, errors="ignore") as f:
        for line in f:
            tm = ts_re.search(line)
            if tm:
                current_ts = tm.group(1)

            pm = pkt_re.search(line)
            if pm:
                src_ip, src_port, dst_ip, dst_port = pm.groups()
                proto = "TCP" if "Flags" in line else ("UDP" if "udp" in line.lower() else "IP")
                packet_records.append({
                    "timestamp": current_ts,
                    "proto": proto,
                    "src_ip": src_ip,
                    "src_port": int(src_port),
                    "dst_ip": dst_ip,
                    "dst_port": int(dst_port),
                    "raw": line.strip()
                })
    return packet_records

def analyze_traffic(socket_entries, pktmon_packets):
    """
    Correlates socket activity and raw packet monitor data.
    """
    endpoints = defaultdict(lambda: {
        "proto": "UNKNOWN",
        "sent_pkts": 0,
        "recv_pkts": 0,
        "sent_bytes": 0,
        "recv_bytes": 0,
        "first_seen": None,
        "last_seen": None,
        "local_ports": set(),
        "role": "UNKNOWN",
        "region": "",
        "rtt_samples": [],
    })

    # Process socket entries
    for entry in socket_entries:
        remote_ip = entry.get("remote_ip")
        remote_port = entry.get("remote_port", 0)
        proto = entry.get("proto", "TCP").upper()
        local_port = entry.get("local_port", 0)
        ts = entry.get("timestamp", "")
        role = entry.get("role", "UNKNOWN")

        if not remote_ip or remote_ip in ("0.0.0.0", "127.0.0.1", "::", "::1"):
            continue

        key = (remote_ip, remote_port)
        ep = endpoints[key]
        ep["proto"] = proto
        ep["local_ports"].add(local_port)
        if role != "UNKNOWN":
            ep["role"] = role
        if not ep["first_seen"]:
            ep["first_seen"] = ts
        ep["last_seen"] = ts
        ep["region"] = guess_region(remote_ip)

    # Process raw packets from pktmon
    for pkt in pktmon_packets:
        src_ip = pkt["src_ip"]
        src_port = pkt["src_port"]
        dst_ip = pkt["dst_ip"]
        dst_port = pkt["dst_port"]
        proto = pkt["proto"]
        ts = pkt["timestamp"]

        # Check if outbound
        is_local_src = src_ip.startswith(("192.168.", "10.", "172.16.", "172.17.", "172.18.", "172.19.", "127."))
        is_local_dst = dst_ip.startswith(("192.168.", "10.", "172.16.", "172.17.", "172.18.", "172.19.", "127."))

        if is_local_src and not is_local_dst:
            # Outbound packet
            key = (dst_ip, dst_port)
            ep = endpoints[key]
            ep["proto"] = proto
            ep["sent_pkts"] += 1
            ep["local_ports"].add(src_port)
            if not ep["first_seen"]:
                ep["first_seen"] = ts
            ep["last_seen"] = ts
            ep["region"] = guess_region(dst_ip)
        elif not is_local_src and is_local_dst:
            # Inbound packet
            key = (src_ip, src_port)
            ep = endpoints[key]
            ep["proto"] = proto
            ep["recv_pkts"] += 1
            if not ep["first_seen"]:
                ep["first_seen"] = ts
            ep["last_seen"] = ts
            ep["region"] = guess_region(src_ip)

    # Classification and Diagnostic Logic
    ping_probes = []
    auth_apis = []
    game_streams = []
    other_flows = []

    for (remote_ip, remote_port), ep in endpoints.items():
        proto = ep["proto"]
        sent = ep["sent_pkts"]
        recv = ep["recv_pkts"]
        total = sent + recv

        # Rule for ping probes:
        # 1) UDP packets to QoS beacon ports (13328, 13700, 3478, 7777-7788)
        # 2) Small packet counts (1-20 packets sent) with periodic bursts
        # 3) ICMP packets
        # 4) Or role explicitly marked REGION_PING
        is_ping_probe = False
        if ep["role"] == "REGION_PING":
            is_ping_probe = True
        elif proto == "ICMP":
            is_ping_probe = True
        elif proto == "UDP" and (remote_port in (13328, 13700, 3478) or (sent > 0 and sent <= 25 and recv <= 25)):
            is_ping_probe = True
        elif proto == "TCP" and remote_port in (13328, 13700) and sent <= 10:
            is_ping_probe = True

        if is_ping_probe:
            ep["role"] = "REGION_PING"
            # Status determination
            if sent > 0 and recv == 0:
                ep["status"] = "RED / TIMEOUT (100% потери пакетов)"
            elif recv > 0:
                loss = max(0.0, (sent - recv) / sent * 100.0) if sent > 0 else 0.0
                if loss > 50.0:
                    ep["status"] = f"YELLOW / UNSTABLE ({loss:.1f}% потери)"
                else:
                    ep["status"] = f"OK / GREEN ({loss:.1f}% потери)"
            else:
                ep["status"] = "SOCKET OPEN (Ожидание пакетов)"
            ping_probes.append(((remote_ip, remote_port), ep))
        elif remote_port == 443 and proto == "TCP":
            ep["role"] = "AUTH_API / HTTPS"
            ep["status"] = "ESTABLISHED"
            auth_apis.append(((remote_ip, remote_port), ep))
        elif proto == "UDP" and (sent > 25 or recv > 25):
            ep["role"] = "GAME_STREAM"
            ep["status"] = "ACTIVE GAMEPLAY"
            game_streams.append(((remote_ip, remote_port), ep))
        else:
            ep["status"] = "MONITORED"
            other_flows.append(((remote_ip, remote_port), ep))

    return {
        "endpoints": endpoints,
        "ping_probes": ping_probes,
        "auth_apis": auth_apis,
        "game_streams": game_streams,
        "other_flows": other_flows
    }

def print_report(results, report_file_path=None):
    lines = []
    lines.append("=" * 80)
    lines.append("   AION 2: ДЕТАЛЬНЫЙ АНАЛИЗ СЕТЕВОГО ТРАФИКА И ДИАГНОСТИКА ПИНГА")
    lines.append("=" * 80)
    lines.append(f"Время анализа: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    lines.append(f"Всего обнаружено сетевых эндпоинтов: {len(results['endpoints'])}")
    lines.append(f"Выявлено пробников пинга регионов: {len(results['ping_probes'])}")
    lines.append(f"Авторизационных и служебных сессий: {len(results['auth_apis'])}")
    lines.append(f"Активных игровых UDP сессий: {len(results['game_streams'])}")
    lines.append("-" * 80)

    # 1. Секция замеров пинга регионов
    lines.append("\n[1] ПРОБНИКИ ПИНГА РЕГИОНОВ (REGION PING PROBES):")
    lines.append("--------------------------------------------------------------------------------")
    if not results["ping_probes"]:
        lines.append("Внимание: во время сессии захвата не зафиксировано явных пакетов проверки пинга.")
        lines.append("Возможные причины: игра еще не заходила в меню выбора региона или трафик шел")
        lines.append("через другой сетевой интерфейс без генерации пакетов в драйвере.")
    else:
        lines.append(f"{'Назначение (IP:Port)':<26} {'Протокол':<8} {'Отпр':<6} {'Прин':<6} {'Статус':<24} {'Регион/Сервис'}")
        lines.append("-" * 80)
        for (ip, port), ep in results["ping_probes"]:
            target = f"{ip}:{port}"
            proto = ep["proto"]
            sent = ep["sent_pkts"]
            recv = ep["recv_pkts"]
            status = ep.get("status", "UNKNOWN")
            region = ep["region"]
            lines.append(f"{target:<26} {proto:<8} {sent:<6} {recv:<6} {status:<24} {region}")

    # 2. Секция авторизации и служебных вызовов
    lines.append("\n[2] АВТОРИЗАЦИЯ, ПЛАТФОРМА И API СЕРВИСЫ (AUTH & API ENDPOINTS):")
    lines.append("--------------------------------------------------------------------------------")
    if not results["auth_apis"]:
        lines.append("Запросов к внешним API не зафиксировано.")
    else:
        lines.append(f"{'Назначение (IP:Port)':<26} {'Протокол':<8} {'Отпр':<6} {'Прин':<6} {'Статус':<16} {'Регион/Сервис'}")
        lines.append("-" * 80)
        for (ip, port), ep in results["auth_apis"][:15]:
            target = f"{ip}:{port}"
            proto = ep["proto"]
            sent = ep["sent_pkts"]
            recv = ep["recv_pkts"]
            status = ep.get("status", "ESTABLISHED")
            region = ep["region"]
            lines.append(f"{target:<26} {proto:<8} {sent:<6} {recv:<6} {status:<16} {region}")

    # 3. Секция игровых потоков
    if results["game_streams"]:
        lines.append("\n[3] ОСНОВНЫЕ ИГРОВЫЕ СЕТЕВЫЕ СЕССИИ (GAMEPLAY SESSIONS):")
        lines.append("--------------------------------------------------------------------------------")
        for (ip, port), ep in results["game_streams"]:
            target = f"{ip}:{port}"
            proto = ep["proto"]
            sent = ep["sent_pkts"]
            recv = ep["recv_pkts"]
            region = ep["region"]
            lines.append(f"{target:<26} {proto:<8} Отправлено: {sent:<6} Получено: {recv:<6} Регион: {region}")

    # 4. Технический вердикт и диагностика красного пинга
    lines.append("\n" + "=" * 80)
    lines.append("   ТЕХНИЧЕСКИЙ ВЕРДИКТ И ПРИЧИНА КРАСНОГО ПИНГА (ROOT CAUSE ANALYSIS)")
    lines.append("=" * 80)

    red_probes = [ep for (ip, port), ep in results["ping_probes"] if "RED" in ep.get("status", "")]
    if red_probes:
        lines.append("[!] ЗАФИКСИРОВАНА ПРИЧИНА КРАСНОГО ПИНГА:")
        lines.append(f"Обнаружено {len(red_probes)} пробников, у которых отправленные пакеты не получили ответа (100% Packet Loss).")
        lines.append("Механизм в игре:")
        lines.append("1. Клиент AION 2 отправляет проверочные UDP/STUN запросы на порты регионов (13328, 13700, 3478).")
        lines.append("2. Ответные пакеты (PONG / Binding Response) не возвращаются в установленный таймаут (1.5-2.0 сек).")
        lines.append("3. Игра переводит статус региона в EQosDatacenterResult::Incomplete и выводит красный маркер с '....'.")
        lines.append("\nВозможные причины потери ответа:")
        lines.append("- Маршрут идет в обход VPN напрямую через провайдера РФ, где UDP-порты или IP шлюза блокируются ТСПУ/РКН.")
        lines.append("- Маршрут идет через VPN, но на выходном сервере Hysteria/VLESS в правилах ACL отсутствуют целевые диапазоны портов.")
        lines.append("- Драйвер Wintun / сетевой стек sing-box работает в режиме mixed/gVisor, из-за чего UDP NAT-таблица не пробрасывает обратный ответ.")
    else:
        lines.append("[OK] Критических потерь в зарегистрированных пробах пинга не обнаружено.")
        lines.append("Если пинг в игре все еще красный, замер следует повторить непосредственно в момент")
        lines.append("открытия окна 'Выбор региона', предварительно очистив сетевые соединения.")

    lines.append("=" * 80)

    report_text = "\n".join(lines)
    print(report_text)

    if report_file_path:
        os.makedirs(os.path.dirname(os.path.abspath(report_file_path)), exist_ok=True)
        with open(report_file_path, "w", encoding="utf-8") as f:
            f.write(report_text)
        print(f"\n[OK] Полный отчет сохранен в: {report_file_path}")

def main():
    if len(sys.argv) < 2:
        print("Использование: python analyze_aion2_capture.py <journal_jsonl> [pktmon_txt] [output_report_txt]")
        sys.exit(1)

    journal_path = sys.argv[1]
    pktmon_txt_path = sys.argv[2] if len(sys.argv) > 2 else ""
    output_report = sys.argv[3] if len(sys.argv) > 3 else "captures/aion2_diagnosis_report.txt"

    socket_entries = parse_socket_journal(journal_path)
    pktmon_packets = parse_pktmon_txt(pktmon_txt_path) if pktmon_txt_path else []

    results = analyze_traffic(socket_entries, pktmon_packets)
    print_report(results, output_report)

if __name__ == "__main__":
    main()
