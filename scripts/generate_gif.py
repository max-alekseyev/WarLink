import os
import sys
import http.server
import socketserver
import threading
import time
import json
import math
from PIL import Image, ImageDraw
from playwright.sync_api import sync_playwright

def get_cursor_img():
    # Crisp 24x24 RGBA arrow cursor
    cursor = Image.new('RGBA', (24, 24), (0, 0, 0, 0))
    draw = ImageDraw.Draw(cursor)
    poly = [(0, 0), (0, 17), (4.5, 13.5), (8, 20), (10.5, 18.8), (7, 12.3), (12.5, 12.3)]
    shadow_poly = [(x+1, y+1) for x, y in poly]
    draw.polygon(shadow_poly, fill=(0, 0, 0, 95))
    draw.polygon(poly, fill=(255, 255, 255, 255), outline=(15, 15, 15, 255), width=1)
    return cursor

def ease_in_out(t):
    t = max(0.0, min(1.0, t))
    return t * t * (3.0 - 2.0 * t)

def interpolate(p1, p2, t):
    t_e = ease_in_out(t)
    return (p1[0] + (p2[0] - p1[0]) * t_e, p1[1] + (p2[1] - p1[1]) * t_e)

def main():
    PORT = 19910
    
    class Handler(http.server.SimpleHTTPRequestHandler):
        def __init__(self, *args, **kwargs):
            super().__init__(*args, directory='ui', **kwargs)
        def log_message(self, format, *args):
            pass

    httpd = socketserver.TCPServer(('127.0.0.1', PORT), Handler)
    threading.Thread(target=httpd.serve_forever, daemon=True).start()
    time.sleep(0.3)

    cursor_img = get_cursor_img()

    with sync_playwright() as p:
        browser = p.chromium.launch()
        page = browser.new_page(viewport={'width': 520, 'height': 370}, device_scale_factor=1)

        mock_data = {
            "version": "v2.1.7",
            "is_connected": False,
            "ping_ms": 29,
            "gateway_ping": 29,
            "ping_label": "29 мс",
            "gateway_slots": "14/100",
            "gateway_days": 28,
            "donate_amount_rub": 98,
            "games": [{"id": "wardogs", "title": "WARDOGS", "steam_app_id": "1867240", "icon_url": "wardogs_icon.png", "is_default": True}],
            "selected_game_id": "wardogs"
        }

        page.route('**/api/**', lambda r: r.fulfill(status=200, content_type='application/json', body=json.dumps(mock_data)))
        page.goto(f'http://127.0.0.1:{PORT}/index.html')
        page.wait_for_timeout(500)

        # 1. Capture Base Idle
        import io
        buf_idle = page.screenshot()
        img_idle = Image.open(io.BytesIO(buf_idle)).convert('RGBA')

        # 2. Capture Base Connecting (dimmed card, connecting dot in footer)
        page.evaluate('''() => {
            isBusy = true;
            launchingGameId = 'wardogs';
            renderShowcase(cachedGames, 'wardogs');
            const gwDot = document.querySelector('.gateway-dot');
            if (gwDot) { gwDot.classList.remove('is-online'); gwDot.classList.add('is-connecting'); }
            const spin = document.querySelector('.shortcut-spinner-icon');
            if (spin) spin.style.display = 'none';
        }''')
        page.wait_for_timeout(200)
        buf_conn = page.screenshot()
        img_connecting = Image.open(io.BytesIO(buf_conn)).convert('RGBA')

        # 3. Capture Base Connected
        mock_data["is_connected"] = True
        mock_data["gateway_ping"] = 24
        mock_data["ping_label"] = "24 мс"
        page.evaluate(f'''() => {{
            isBusy = false;
            isConnected = true;
            launchingGameId = null;
            updateUI({json.dumps(mock_data)});
            showToast('Маршрут оптимизирован • WARDOGS готов к игре', 10000);
        }}''')
        page.wait_for_timeout(300)
        buf_active = page.screenshot()
        img_connected = Image.open(io.BytesIO(buf_active)).convert('RGBA')

        browser.close()
    httpd.shutdown()

    print('Captured high-DPI base states. Generating 220 seamless 50 FPS frames...')

    # Key Points
    P_idle = (460, 280)
    P_card = (60, 125)
    P_watch = (320, 220)
    spinner_center = (60, 122)

    frames = []

    # Total 220 frames @ 20ms = 4.40s
    # F0 - F10 (0.00-0.20s): Standby, cursor at P_idle
    # F11 - F35 (0.22-0.70s): Cursor glides P_idle -> P_card
    # F36 - F40 (0.72-0.80s): Click WARDOGS
    # F41 - F100 (0.82-2.00s): Connecting spinner rotating at 50 FPS, cursor moves to P_watch
    # F101 - F155 (2.02-3.10s): Connected state (green dot, 24 ms ping, toast)
    # F156 - F175 (3.12-3.50s): Cursor moves P_watch -> P_card
    # F176 - F180 (3.52-3.60s): Click disconnect
    # F181 - F205 (3.62-4.10s): Cursor moves P_card -> P_idle, standby state
    # F206 - F220 (4.12-4.40s): Standby, cursor at P_idle
    # Frame 220 == Frame 0 (100% Seamless!)

    for f in range(221):
        clicking = False

        if f <= 10:
            base = img_idle.copy()
            cur_pos = P_idle

        elif f <= 35:
            base = img_idle.copy()
            t = (f - 11) / 24.0
            cur_pos = interpolate(P_idle, P_card, t)

        elif f <= 40:
            base = img_idle.copy()
            cur_pos = P_card
            clicking = True

        elif f <= 100:
            base = img_connecting.copy()
            # Draw mathematically rotating smooth spinner arc (9 degrees/frame)
            theta = (f - 41) * 9.0
            start_ang = theta % 360
            end_ang = start_ang + 270
            draw_spin = ImageDraw.Draw(base)
            cx, cy = spinner_center
            r = 10
            draw_spin.arc([cx-r, cy-r, cx+r, cy+r], start=start_ang, end=end_ang, fill=(255, 94, 31, 255), width=2)
            
            # Cursor glides to watching position
            t = min(1.0, (f - 41) / 20.0)
            cur_pos = interpolate(P_card, P_watch, t)

        elif f <= 155:
            base = img_connected.copy()
            cur_pos = P_watch

        elif f <= 175:
            base = img_connected.copy()
            t = (f - 156) / 19.0
            cur_pos = interpolate(P_watch, P_card, t)

        elif f <= 180:
            base = img_connected.copy()
            cur_pos = P_card
            clicking = True

        elif f <= 205:
            base = img_idle.copy()
            t = (f - 181) / 24.0
            cur_pos = interpolate(P_card, P_idle, t)

        else:
            base = img_idle.copy()
            cur_pos = P_idle

        # Click ripple
        cx, cy = int(cur_pos[0]), int(cur_pos[1])
        if clicking:
            draw_click = ImageDraw.Draw(base)
            draw_click.ellipse([cx-4, cy-4, cx+4, cy+4], outline=(255, 94, 31, 220), width=2)

        # Composite cursor
        base.alpha_composite(cursor_img, (cx, cy))
        frames.append(base.convert('RGB'))

    print(f'Composited {len(frames)} frames. Building global palette and zero-flicker GIF...')

    # Build Global Palette from collage to eliminate flickering
    collage = Image.new('RGB', (520 * 3, 370))
    collage.paste(frames[0], (0, 0))
    collage.paste(frames[70], (520, 0))
    collage.paste(frames[130], (1040, 0))
    global_palette_img = collage.quantize(colors=128, method=Image.Quantize.MEDIANCUT)

    p_frames = [frm.quantize(palette=global_palette_img, dither=Image.Dither.NONE) for frm in frames]

    out_path = 'assets/warlink_launch.gif'
    p_frames[0].save(
        out_path,
        save_all=True,
        append_images=p_frames[1:],
        duration=20, # strictly 20ms = 50 FPS (rock-solid, no browser fallback)
        loop=0,
        disposal=2,
        optimize=False
    )

    size = os.path.getsize(out_path)
    print(f'Done! warlink_launch.gif saved successfully ({size} bytes, {size/1024:.1f} KB).')

if __name__ == '__main__':
    main()
