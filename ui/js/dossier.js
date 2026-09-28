// ============================================================================
// МОДУЛЬ ТАКТИЧЕСКОГО ДОСЬЕ СПОНСОРА (DARK CLOUDFLARE-INSPIRED LANDSCAPE SVG)
// ============================================================================

function hashFnv1a(str) {
    let hash = 0x811c9dc5;
    for (let i = 0; i < str.length; i++) {
        hash ^= str.charCodeAt(i);
        hash = Math.imul(hash, 0x01000193);
    }
    return hash >>> 0;
}

function createMulberry32(seed) {
    let s = seed >>> 0;
    return function() {
        s = (s + 0x6D2B79F5) | 0;
        let t = Math.imul(s ^ (s >>> 15), 1 | s);
        t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
        return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };
}

function generateOrganicChiselStroke(xStart, yStart, length, baseHeight, seed) {
    const rnd = createMulberry32(seed);
    const steps = 14;
    const dx = length / steps;

    const topPoints = [];
    const botPoints = [];

    const curTopY = yStart - (baseHeight * 0.48);
    const curBotY = yStart + (baseHeight * 0.52);

    for (let i = 0; i <= steps; i++) {
        const x = xStart + i * dx;
        const t = i / steps;
        const pressure = 1.0 - 0.14 * Math.sin(t * Math.PI) + (rnd() - 0.5) * 0.16;
        const currentH = baseHeight * pressure;
        const centerY = yStart + (rnd() - 0.48) * 1.6 + Math.sin(t * Math.PI * 0.9) * (rnd() * 1.4);

        const topY = centerY - currentH * (0.46 + (rnd() - 0.5) * 0.08);
        const botY = centerY + currentH * (0.54 + (rnd() - 0.5) * 0.08);

        topPoints.push({ x, y: topY });
        botPoints.push({ x, y: botY });
    }

    const landTopX = xStart - (baseHeight * 0.25) + (rnd() - 0.5) * 1.5;
    const landBotX = xStart - (baseHeight * 0.35) + (rnd() - 0.5) * 1.5;
    const liftTopX = xStart + length + 3 + rnd() * 4;
    const liftBotX = xStart + length + (rnd() - 0.5) * 3;

    let d = `M ${landTopX.toFixed(1)} ${curTopY.toFixed(1)}`;
    for (let i = 0; i < topPoints.length; i++) {
        d += ` L ${topPoints[i].x.toFixed(1)} ${topPoints[i].y.toFixed(1)}`;
    }
    d += ` L ${liftTopX.toFixed(1)} ${topPoints[topPoints.length - 1].y.toFixed(1)}`;
    d += ` L ${liftBotX.toFixed(1)} ${botPoints[botPoints.length - 1].y.toFixed(1)}`;
    for (let i = botPoints.length - 1; i >= 0; i--) {
        d += ` L ${botPoints[i].x.toFixed(1)} ${botPoints[i].y.toFixed(1)}`;
    }
    d += ` L ${landBotX.toFixed(1)} ${curBotY.toFixed(1)}`;
    d += ' Z';

    const bristle1Y = yStart - baseHeight * 0.15;
    const bristle2Y = yStart + baseHeight * 0.18;
    const bristleD1 = `M ${(xStart + 3).toFixed(1)} ${bristle1Y.toFixed(1)} Q ${(xStart + length * 0.5).toFixed(1)} ${(bristle1Y + (rnd() - 0.5) * 2).toFixed(1)}, ${(xStart + length - 2).toFixed(1)} ${(bristle1Y + (rnd() - 0.5) * 1.5).toFixed(1)}`;
    const bristleD2 = `M ${(xStart + 5).toFixed(1)} ${bristle2Y.toFixed(1)} Q ${(xStart + length * 0.5).toFixed(1)} ${(bristle2Y + (rnd() - 0.5) * 2).toFixed(1)}, ${(xStart + length - 5).toFixed(1)} ${(bristle2Y + (rnd() - 0.5) * 1.5).toFixed(1)}`;

    return `
        <g class="marker-pass">
            <path d="${d}" fill="#0a0b0a" opacity="0.95"/>
            <path d="${bristleD1}" fill="none" stroke="#050605" stroke-width="1.8" opacity="0.8"/>
            <path d="${bristleD2}" fill="none" stroke="#050605" stroke-width="1.4" opacity="0.7"/>
        </g>
    `;
}

function renderRealMarkerGroup(xStart, yCenter, length, baseHeight, passes, seed) {
    const strokes = [];
    const rnd = createMulberry32(seed);

    const passHeight = passes === 1 ? baseHeight : (baseHeight * 0.52);
    const stepY = passes === 1 ? 0 : (baseHeight - passHeight) / (passes - 1);

    for (let p = 0; p < passes; p++) {
        const curY = passes === 1 ? yCenter : (yCenter - baseHeight * 0.5 + passHeight * 0.5 + p * stepY);
        const curX = xStart - 3 - rnd() * 4;
        const curLen = length + 6 + rnd() * 8;
        const h = passHeight * (0.94 + rnd() * 0.12);
        strokes.push(generateOrganicChiselStroke(curX, curY, curLen, h, seed + p * 31));
    }

    return `
        <g class="marker-redaction" filter="url(#chisel-ink-bleed)" style="mix-blend-mode: multiply;">
            ${strokes.join('\n')}
        </g>
    `;
}

function renderDossierSvg(data, secret = false, steamLinked = true, showMotto = true) {
    const amountBlock = secret ? 
        renderRealMarkerGroup(12, 44, 180, 30, 3, 303) :
        `<text x="12" y="56" font-family="'JetBrains Mono', monospace" font-size="28" font-weight="800" fill="#0f100e">${data.amount}</text>`;

    const rankBlock = secret ? 
        renderRealMarkerGroup(0, 24, 110, 11, 1, 202) :
        `<text x="0" y="27" font-family="'JetBrains Mono', monospace" font-size="10.5" font-weight="700" fill="${data.stampColor}">${data.rankTitle}</text>`;

    const avatarBadgeBlock = secret ? 
        `<rect x="40" y="48" width="22" height="14" fill="#090a09" rx="1"/>` :
        `<rect x="40" y="48" width="22" height="14" fill="${data.stampColor}" rx="1"/>
         <text x="51" y="59" text-anchor="middle" font-family="'JetBrains Mono', monospace" font-size="8.5" font-weight="800" fill="#ffffff">${data.avatarTier}</text>`;

    let rawSteam = data.steamId || '';
    let steamUrl = data.steamUrl || '';
    if (rawSteam.startsWith('http://') || rawSteam.startsWith('https://')) {
        steamUrl = rawSteam;
        const match = rawSteam.match(/(?:id|profiles)\/([^/?#]+)/i);
        if (match) {
            rawSteam = match[1];
        }
    } else if (rawSteam) {
        if (!steamUrl) {
            steamUrl = /^\d{17}$/.test(rawSteam) ? `https://steamcommunity.com/profiles/${rawSteam}` : `https://steamcommunity.com/id/${rawSteam}`;
        }
    }
    const cleanSteamId = rawSteam.replace(/^STEAM:\s*/i, '').trim();
    const steamIconX = Math.round(cleanSteamId.length * 6.0 + 5);

    const steamBlock = (steamLinked && cleanSteamId) ? `
        <g transform="translate(0, 44)">
            <text x="0" y="0" font-family="'JetBrains Mono', monospace" font-size="7.5" font-weight="700" fill="#756e5c" letter-spacing="0.8">STEAM ID:</text>
            <a href="${steamUrl || '#'}" target="_blank" class="steam-link-group" title="Открыть профиль Steam">
                <g transform="translate(0, 13)">
                    <text class="steam-text" x="0" y="0" font-family="'JetBrains Mono', monospace" font-size="9.5" font-weight="700" fill="#1b1c1a" letter-spacing="0">${cleanSteamId}</text>
                    <g transform="translate(${steamIconX}, -7)" class="steam-icon-ext" stroke="#68604f" stroke-width="1.1" stroke-linecap="round" stroke-linejoin="round" fill="none">
                        <path d="M 0 3.5 L 0 7.5 L 5 7.5 L 5 4.5"/>
                        <path d="M 2.5 5 L 7 0.5"/>
                        <path d="M 4.5 0.5 L 7 0.5 L 7 3"/>
                    </g>
                </g>
            </a>
        </g>
    ` : `
        <g transform="translate(0, 44)">
            <text x="0" y="0" font-family="'JetBrains Mono', monospace" font-size="7.5" font-weight="700" fill="#756e5c" letter-spacing="0.8">STEAM ID:</text>
            <text x="0" y="13" font-family="'JetBrains Mono', monospace" font-size="9" font-weight="600" fill="#8a8370">НЕ ПРИВЯЗАН</text>
        </g>
    `;

    let mottoLine1 = '';
    let mottoLine2 = '';
    if (showMotto && data.motto) {
        const fullMotto = data.motto.trim();
        if (fullMotto.length <= 32) {
            mottoLine1 = `&gt; "${escapeHtml(fullMotto)}"`;
        } else {
            let splitIdx = fullMotto.lastIndexOf(' ', 32);
            if (splitIdx === -1 || splitIdx < 15) splitIdx = 32;
            mottoLine1 = `&gt; "${escapeHtml(fullMotto.slice(0, splitIdx))}`;
            mottoLine2 = `${escapeHtml(fullMotto.slice(splitIdx).trim())}"`;
        }
    }

    const mottoBlock = (showMotto && data.motto) ? `
        <g transform="translate(38, 142)">
            <line x1="0" y1="0" x2="204" y2="0" stroke="#c8c0aa" stroke-width="0.75"/>
            <text x="0" y="16" font-family="'JetBrains Mono', monospace" font-size="8.5" font-style="italic" fill="#24221c">${mottoLine1}</text>
            ${mottoLine2 ? `<text x="10" y="28" font-family="'JetBrains Mono', monospace" font-size="8.5" font-style="italic" fill="#24221c">${mottoLine2}</text>` : ''}
        </g>
    ` : `
        <g transform="translate(38, 142)">
            <line x1="0" y1="0" x2="204" y2="0" stroke="#c8c0aa" stroke-width="0.75"/>
            <text x="0" y="18" font-family="'JetBrains Mono', monospace" font-size="8.5" font-style="italic" fill="#807865">&gt; ОПИСАНИЕ ОТСУТСТВУЕТ</text>
        </g>
    `;

    const avatarGraphic = data.avatarUrl ? `
        <image x="0" y="0" width="64" height="64" href="${escapeHtml(data.avatarUrl)}" preserveAspectRatio="xMidYMid slice" clip-path="url(#dossier-avatar-clip)"/>
    ` : `
        <circle cx="32" cy="26" r="12" fill="none" stroke="#68766c" stroke-width="1.8"/>
        <path d="M 16 56 C 16 42, 48 42, 48 56" fill="none" stroke="#68766c" stroke-width="1.8"/>
    `;

    return `
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 380" width="640" height="380">
        <defs>
            <clipPath id="dossier-avatar-clip">
                <rect width="64" height="64" rx="1"/>
            </clipPath>
            <pattern id="guilloche" width="16" height="16" patternUnits="userSpaceOnUse">
                <path d="M 16 0 L 0 0 0 16" fill="none" stroke="rgba(100, 90, 70, 0.05)" stroke-width="1"/>
            </pattern>
            <filter id="chisel-ink-bleed" filterUnits="userSpaceOnUse" x="0" y="0" width="640" height="380">
                <feTurbulence type="fractalNoise" baseFrequency="0.12 0.35" numOctaves="3" seed="43" result="noise"/>
                <feDisplacementMap in="SourceGraphic" in2="noise" scale="1.5" xChannelSelector="R" yChannelSelector="G"/>
            </filter>
        </defs>

        <rect x="0" y="0" width="640" height="380" rx="2" fill="#eae4d4" stroke="#b8b096" stroke-width="1.2"/>
        <rect x="0" y="0" width="640" height="380" fill="url(#guilloche)"/>
        <rect x="8" y="8" width="624" height="364" fill="none" stroke="#d5ceba" stroke-width="0.75"/>

        <g stroke="#7a725d" stroke-width="0.8">
            <path d="M 16 20 L 16 12 M 12 16 L 20 16"/>
            <path d="M 624 20 L 624 12 M 620 16 L 628 16"/>
            <path d="M 16 368 L 16 360 M 12 364 L 20 364"/>
            <path d="M 624 368 L 624 360 M 620 364 L 628 364"/>
        </g>

        <!-- ШАПКА -->
        <text x="28" y="32" font-family="'Space Mono', monospace" font-size="15" font-weight="700" fill="#0f100e" letter-spacing="1">WARLINK // ДОСЬЕ</text>
        
        <!-- АККАУНТ: ЗАСЕКРЕЧЕН ВСЕГДА -->
        <g transform="translate(476, 20)">
            <text x="0" y="12" font-family="'JetBrains Mono', monospace" font-size="10" font-weight="700" fill="#665f4d">АККАУНТ:</text>
            ${renderRealMarkerGroup(58, 8, 76, 12, 1, 101)}
        </g>

        <line x1="28" y1="42" x2="612" y2="42" stroke="#0f100e" stroke-width="1.5"/>
        <line x1="265" y1="54" x2="265" y2="354" stroke="#c8c0aa" stroke-width="1"/>

        <!-- ЛЕВАЯ КОЛОНКА -->
        <rect x="28" y="56" width="224" height="138" fill="rgba(220, 214, 196, 0.45)" stroke="#bfb79e" stroke-width="1" rx="1"/>
        
        <g transform="translate(38, 66)">
            <rect x="0" y="0" width="64" height="64" fill="#1c201e" stroke="#3c443e" stroke-width="1" rx="1"/>
            ${avatarGraphic}
            <path d="M 5 9 L 5 5 L 9 5" stroke="#ff5e1f" stroke-width="1.2" fill="none"/>
            <path d="M 59 9 L 59 5 L 55 5" stroke="#ff5e1f" stroke-width="1.2" fill="none"/>
            <path d="M 5 55 L 5 59 L 9 59" stroke="#ff5e1f" stroke-width="1.2" fill="none"/>
            <path d="M 59 55 L 59 59 L 55 59" stroke="#ff5e1f" stroke-width="1.2" fill="none"/>
            ${avatarBadgeBlock}
        </g>

        <g transform="translate(114, 68)">
            <text x="0" y="14" font-family="'JetBrains Mono', monospace" font-size="14.5" font-weight="800" fill="#0f100e">${data.callsign}</text>
            ${rankBlock}
            ${steamBlock}
        </g>

        ${mottoBlock}

        <!-- ШТРИХКОД -->
        <g transform="translate(28, 206)">
            <rect x="0" y="0" width="224" height="148" fill="rgba(220, 212, 190, 0.45)" stroke="#605545" stroke-width="1" stroke-dasharray="2,2" rx="1"/>
            
            <g transform="translate(14, 22)" fill="#111111">
                <rect x="0" y="0" width="2" height="48"/>
                <rect x="4" y="0" width="1" height="48"/>
                <rect x="7" y="0" width="3" height="48"/>
                <rect x="13" y="0" width="1" height="48"/>
                <rect x="16" y="0" width="2" height="48"/>
                <rect x="21" y="0" width="3" height="48"/>
                <rect x="27" y="0" width="1" height="48"/>
                <rect x="31" y="0" width="2" height="48"/>
                <rect x="36" y="0" width="3" height="48"/>
                <rect x="42" y="0" width="1" height="48"/>
                <rect x="46" y="0" width="2.5" height="48"/>
                <rect x="52" y="0" width="1.5" height="48"/>
                <rect x="56" y="0" width="3" height="48"/>
                <rect x="62" y="0" width="1" height="48"/>
                <rect x="66" y="0" width="2" height="48"/>
                <rect x="71" y="0" width="3" height="48"/>
                <rect x="77" y="0" width="1.5" height="48"/>
                <rect x="81" y="0" width="2.5" height="48"/>
                <rect x="86" y="0" width="2" height="48"/>
                <rect x="91" y="0" width="1" height="48"/>
            </g>

            <text x="124" y="42" font-family="'JetBrains Mono', monospace" font-size="11" font-weight="800" fill="#1b1c1a" letter-spacing="1">WARLINK</text>
            <text x="124" y="58" font-family="'JetBrains Mono', monospace" font-size="8" font-weight="600" fill="#756e5c">SPONSOR PASS</text>

            <line x1="14" y1="84" x2="210" y2="84" stroke="#bfb69e" stroke-width="0.75"/>
            <text x="14" y="104" font-family="'JetBrains Mono', monospace" font-size="8.5" font-weight="700" fill="#423d32">STATUS: VERIFIED</text>
            <text x="14" y="120" font-family="'JetBrains Mono', monospace" font-size="7.5" font-weight="600" fill="#7a725d">ОФИЦИАЛЬНЫЙ СПОНСОР</text>
        </g>

        <!-- ПРАВАЯ КОЛОНКА -->
        <g transform="translate(284, 56)">
            <rect x="0" y="0" width="328" height="94" fill="rgba(220, 214, 196, 0.45)" stroke="#bfb79e" stroke-width="1" rx="1"/>
            
            <text x="12" y="20" font-family="'JetBrains Mono', monospace" font-size="8" font-weight="700" fill="#665f4d" letter-spacing="0.5">СУММА ПОДДЕРЖКИ:</text>
            
            ${amountBlock}

            <line x1="12" y1="72" x2="316" y2="72" stroke="#d5cdb8" stroke-width="0.75"/>
            <text x="12" y="86" font-family="'JetBrains Mono', monospace" font-size="8.5" font-weight="600" fill="#665f4d">ДАТА РЕГИСТРАЦИИ: <tspan font-weight="700" fill="#292620">${data.joinedDate}</tspan></text>

            <!-- ШТАМП ПОДТВЕРЖДЕНИЯ -->
            <g transform="translate(196, -8) rotate(-4)">
                <rect x="0" y="0" width="138" height="42" fill="none" stroke="${data.stampColor}" stroke-width="1.8" rx="1" stroke-dasharray="0.5, 0.5"/>
                <text x="69" y="18" text-anchor="middle" font-family="'Space Mono', monospace" font-size="9" font-weight="800" fill="${data.stampColor}" letter-spacing="1">${data.stampTitle}</text>
                <text x="69" y="32" text-anchor="middle" font-family="'JetBrains Mono', monospace" font-size="7" font-weight="700" fill="${data.stampColor}" letter-spacing="0.8">${data.stampSub}</text>
            </g>
        </g>

        <!-- ИСТОРИЯ ВКЛАДОВ -->
        <g transform="translate(284, 160)">
            <text x="0" y="12" font-family="'JetBrains Mono', monospace" font-size="8.5" font-weight="700" fill="#4d4739" letter-spacing="0.5">ИСТОРИЯ ВКЛАДОВ:</text>

            <g transform="translate(0, 22)">
                <rect x="0" y="0" width="328" height="24" fill="#ded7c4" stroke="#bfb79e" stroke-width="0.75" rx="1"/>
                <text x="14" y="16" font-family="'JetBrains Mono', monospace" font-size="8.5" font-weight="700" fill="#363228">ДАТА</text>
                <text x="100" y="16" font-family="'JetBrains Mono', monospace" font-size="8.5" font-weight="700" fill="#363228">НАЗНАЧЕНИЕ</text>
                <text x="270" y="16" font-family="'JetBrains Mono', monospace" font-size="8.5" font-weight="700" fill="#363228">СТАТУС</text>
            </g>

            ${renderDossierHistoryRows(data)}
        </g>
    </svg>
    `;
}

function renderLoadingDossierSvg() {
    return `
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 380" width="640" height="380">
        <defs>
            <style>
                @keyframes skel-pulse {
                    0%, 100% { fill-opacity: 0.18; }
                    50% { fill-opacity: 0.44; }
                }
                .skel-bar {
                    fill: #706856;
                    animation: skel-pulse 1.4s ease-in-out infinite;
                    rx: 1.5px;
                }
            </style>
            <pattern id="guilloche-skel" width="16" height="16" patternUnits="userSpaceOnUse">
                <path d="M 16 0 L 0 0 0 16" fill="none" stroke="rgba(100, 90, 70, 0.05)" stroke-width="1"/>
            </pattern>
        </defs>

        <rect x="0" y="0" width="640" height="380" rx="2" fill="#eae4d4" stroke="#b8b096" stroke-width="1.2"/>
        <rect x="0" y="0" width="640" height="380" fill="url(#guilloche-skel)"/>
        <rect x="8" y="8" width="624" height="364" fill="none" stroke="#d5ceba" stroke-width="0.75"/>

        <g stroke="#7a725d" stroke-width="0.8">
            <path d="M 16 20 L 16 12 M 12 16 L 20 16"/>
            <path d="M 624 20 L 624 12 M 620 16 L 628 16"/>
            <path d="M 16 368 L 16 360 M 12 364 L 20 364"/>
            <path d="M 624 368 L 624 360 M 620 364 L 628 364"/>
        </g>

        <!-- ШАПКА -->
        <text x="28" y="32" font-family="'Space Mono', monospace" font-size="15" font-weight="700" fill="#0f100e" letter-spacing="1">WARLINK // ДОСЬЕ</text>
        
        <g transform="translate(476, 20)">
            <text x="0" y="12" font-family="'JetBrains Mono', monospace" font-size="10" font-weight="700" fill="#665f4d">АККАУНТ:</text>
            <rect x="58" y="2" width="76" height="12" class="skel-bar"/>
        </g>

        <line x1="28" y1="42" x2="612" y2="42" stroke="#0f100e" stroke-width="1.5"/>
        <line x1="265" y1="54" x2="265" y2="354" stroke="#c8c0aa" stroke-width="1"/>

        <!-- ЛЕВАЯ КОЛОНКА (СКЕЛЕТОН) -->
        <rect x="28" y="56" width="224" height="138" fill="rgba(220, 214, 196, 0.45)" stroke="#bfb79e" stroke-width="1" rx="1"/>
        
        <g transform="translate(38, 66)">
            <rect x="0" y="0" width="64" height="64" fill="#1c201e" stroke="#3c443e" stroke-width="1" rx="1"/>
            <circle cx="32" cy="26" r="12" fill="none" stroke="#68766c" stroke-width="1.8"/>
            <path d="M 16 56 C 16 42, 48 42, 48 56" fill="none" stroke="#68766c" stroke-width="1.8"/>
            <path d="M 5 9 L 5 5 L 9 5" stroke="#ff5e1f" stroke-width="1.2" fill="none"/>
            <path d="M 59 9 L 59 5 L 55 5" stroke="#ff5e1f" stroke-width="1.2" fill="none"/>
            <path d="M 5 55 L 5 59 L 9 59" stroke="#ff5e1f" stroke-width="1.2" fill="none"/>
            <path d="M 59 55 L 59 59 L 55 59" stroke="#ff5e1f" stroke-width="1.2" fill="none"/>
            <rect x="40" y="48" width="22" height="14" class="skel-bar"/>
        </g>

        <g transform="translate(114, 68)">
            <rect x="0" y="2" width="118" height="13" class="skel-bar"/>
            <rect x="0" y="19" width="84" height="9" class="skel-bar"/>
            <text x="0" y="44" font-family="'JetBrains Mono', monospace" font-size="7.5" font-weight="700" fill="#756e5c" letter-spacing="0.8">STEAM ID:</text>
            <rect x="0" y="49" width="76" height="9" class="skel-bar"/>
        </g>

        <g transform="translate(38, 142)">
            <line x1="0" y1="0" x2="204" y2="0" stroke="#c8c0aa" stroke-width="0.75"/>
            <rect x="0" y="8" width="190" height="9" class="skel-bar"/>
            <rect x="0" y="21" width="130" height="9" class="skel-bar"/>
        </g>

        <!-- ШТРИХКОД (СКЕЛЕТОН) -->
        <g transform="translate(28, 206)">
            <rect x="0" y="0" width="224" height="148" fill="rgba(220, 212, 190, 0.45)" stroke="#605545" stroke-width="1" stroke-dasharray="2,2" rx="1"/>
            <rect x="14" y="22" width="100" height="48" class="skel-bar" rx="1"/>
            <text x="124" y="42" font-family="'JetBrains Mono', monospace" font-size="11" font-weight="800" fill="#1b1c1a" letter-spacing="1">WARLINK</text>
            <text x="124" y="58" font-family="'JetBrains Mono', monospace" font-size="8" font-weight="600" fill="#756e5c">SPONSOR PASS</text>
            <line x1="14" y1="84" x2="210" y2="84" stroke="#bfb69e" stroke-width="0.75"/>
            <rect x="14" y="94" width="110" height="10" class="skel-bar"/>
            <rect x="14" y="112" width="80" height="9" class="skel-bar"/>
        </g>

        <!-- ПРАВАЯ КОЛОНКА (СКЕЛЕТОН) -->
        <g transform="translate(284, 56)">
            <rect x="0" y="0" width="328" height="94" fill="rgba(220, 214, 196, 0.45)" stroke="#bfb79e" stroke-width="1" rx="1"/>
            <text x="12" y="20" font-family="'JetBrains Mono', monospace" font-size="8" font-weight="700" fill="#665f4d" letter-spacing="0.5">СУММА ПОДДЕРЖКИ:</text>
            <rect x="12" y="32" width="140" height="30" class="skel-bar" rx="2"/>
            <line x1="12" y1="72" x2="316" y2="72" stroke="#d5cdb8" stroke-width="0.75"/>
            <rect x="12" y="78" width="170" height="10" class="skel-bar"/>
            <g transform="translate(196, -8) rotate(-4)">
                <rect x="0" y="0" width="138" height="42" fill="none" stroke="#706856" stroke-width="1.2" rx="1" stroke-dasharray="3,3" opacity="0.6"/>
                <rect x="16" y="10" width="106" height="10" class="skel-bar"/>
                <rect x="28" y="24" width="82" height="8" class="skel-bar"/>
            </g>
        </g>

        <!-- ТАБЛИЦА (СКЕЛЕТОН) -->
        <g transform="translate(284, 160)">
            <text x="0" y="12" font-family="'JetBrains Mono', monospace" font-size="8.5" font-weight="700" fill="#4d4739" letter-spacing="0.5">ИСТОРИЯ ВКЛАДОВ:</text>
            <g transform="translate(0, 22)">
                <rect x="0" y="0" width="328" height="24" fill="#ded7c4" stroke="#bfb79e" stroke-width="0.75" rx="1"/>
                <text x="14" y="16" font-family="'JetBrains Mono', monospace" font-size="8.5" font-weight="700" fill="#363228">ДАТА</text>
                <text x="100" y="16" font-family="'JetBrains Mono', monospace" font-size="8.5" font-weight="700" fill="#363228">НАЗНАЧЕНИЕ</text>
                <text x="270" y="16" font-family="'JetBrains Mono', monospace" font-size="8.5" font-weight="700" fill="#363228">СТАТУС</text>
            </g>
            <g transform="translate(0, 52)">
                <rect x="0" y="0" width="328" height="38" fill="rgba(224, 218, 202, 0.45)" stroke="#c8c0aa" stroke-width="0.75" rx="1"/>
                <rect x="14" y="14" width="64" height="10" class="skel-bar"/>
                <rect x="100" y="14" width="124" height="10" class="skel-bar"/>
                <rect x="270" y="14" width="46" height="10" class="skel-bar"/>
            </g>
            <g transform="translate(0, 96)">
                <rect x="0" y="0" width="328" height="38" fill="rgba(224, 218, 202, 0.45)" stroke="#c8c0aa" stroke-width="0.75" rx="1"/>
                <rect x="14" y="14" width="64" height="10" class="skel-bar"/>
                <rect x="100" y="14" width="110" height="10" class="skel-bar"/>
                <rect x="270" y="14" width="46" height="10" class="skel-bar"/>
            </g>
        </g>
    </svg>
    `;
}

function renderErrorDossierSvg(errorMessage = 'ОШИБКА ПОЛУЧЕНИЯ ДАННЫХ ШЛЮЗА') {
    return `
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 380" width="640" height="380">
        <rect x="0" y="0" width="640" height="380" rx="2" fill="#eae4d4" stroke="#b8b096" stroke-width="1.2"/>
        <rect x="8" y="8" width="624" height="364" fill="none" stroke="#d5ceba" stroke-width="0.75"/>
        <text x="28" y="32" font-family="'Space Mono', monospace" font-size="15" font-weight="700" fill="#0f100e" letter-spacing="1">WARLINK // ДОСЬЕ</text>
        <line x1="28" y1="42" x2="612" y2="42" stroke="#0f100e" stroke-width="1.5"/>
        <g transform="translate(320, 190)">
            <rect x="-180" y="-50" width="360" height="100" fill="rgba(220, 212, 190, 0.6)" stroke="#9e1b1b" stroke-width="1" rx="2"/>
            <path d="M 0 -24 L -16 6 L 16 6 Z" fill="none" stroke="#9e1b1b" stroke-width="2"/>
            <line x1="0" y1="-14" x2="0" y2="-6" stroke="#9e1b1b" stroke-width="2"/>
            <circle cx="0" cy="0" r="1.2" fill="#9e1b1b"/>
            <text x="0" y="24" text-anchor="middle" font-family="'JetBrains Mono', monospace" font-size="10.5" font-weight="700" fill="#9e1b1b" letter-spacing="0.5">${escapeHtml(errorMessage)}</text>
            <text x="0" y="38" text-anchor="middle" font-family="'JetBrains Mono', monospace" font-size="8.5" font-weight="600" fill="#665f4d">Повторите попытку позже или проверьте соединение</text>
        </g>
    </svg>
    `;
}

function renderDossierHistoryRows(data) {
    if (data.donations && data.donations.length > 0) {
        let rows = '';
        const items = data.donations.slice(0, 2);
        items.forEach((item, idx) => {
            const yOffset = 52 + idx * 44;
            const dDate = (item.created_at || '').slice(0, 10) || data.joinedDate;
            const dTitle = item.amount_rub ? `ПОДДЕРЖКА (${item.amount_rub} ₽)` : 'ПОДДЕРЖКА ШЛЮЗА';
            const isPaid = (item.status === 'paid' || item.status === 'confirmed');
            const dStatus = isPaid ? 'ОПЛАЧЕНО' : 'В ОБРАБОТКЕ';
            const statusColor = isPaid ? '#1b5e28' : '#706856';
            rows += `
                <g transform="translate(0, ${yOffset})">
                    <rect x="0" y="0" width="328" height="38" fill="rgba(224, 218, 202, 0.45)" stroke="#c8c0aa" stroke-width="0.75" rx="1"/>
                    <text x="14" y="23" font-family="'JetBrains Mono', monospace" font-size="8.5" font-weight="600" fill="#2b2821">${escapeHtml(dDate)}</text>
                    <text x="100" y="23" font-family="'JetBrains Mono', monospace" font-size="8.5" font-weight="600" fill="#2b2821">${escapeHtml(dTitle)}</text>
                    <text x="270" y="23" font-family="'JetBrains Mono', monospace" font-size="8.5" font-weight="700" fill="${statusColor}">${dStatus}</text>
                </g>
            `;
        });
        return rows;
    }
    return `
        <g transform="translate(0, 52)">
            <rect x="0" y="0" width="328" height="82" fill="rgba(220, 212, 190, 0.3)" stroke="#c8c0aa" stroke-width="0.75" stroke-dasharray="2,2" rx="1"/>
            <text x="164" y="46" text-anchor="middle" font-family="'JetBrains Mono', monospace" font-size="9" font-weight="600" fill="#756e5c">ЗАПИСИ О ВЗНОСАХ В РЕЕСТРЕ ОТСУТСТВУЮТ</text>
        </g>
    `;
}

function prepareDossierData(sponsor) {
    const totalDonated = Number(sponsor.total_donated_rub) || 0;
    
    let rankTitle = 'РЕКРУТ [TIER-V]';
    let avatarTier = 'REC';
    let stampColor = '#5a6268';
    
    if (totalDonated >= 5000) {
        rankTitle = 'ГЕНЕРАЛ [TIER-I]';
        avatarTier = 'GEN';
        stampColor = '#9e1b1b';
    } else if (totalDonated >= 1000) {
        rankTitle = 'ВЕТЕРАН [TIER-III]';
        avatarTier = 'VET';
        stampColor = '#1d6e2e';
    } else if (totalDonated >= 500) {
        rankTitle = 'ОПЕРАТИВНИК [TIER-IV]';
        avatarTier = 'OPR';
        stampColor = '#2b5a8f';
    }
    
    const rawJoined = sponsor.joined_date || sponsor.created_at || '';
    const joined = rawJoined.slice(0, 10) || '27.09.2026';
    const formattedJoined = joined.includes('-') ? joined.split('-').reverse().join('.') : joined;
    
    let rawSteam = (sponsor.steam_id || sponsor.steam_url || '').trim();
    let steamUrl = sponsor.steam_url || '';
    let steamId = rawSteam;

    if (rawSteam.startsWith('http://') || rawSteam.startsWith('https://')) {
        steamUrl = rawSteam;
        const m = rawSteam.match(/steamcommunity\.com\/(id|profiles)\/([^/?#]+)/i);
        steamId = m ? m[2] : 'STEAM-PROFILE';
    } else if (rawSteam) {
        steamId = rawSteam.replace(/^STEAM:\s*/i, '');
        steamUrl = /^\d{17}$/.test(steamId) 
            ? `https://steamcommunity.com/profiles/${encodeURIComponent(steamId)}`
            : `https://steamcommunity.com/id/${encodeURIComponent(steamId)}`;
    }
    
    return {
        callsign: (sponsor.nickname || 'OPERATOR').toUpperCase(),
        rankTitle,
        avatarTier,
        stampTitle: 'VERIFIED SPONSOR',
        stampSub: 'WARLINK OPERATOR',
        stampColor,
        amount: totalDonated > 0 ? `${totalDonated.toLocaleString('ru-RU')} ₽` : '0 ₽',
        isSecret: Boolean(sponsor.hide_donation_amount || sponsor.is_secret || sponsor.secret_donations),
        steamId: steamId,
        steamUrl: steamUrl,
        joinedDate: formattedJoined,
        motto: sponsor.motto || '',
        avatarUrl: sponsor.avatar_url || '',
        donations: sponsor.donations || []
    };
}

let dossierLoadingTimer = null;

function openSponsorDossierByIndex(idx) {
    if (typeof cachedSponsors === 'undefined' || !cachedSponsors || !cachedSponsors[idx]) return;
    openSponsorDossier(cachedSponsors[idx]);
}

function openMyDossier() {
    if (typeof cachedAccountProfile !== 'undefined' && cachedAccountProfile) {
        openSponsorDossier({
            nickname: cachedAccountProfile.nickname || 'МОЙ ПРОФИЛЬ',
            total_donated_rub: cachedAccountProfile.total_donated_rub || 0,
            joined_date: cachedAccountProfile.created_at || '27.09.2026',
            avatar_url: cachedAccountProfile.avatar_url || '',
            steam_id: cachedAccountProfile.steam_id || '',
            motto: cachedAccountProfile.motto || '',
            hide_donation_amount: Boolean(cachedAccountProfile.hide_donation_amount),
            donations: cachedAccountProfile.donations || []
        });
    } else {
        openSponsorDossier({
            nickname: 'ОПЕРАТОР ШЛЮЗА',
            total_donated_rub: 0,
            joined_date: '27.09.2026',
            donations: []
        });
    }
}

function openSponsorDossier(sponsor) {
    const modal = document.getElementById('modal-sponsor-dossier');
    const container = document.getElementById('dossier-svg-container');
    if (!modal || !container) return;

    if (dossierLoadingTimer) {
        clearTimeout(dossierLoadingTimer);
        dossierLoadingTimer = null;
    }

    modal.style.display = 'flex';
    container.innerHTML = renderLoadingDossierSvg();

    const data = prepareDossierData(sponsor);

    dossierLoadingTimer = setTimeout(() => {
        container.innerHTML = renderDossierSvg(
            data,
            data.isSecret,
            Boolean(data.steamId),
            Boolean(data.motto)
        );
        dossierLoadingTimer = null;
    }, 180);
}

function closeSponsorDossier() {
    const modal = document.getElementById('modal-sponsor-dossier');
    const container = document.getElementById('dossier-svg-container');
    if (modal) modal.style.display = 'none';
    if (container) container.innerHTML = '';
    if (dossierLoadingTimer) {
        clearTimeout(dossierLoadingTimer);
        dossierLoadingTimer = null;
    }
}
