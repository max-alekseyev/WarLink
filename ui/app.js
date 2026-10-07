window.WarLinkFlags = {
    flags: {},
    async fetch() {
        try {
            const res = await fetch('/api/features');
            if (res.ok) {
                const data = await res.json();
                if (data && data.flags) {
                    this.flags = data.flags;
                    window.dispatchEvent(new CustomEvent('warlink:flags_updated', { detail: this.flags }));
                }
            }
        } catch (e) {}
    },
    isEnabled(name) {
        return !!(this.flags[name] && this.flags[name].enabled);
    },
    getPayload(name) {
        return (this.flags[name] && this.flags[name].payload) || {};
    }
};

let isConnected = false;
let isBusy = false;
let isDownloadingDeps = false;
let isInitializing = false;
let freeInternetEnabled = false;
let selectedGameId = 'wardogs';
const defaultGames = [
    {
        id: 'wardogs',
        title: 'WARDOGS',
        steam_app_id: '1867240',
        icon_url: 'wardogs_icon.png',
        last_played: 1789653625,
        is_default: true,
        autolaunch: true,
        launch_count: 0
    },
    {
        id: 'arc_raiders',
        title: 'ARC Raiders',
        steam_app_id: '1808500',
        icon_url: 'arc_raiders_icon.png',
        last_played: 1789653525,
        is_default: true,
        autolaunch: true,
        launch_count: 0
    },
    {
        id: 'dark_and_darker',
        title: 'Dark and Darker',
        steam_app_id: '2016590',
        icon_url: 'dark_and_darker_icon.png',
        last_played: 1789653425,
        is_default: true,
        autolaunch: true,
        launch_count: 0
    },
    {
        id: 'aion2',
        title: 'AION 2',
        steam_app_id: '3393110',
        icon_url: 'aion2_icon.png',
        last_played: 1789653325,
        is_default: true,
        autolaunch: true,
        launch_count: 0
    }
];

function loadCachedShowcaseGames() {
    try {
        const raw = localStorage.getItem('warlink_showcase_games');
        if (raw) {
            const parsed = JSON.parse(raw);
            if (Array.isArray(parsed) && parsed.length > 0) {
                const existingIds = new Set(parsed.map(g => g.id));
                const list = [...parsed];
                for (const defG of defaultGames) {
                    if (!existingIds.has(defG.id)) {
                        list.push(defG);
                    }
                }
                return list.map(g => {
                    if (g.id === 'wardogs' && (!g.icon_url || g.icon_url.includes('steamstatic.com'))) g.icon_url = 'wardogs_icon.png';
                    if (g.id === 'arc_raiders' && (!g.icon_url || g.icon_url.includes('steamstatic.com'))) g.icon_url = 'arc_raiders_icon.png';
                    if (g.id === 'dark_and_darker' && (!g.icon_url || g.icon_url.includes('steamstatic.com'))) g.icon_url = 'dark_and_darker_icon.png';
                    if (g.id === 'aion2' && (!g.icon_url || g.icon_url.includes('steamstatic.com'))) g.icon_url = 'aion2_icon.png';
                    return g;
                });
            }
        }
    } catch (e) {}
    return defaultGames;
}

function loadCachedStatus() {
    try {
        const raw = localStorage.getItem('warlink_status_cache');
        if (raw) return JSON.parse(raw);
    } catch (e) {}
    return null;
}

let cachedGames = loadCachedShowcaseGames();
let launchingGameId = null;
let lastShowcaseStateKey = '';
let lastSavedStatusKey = '';
let isVotingEnabled = true;
let isDonateEnabled = true;

const GAME_ICON_FALLBACK_SVG = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
    <line x1="6" x2="10" y1="11" y2="11" />
    <line x1="8" x2="8" y1="9" y2="13" />
    <line x1="15" x2="15.01" y1="12" y2="12" />
    <line x1="18" x2="18.01" y1="10" y2="10" />
    <path d="M17.32 5H6.68a4 4 0 0 0-3.978 3.59c-.006.052-.01.101-.017.152C2.604 9.416 2 14.456 2 16a3 3 0 0 0 3 3c1 0 1.5-.5 2-1l1.414-1.414A2 2 0 0 1 9.828 16h4.344a2 2 0 0 1 1.414.586L17 18c.5.5 1 1 2 1a3 3 0 0 0 3-3c0-1.545-.604-6.584-.685-7.258-.007-.05-.011-.1-.017-.151A4 4 0 0 0 17.32 5z" />
</svg>`;

// --- Route Dispatcher ---
function applyInitialRoute() {
    const urlParams = new URLSearchParams(window.location.search);
    const initialView = urlParams.get('view');
    if (initialView) {
        switchView(initialView);
    }
}

// --- Automated Crash Telemetry (Zero Overhead) ---
let lastReportedCrashTime = 0;
let lastReportedCrashSig = '';

function reportClientCrash(errorType, message, stack) {
    const now = Date.now();
    const sig = `${errorType}:${message}`;
    if (sig === lastReportedCrashSig && (now - lastReportedCrashTime) < 180000) {
        return; // Suppress duplicate error spam within 3 minutes
    }
    lastReportedCrashSig = sig;
    lastReportedCrashTime = now;

    const payload = {
        error_type: errorType || 'js_uncaught',
        message: String(message || 'Unknown error').slice(0, 1000),
        stack_trace: String(stack || '').slice(0, 4000),
        context: {
            is_connected: isConnected,
            selected_game_id: selectedGameId,
            free_internet: freeInternetEnabled
        }
    };

    fetch('/api/telemetry/crash', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
    }).catch(() => {});
}

window.addEventListener('error', (event) => {
    reportClientCrash('js_uncaught', event.message, event.error ? event.error.stack : `${event.filename}:${event.lineno}:${event.colno}`);
});

window.addEventListener('unhandledrejection', (event) => {
    const reason = event.reason;
    reportClientCrash('js_unhandled_rejection', reason ? (reason.message || String(reason)) : 'Unhandled Promise Rejection', reason ? reason.stack : '');
});

// --- Skeletons & Shimmer Generators (Zero Layout Shift) ---
function getNotificationsSkeletonHtml(count = 2) {
    let html = '';
    for (let i = 0; i < count; i++) {
        html += `
            <div class="notification-card skeleton-card">
                <div class="notif-header">
                    <div class="notif-title-row">
                        <div class="skeleton" style="width: 52px; height: 16px; border-radius: 2px;"></div>
                        <div class="skeleton" style="width: ${130 + (i % 2) * 35}px; height: 12px;"></div>
                    </div>
                    <div class="skeleton" style="width: 48px; height: 10px;"></div>
                </div>
                <div class="skeleton" style="width: 90%; height: 11px; margin-top: 4px;"></div>
                <div class="skeleton" style="width: ${60 + (i % 2) * 20}%; height: 11px; margin-top: 2px;"></div>
            </div>
        `;
    }
    return html;
}

// --- Window Dragging and Controls ---
function handleTitlebarMouseDown(e) {
    if (e.target.closest('.win-btn') || e.target.closest('.free-net-toggle')) return;
    if (window.dragWindow) {
        window.dragWindow();
    }
}

function handleMinimize(e) {
    e.stopPropagation();
    if (window.minimizeWindow) {
        window.minimizeWindow();
    }
}

function handleClose(e) {
    e.stopPropagation();
    if (window.closeWindow) {
        window.closeWindow();
    }
}

function switchView(targetViewId) {
    const allViews = ['view-details', 'view-notifications', 'view-sponsors', 'view-account', 'view-progression', 'view-support', 'view-gold-market'];
    const showcase = document.getElementById('view-showcase');
    const targetEl = targetViewId ? document.getElementById(targetViewId) : null;
    const isAlreadyOpen = targetEl && targetEl.classList.contains('active');

    // Deactivate all sub-views atomically
    for (const vId of allViews) {
        const el = document.getElementById(vId);
        if (el) {
            el.classList.remove('active');
            el.style.display = '';
        }
    }
    if (window.ProgressionController && typeof window.ProgressionController.onClose === 'function') {
        window.ProgressionController.onClose();
    }

    const nextActiveId = (!targetViewId || isAlreadyOpen) ? null : targetViewId;

    if (!nextActiveId) {
        // Return to showcase
        if (showcase) {
            showcase.classList.add('active');
            showcase.style.display = '';
        }
    } else {
        if (showcase) {
            showcase.classList.remove('active');
            showcase.style.display = '';
        }
        if (targetEl) {
            targetEl.classList.add('active');
            targetEl.style.display = '';
        }

        // Trigger cached/background data refreshes without wiping DOM
        if (nextActiveId === 'view-sponsors' && typeof fetchSponsors === 'function') fetchSponsors();
        if (nextActiveId === 'view-account' && typeof fetchAccountProfile === 'function') fetchAccountProfile();
        if (nextActiveId === 'view-notifications' && typeof fetchNotifications === 'function') fetchNotifications();
        if (nextActiveId === 'view-progression' && window.ProgressionController) window.ProgressionController.onOpen();
        if (nextActiveId === 'view-support' && typeof openSupportChat === 'function') openSupportChat();
        if (nextActiveId === 'view-gold-market' && typeof openGoldMarket === 'function') openGoldMarket();
        if (nextActiveId === 'view-details' && typeof loadWardogsTweaksStatus === 'function') loadWardogsTweaksStatus();
    }

    updateTitlebarActiveState(nextActiveId);
}

function updateTitlebarActiveState(activeViewId) {
    const btnMap = {
        'view-account': 'btn-account-toggle',
        'view-sponsors': 'btn-sponsors-toggle',
        'view-notifications': 'btn-notif-toggle',
        'view-progression': 'btn-progression-toggle',
        'view-gold-market': 'btn-gold-market-toggle',
        'view-support': 'btn-support-toggle',
        'view-details': 'btn-settings-toggle'
    };
    for (const [vId, btnId] of Object.entries(btnMap)) {
        const btn = document.getElementById(btnId);
        if (btn) {
            btn.classList.toggle('is-active', vId === activeViewId);
        }
    }
}

function toggleGoldMarket(e) {
    if (e) e.stopPropagation();
    switchView('view-gold-market');
}

function toggleSupportChat(e) {
    if (e) e.stopPropagation();
    switchView('view-support');
}

function toggleProgression(e) {
    if (e) e.stopPropagation();
    switchView('view-progression');
}

function toggleDetails(e) {
    if (e) e.stopPropagation();
    switchView('view-details');
}


function togglePrivacyInfo(e) {
    if (e) e.stopPropagation();
    const box = document.getElementById('privacy-info-box');
    const btn = document.getElementById('btn-privacy-info');
    if (!box) return;
    const isHidden = (box.style.display === 'none' || !box.style.display);
    box.style.display = isHidden ? 'block' : 'none';
    if (btn) btn.classList.toggle('is-active', isHidden);
}

// --- Complex Mode Toggle (Titlebar) ---
let isTogglingFreeNet = false;

async function toggleFreeInternet(e) {
    if (e) e.stopPropagation();
    if (isTogglingFreeNet) return;

    const ctrl = document.getElementById('free-net-control');
    isTogglingFreeNet = true;
    if (ctrl) {
        ctrl.style.pointerEvents = 'none';
        ctrl.style.opacity = '0.6';
    }

    const newTarget = !freeInternetEnabled;
    freeInternetEnabled = newTarget;
    updateFreeInternetUI(newTarget);

    try {
        const resp = await fetch('/api/free-internet', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ enabled: newTarget })
        });
        if (resp.ok) {
            const data = await resp.json();
            if (typeof data.enabled === 'boolean') {
                freeInternetEnabled = data.enabled;
                updateFreeInternetUI(data.enabled);
            }
        } else {
            const errData = await resp.json().catch(() => ({}));
            freeInternetEnabled = !newTarget;
            updateFreeInternetUI(freeInternetEnabled);
            if (errData && errData.error) {
                showToast(errData.error);
                if (window.WarLinkAudio && typeof window.WarLinkAudio.playDenied === 'function') {
                    window.WarLinkAudio.playDenied();
                }
            }
        }
    } catch (err) {
        console.error('Ошибка переключения комплексного режима:', err);
        freeInternetEnabled = !newTarget;
        updateFreeInternetUI(freeInternetEnabled);
        showToast('Ошибка сети при переключении комплексного режима');
    } finally {
        isTogglingFreeNet = false;
        if (ctrl) {
            ctrl.style.pointerEvents = '';
            ctrl.style.opacity = '';
        }
    }
}

function updateFreeInternetUI(enabled) {
    const ctrl = document.getElementById('free-net-control');
    if (ctrl) {
        if (enabled) {
            ctrl.classList.add('active');
        } else {
            ctrl.classList.remove('active');
        }
    }
}

// --- Games Showcase Render (Desktop-Style Shortcuts) ---
function renderShowcase(games, activeId) {
    cachedGames = games || [];
    const grid = document.getElementById('showcase-grid');
    if (!grid) return;

    // Strict priority for featured titles: 1. WARDOGS, 2. ARC Raiders, 3. Dark and Darker (dnd)
    const priorityOrder = {
        'wardogs': 1,
        'arc_raiders': 2,
        'dark_and_darker': 3
    };

    const sorted = [...cachedGames].sort((a, b) => {
        const pA = priorityOrder[a.id] || 999;
        const pB = priorityOrder[b.id] || 999;
        if (pA !== pB) {
            return pA - pB;
        }
        return (b.last_played || 0) - (a.last_played || 0);
    });

    // Avoid destroying and recreating DOM on every poll if state hasn't changed (prevents hover flickering)
    const stateKey = JSON.stringify(sorted) + '_' + activeId + '_' + isConnected + '_' + isBusy + '_' + launchingGameId + '_' + isVotingEnabled;
    if (stateKey === lastShowcaseStateKey && grid.children.length > 0) {
        return;
    }
    lastShowcaseStateKey = stateKey;

    try {
        if (typeof localStorage !== 'undefined' && sorted.length > 0) {
            localStorage.setItem('warlink_showcase_games', JSON.stringify(sorted));
        }
    } catch (e) {}

    let html = '';

    sorted.forEach(g => {
        const isSelected = (g.id === activeId);
        const isDefault = !!g.is_default;
        const isLaunching = (launchingGameId === g.id) || (isBusy && !isConnected && isSelected);

        let iconHtml = '';
        let iconSrc = g.icon_url;
        if (g.id === 'wardogs' && (!iconSrc || iconSrc.includes('steamstatic.com'))) iconSrc = 'wardogs_icon.png';
        if (g.id === 'arc_raiders' && (!iconSrc || iconSrc.includes('steamstatic.com'))) iconSrc = 'arc_raiders_icon.png';
        if (g.id === 'dark_and_darker' && (!iconSrc || iconSrc.includes('steamstatic.com'))) iconSrc = 'dark_and_darker_icon.png';
        if (g.id === 'aion2' && (!iconSrc || iconSrc.includes('steamstatic.com'))) iconSrc = 'aion2_icon.png';
        if (!iconSrc && g.id === 'wardogs') iconSrc = 'wardogs_icon.png';
        if (iconSrc) {
            iconHtml = `
                <img src="${escapeHtml(iconSrc)}" class="shortcut-icon-img" alt="${escapeHtml(g.title)}" onerror="this.style.display='none'; this.nextElementSibling.style.display='block';" />
                <svg class="shortcut-icon-fallback" style="display:none;" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <line x1="6" x2="10" y1="11" y2="11" />
                    <line x1="8" x2="8" y1="9" y2="13" />
                    <line x1="15" x2="15.01" y1="12" y2="12" />
                    <line x1="18" x2="18.01" y1="10" y2="10" />
                    <path d="M17.32 5H6.68a4 4 0 0 0-3.978 3.59c-.006.052-.01.101-.017.152C2.604 9.416 2 14.456 2 16a3 3 0 0 0 3 3c1 0 1.5-.5 2-1l1.414-1.414A2 2 0 0 1 9.828 16h4.344a2 2 0 0 1 1.414.586L17 18c.5.5 1 1 2 1a3 3 0 0 0 3-3c0-1.545-.604-6.584-.685-7.258-.007-.05-.011-.1-.017-.151A4 4 0 0 0 17.32 5z" />
                </svg>
            `;
        } else {
            iconHtml = `
                <svg class="shortcut-icon-fallback" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <line x1="6" x2="10" y1="11" y2="11" />
                    <line x1="8" x2="8" y1="9" y2="13" />
                    <line x1="15" x2="15.01" y1="12" y2="12" />
                    <line x1="18" x2="18.01" y1="10" y2="10" />
                    <path d="M17.32 5H6.68a4 4 0 0 0-3.978 3.59c-.006.052-.01.101-.017.152C2.604 9.416 2 14.456 2 16a3 3 0 0 0 3 3c1 0 1.5-.5 2-1l1.414-1.414A2 2 0 0 1 9.828 16h4.344a2 2 0 0 1 1.414.586L17 18c.5.5 1 1 2 1a3 3 0 0 0 3-3c0-1.545-.604-6.584-.685-7.258-.007-.05-.011-.1-.017-.151A4 4 0 0 0 17.32 5z" />
                </svg>
            `;
        }

        const activeDot = (isSelected && isConnected && !isLaunching) ? '<span class="shortcut-dot-active" title="В сети"></span>' : '';
        const spinnerOverlay = isLaunching ? `
            <div class="shortcut-spinner-overlay" title="Подключение и запуск...">
                <svg class="shortcut-spinner-icon" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M21 12a9 9 0 1 1-6.219-8.56" />
                </svg>
            </div>
        ` : '';

        const tooltip = (isSelected && isConnected) 
            ? `${escapeHtml(g.title)} (В сети • Кликните для отключения)` 
            : escapeHtml(g.title);

        html += `
            <div class="game-shortcut ${isSelected ? 'is-selected' : ''} ${isLaunching ? 'is-launching' : ''}" 
                 onclick="onGameClick('${g.id}')" 
                 oncontextmenu="showGameContextMenu(event, '${g.id}', ${isDefault})"
                 title="${tooltip}">
                <div class="shortcut-icon-wrapper">
                    ${iconHtml}
                    ${activeDot}
                    ${spinnerOverlay}
                </div>
                <div class="shortcut-title">${escapeHtml(g.title)}</div>
            </div>
        `;
    });

    // Add Shortcut Tile (if voting is enabled)
    if (isVotingEnabled) {
        html += `
            <div class="game-shortcut add-shortcut" id="btn-add-game" onclick="openAddGameModal()" title="Предложить или проголосовать за новую игру">
                <div class="add-shortcut-btn">
                    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M5 12h14" />
                        <path d="M12 5v14" />
                    </svg>
                </div>
                <span class="shortcut-title">Предложить</span>
            </div>
        `;
    }

    grid.innerHTML = html;
}

function escapeHtml(str) {
    if (!str && str !== 0) return '';
    return String(str)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
}


// 1-Click Game Action (One action, one screen)
function onGameClick(gameId) {
    if (isBusy || isDownloadingDeps || isInitializing) return;

    const targetGame = (cachedGames || []).find(g => g.id === gameId);
    if (targetGame && (targetGame.status === 'crowdfunding' || gameId === 'bf6')) {
        openDonateModalWithTab('boosty');
        showToast('Открыт спецпроект Battlefield 6');
        return;
    }

    if (isConnected && selectedGameId === gameId) {
        // Currently connected to this game -> disconnect
        toggleConnect();
    } else {
        // Quick connect and launch
        quickLaunchGame(gameId);
    }
}

async function quickLaunchGame(gameId) {
    lastShownError = '';
    selectedGameId = gameId;
    launchingGameId = gameId;
    renderShowcase(cachedGames, gameId);
    await selectGame(gameId);

    try {
        await fetch('/api/increment-launch', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id: gameId })
        });
    } catch (e) {}

    if (!isConnected && !isBusy && !isDownloadingDeps && !isInitializing) {
        toggleConnect();
    } else if (isConnected) {
        setTimeout(() => {
            if (launchingGameId === gameId) {
                launchingGameId = null;
                renderShowcase(cachedGames, selectedGameId);
            }
        }, 1200);
    }
}

// --- Context Menu Management ---
let activeContextMenuGameId = null;
let activeContextMenuIsDefault = false;

function showGameContextMenu(e, gameId, isDefault) {
    e.preventDefault();
    e.stopPropagation();

    activeContextMenuGameId = gameId;
    activeContextMenuIsDefault = !!isDefault;

    const game = cachedGames.find(g => g.id === gameId);
    const menu = document.getElementById('game-context-menu');
    if (!menu) return;

    const btnConnect = document.getElementById('ctx-btn-connect');
    if (btnConnect) {
        const isGameActive = isConnected && (selectedGameId === gameId);
        const span = btnConnect.querySelector('span');
        if (span) span.textContent = isGameActive ? 'Отключиться' : 'Подключиться';
    }

    const btnAutolaunch = document.getElementById('ctx-btn-autolaunch');
    if (btnAutolaunch && game) {
        const isAuto = (game.autolaunch !== false); // default to true
        btnAutolaunch.classList.toggle('is-checked', isAuto);
    }

    const btnDelete = document.getElementById('ctx-btn-delete');
    if (btnDelete) {
        btnDelete.style.display = activeContextMenuIsDefault ? 'none' : 'flex';
    }

    menu.style.display = 'flex';
    const menuWidth = menu.offsetWidth || 180;
    const menuHeight = menu.offsetHeight || 110;

    let posX = e.clientX;
    let posY = e.clientY;

    if (posX + menuWidth > window.innerWidth) {
        posX = window.innerWidth - menuWidth - 8;
    }
    if (posY + menuHeight > window.innerHeight) {
        posY = window.innerHeight - menuHeight - 8;
    }

    menu.style.left = `${Math.max(4, posX)}px`;
    menu.style.top = `${Math.max(4, posY)}px`;
}

function hideGameContextMenu() {
    const menu = document.getElementById('game-context-menu');
    if (menu) {
        menu.style.display = 'none';
    }
    activeContextMenuGameId = null;
    activeContextMenuIsDefault = false;
}

function onContextConnect() {
    const gid = activeContextMenuGameId;
    hideGameContextMenu();
    if (gid) {
        if (isConnected && selectedGameId === gid) {
            toggleConnect();
        } else {
            quickLaunchGame(gid);
        }
    }
}

async function onContextToggleAutolaunch() {
    const gid = activeContextMenuGameId;
    hideGameContextMenu();
    if (!gid) return;

    try {
        const res = await fetch('/api/toggle-autolaunch', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id: gid })
        });
        if (res.ok) {
            const data = await res.json();
            const game = cachedGames.find(g => g.id === gid);
            if (game) {
                game.autolaunch = data.autolaunch;
            }
        }
    } catch (err) {
        console.error('Error toggling autolaunch:', err);
    }
}

function onContextDelete() {
    const gid = activeContextMenuGameId;
    const isDef = activeContextMenuIsDefault;
    hideGameContextMenu();
    if (gid && !isDef) {
        deleteGame(null, gid);
    }
}

document.addEventListener('click', (e) => {
    if (!e.target.closest('#game-context-menu')) {
        hideGameContextMenu();
    }
});

document.addEventListener('contextmenu', (e) => {
    if (!e.target.closest('.game-shortcut')) {
        hideGameContextMenu();
    }
});

// Modal backdrop click-to-close
function handleModalBackdropDismiss(e) {
    if (e.target !== e.currentTarget) return;
    const id = e.target.id;
    if (id === 'modal-sponsor-dossier' && typeof closeSponsorDossier === 'function') {
        closeSponsorDossier();
    } else if (id === 'modal-donate-custom' && typeof closeDonateModal === 'function') {
        closeDonateModal();
    } else if (id === 'modal-add-game' && typeof closeAddGameModal === 'function') {
        closeAddGameModal();
    } else if (id === 'modal-bug-report' && typeof closeBugReportModal === 'function') {
        closeBugReportModal();
    }
}

function onModalBackdropMouseDown(e) {
    handleModalBackdropDismiss(e);
}

function onModalBackdropClick(e) {
    handleModalBackdropDismiss(e);
}

// Global Keyboard Navigation (Escape to dismiss modals, sheets, and menus)
document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
        const brModal = document.getElementById('modal-bug-report');
        if (brModal && brModal.style.display !== 'none') {
            closeBugReportModal();
            return;
        }
        const dossierModal = document.getElementById('modal-sponsor-dossier');
        if (dossierModal && dossierModal.style.display !== 'none') {
            closeSponsorDossier();
            return;
        }
        const modal = document.getElementById('modal-add-game');
        if (modal && modal.style.display !== 'none') {
            closeAddGameModal();
            return;
        }
        const donateModal = document.getElementById('modal-donate-custom');
        if (donateModal && donateModal.style.display !== 'none') {
            closeDonateModal();
            return;
        }
        const activeSubView = ['view-account', 'view-sponsors', 'view-notifications', 'view-details'].some(id => {
            const el = document.getElementById(id);
            return el && el.style.display === 'flex';
        });
        if (activeSubView) {
            switchView(null);
            return;
        }
        hideGameContextMenu();
    }
});

// --- Functional Toast Notification ---
let toastTimer = null;
let lastShownError = '';

function showToast(msg, durationMs = 4500) {
    if (!msg) return;
    const toast = document.getElementById('ui-toast');
    const toastMsg = document.getElementById('ui-toast-msg');
    if (!toast || !toastMsg) return;

    if (toastTimer) {
        clearTimeout(toastTimer);
        toastTimer = null;
    }

    toastMsg.textContent = msg;
    toast.classList.remove('toast-hiding');
    toast.style.display = 'flex';

    toastTimer = setTimeout(() => {
        toast.classList.add('toast-hiding');
        setTimeout(() => {
            toast.style.display = 'none';
            toast.classList.remove('toast-hiding');
            toastTimer = null;
        }, 200);
    }, durationMs);
}

async function selectGame(gameId) {
    selectedGameId = gameId;
    try {
        await fetch('/api/select-game', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id: gameId })
        });
    } catch (e) {}
}

async function deleteGame(e, gameId) {
    if (e) e.stopPropagation();

    try {
        await fetch('/api/delete-game', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id: gameId })
        });
        lastShowcaseStateKey = '';
        fetchStatus();
        showToast('Игра удалена из витрины');
    } catch (e) {
        console.error('Delete game error:', e);
    }
}


// --- API / State Sync ---
let isFetchingStatus = false;
async function fetchStatus() {
    if (isFetchingStatus) return;
    isFetchingStatus = true;
    try {
        const resp = await fetch('/api/status');
        if (resp.ok) {
            const data = await resp.json();
            updateUI(data);
        }
    } catch (err) {
        console.error('Fetch status error:', err);
    } finally {
        isFetchingStatus = false;
    }
}

function updateUI(data) {
    if (data.version) {
        const verEl = document.querySelector('.brand-version');
        if (verEl && verEl.textContent !== data.version) {
            verEl.textContent = data.version;
        }
    }

    isConnected = !!data.is_connected;
    isBusy = !!data.is_busy;
    isDownloadingDeps = !!data.is_downloading_deps;
    isInitializing = !!data.is_initializing;

    // Release blocking overlay when not actively launching
    if (!isBusy && !isDownloadingDeps && !isInitializing) {
        launchingGameId = null;
    }

    // Auto-updater overlay
    const updateOverlay = document.getElementById('view-update-overlay');
    if (updateOverlay) {
        if (data.is_updating) {
            updateOverlay.style.display = 'flex';
            const ut = document.getElementById('update-title');
            const um = document.getElementById('update-msg');
            const ub = document.getElementById('update-bar');
            if (ut) ut.textContent = 'Обновление WarLink...';
            if (um) um.textContent = data.update_msg || 'Загрузка новой версии...';
            if (ub) ub.style.width = `${data.update_pct || 0}%`;
            return;
        } else if (data.is_initializing) {
            updateOverlay.style.display = 'flex';
            const ut = document.getElementById('update-title');
            const um = document.getElementById('update-msg');
            const ub = document.getElementById('update-bar');
            if (ut) ut.textContent = data.init_title || 'Инициализация WarLink...';
            if (um) um.textContent = data.init_msg || 'Подготовка сетевых компонентов...';
            if (ub) ub.style.width = `${data.init_pct || 0}%`;
            return;
        } else {
            updateOverlay.style.display = 'none';
        }
    }

    // Update Free Internet toggle (only when not actively user-toggling)
    if (!isTogglingFreeNet) {
        freeInternetEnabled = !!data.free_internet;
        updateFreeInternetUI(freeInternetEnabled);
    }

    // Update showcase grid
    if (data.games) {
        renderShowcase(data.games, data.selected_game_id);
    }

    // Dynamic Community Goal Visibility & October Pool
    if (typeof data.october_pool_rub === 'number') {
        window.octoberPoolRub = data.october_pool_rub;
    }
    if (data.enable_community_goal !== undefined) {
        window.enableCommunityGoal = Boolean(data.enable_community_goal);
        if (typeof updateCommunityGoalVisibility === 'function') {
            updateCommunityGoalVisibility(window.enableCommunityGoal);
        }
    }

    // Sound effects toggle sync
    if (typeof data.enable_sound_effects === 'boolean') {
        const sndChk = document.getElementById('check-sound-effects');
        if (sndChk && document.activeElement !== sndChk) {
            sndChk.checked = data.enable_sound_effects;
        }
        if (window.WarLinkAudio && typeof window.WarLinkAudio.setEnabled === 'function') {
            // keep local memory in sync without re-triggering post
            if (window.WarLinkAudio.isEnabled() !== data.enable_sound_effects) {
                try {
                    localStorage.setItem('wl_sound_effects', data.enable_sound_effects ? 'true' : 'false');
                } catch(e) {}
            }
        }
    }

    // Handle error notifications (e.g. 100/100 slots full or network failure)
    if (data.last_error) {
        launchingGameId = null;
        if (data.last_error !== lastShownError) {
            lastShownError = data.last_error;
            showToast(data.last_error);
            if (window.WarLinkAudio && typeof window.WarLinkAudio.playDenied === 'function') {
                window.WarLinkAudio.playDenied();
            }
        }
    } else {
        lastShownError = '';
    }

    // Reactive Stockholm Gateway status dot
    const gwDot = document.querySelector('.gateway-dot');
    if (gwDot) {
        gwDot.classList.remove('is-online', 'is-connecting', 'is-offline');
        if (isBusy || isDownloadingDeps || isInitializing) {
            gwDot.classList.add('is-connecting');
            gwDot.title = 'Подключение к шлюзу...';
        } else if (data.gateway_ping && data.gateway_ping > 1) {
            gwDot.classList.add('is-online');
            gwDot.title = 'Игровой шлюз (' + (data.gateway_badge || 'Онлайн') + ') активен';
        } else {
            gwDot.classList.add('is-offline');
            gwDot.title = 'Шлюз недоступен';
        }
    }

    // Update Gateway footer metrics
    const gwPingEl = document.getElementById('gw-ping');
    const gwSlotsEl = document.getElementById('gw-slots');
    const gwDaysEl = document.getElementById('gw-days');
    const btnDonateText = document.getElementById('btn-donate-text');
    if (gwPingEl) {
        if (data.ping_label && data.ping_label !== '— мс' && data.ping_label !== '—') {
            gwPingEl.textContent = data.ping_label;
            if (data.gateway_ping && data.gateway_ping > 1) {
                gwPingEl.title = `Пинг ПК -> Шлюз (${data.gateway_badge || 'Игровой'}): ${data.gateway_ping} мс`;
            }
        } else if (data.gateway_ping && data.gateway_ping > 1) {
            gwPingEl.textContent = data.gateway_ping + ' мс';
            gwPingEl.title = `Пинг ПК -> Шлюз (${data.gateway_badge || 'Игровой'}): ${data.gateway_ping} мс`;
        } else {
            if (!gwPingEl.querySelector('.skeleton')) {
                gwPingEl.innerHTML = '<span class="skeleton" style="width: 36px; height: 10px;"></span>';
            }
            gwPingEl.title = 'Измерение сетевой задержки...';
        }
    }
    if (gwSlotsEl) {
        const slots = data.gateway_slots;
        if (slots && slots !== '—' && slots !== '—/—') {
            gwSlotsEl.textContent = slots;
            const isFull = slots.includes('50/50') || slots.includes('60/60') || (data.gateway_full === true);
            gwSlotsEl.classList.toggle('slots-full', isFull);
            gwSlotsEl.title = data.gateway_slots_tooltip || 'Активные слоты игрового шлюза';
        } else if (!gwSlotsEl.textContent || gwSlotsEl.textContent === '—') {
            if (!gwSlotsEl.querySelector('.skeleton')) {
                gwSlotsEl.innerHTML = '<span class="skeleton" style="width: 30px; height: 10px;"></span>';
            }
            gwSlotsEl.title = 'Получение статуса слотов шлюза...';
        }
    }
    if (gwDaysEl) {
        if (data.gateway_days && data.gateway_days > 0) {
            gwDaysEl.textContent = `${data.gateway_days} дн`;
            gwDaysEl.title = `Оплачено дней работы шлюза: ${data.gateway_days} дн`;
        } else if (data.gateway_days === 0 && data.gateway_slots && data.gateway_slots !== '—') {
            gwDaysEl.textContent = '30 дн';
            gwDaysEl.title = 'Оплачено дней работы шлюза: 30 дн';
        } else if (!gwDaysEl.textContent || gwDaysEl.textContent === '—') {
            if (!gwDaysEl.querySelector('.skeleton')) {
                gwDaysEl.innerHTML = '<span class="skeleton" style="width: 38px; height: 10px;"></span>';
            }
            gwDaysEl.title = 'Получение статуса оплаты шлюза...';
        }
    }

    // Sync Account and Profile UI
    if (data.account_number) {
        const accNumEl = document.getElementById('val-account-number');
        if (accNumEl && accNumEl.textContent !== data.account_number) {
            accNumEl.textContent = data.account_number;
        }
    }
    const nickInput = document.getElementById('input-nickname');
    if (nickInput && document.activeElement !== nickInput && data.nickname) {
        nickInput.value = data.nickname;
    }
    const tierBadge = document.getElementById('profile-tier-badge');
    if (tierBadge) {
        if (data.account_tier === 'admin' || data.is_admin) {
            tierBadge.textContent = 'Статус: Администратор (Админ-слот 61/61)';
            tierBadge.classList.remove('tier-sponsor');
            tierBadge.classList.add('tier-admin');
        } else if (data.is_sponsor) {
            tierBadge.textContent = 'Статус: Спонсор шлюза';
            tierBadge.classList.remove('tier-admin');
            tierBadge.classList.add('tier-sponsor');
        } else {
            tierBadge.textContent = 'Статус: Базовый доступ';
            tierBadge.classList.remove('tier-sponsor', 'tier-admin');
        }
    }
    updateAvatarDisplays(data.avatar_url);
    const gwTitleEl = document.getElementById('gw-title') || document.querySelector('.gateway-title');
    if (gwTitleEl) {
        if (data.gateway_badge) {
            gwTitleEl.textContent = data.gateway_badge;
        } else if (data.gateway_location) {
            gwTitleEl.textContent = data.gateway_location.split(',')[0].trim();
        }
    }
    const routeSelect = document.getElementById('select-network-route');
    if (routeSelect && data.network_route_mode && document.activeElement !== routeSelect) {
        routeSelect.value = data.network_route_mode;
        updateRouteModeDescription(data.network_route_mode);
    }
    const btnDonate = document.getElementById('btn-donate-server');
    if (btnDonateText) {
        btnDonateText.textContent = 'Поддержать сервер';
        if (btnDonate) {
            btnDonate.title = 'Поддержать сервер через СБП';
        }
    }

    // Dynamic live feature toggles (Donate & Voting)
    if (typeof data.enable_voting === 'boolean' && isVotingEnabled !== data.enable_voting) {
        isVotingEnabled = data.enable_voting;
        if (!isVotingEnabled) {
            closeAddGameModal();
        }
        lastShowcaseStateKey = '';
        renderShowcase(cachedGames, selectedGameId);
    }

    if (typeof data.enable_donate === 'boolean') {
        isDonateEnabled = data.enable_donate;
        window.isDonateEnabled = isDonateEnabled;
        if (btnDonate) {
            btnDonate.style.display = isDonateEnabled ? 'inline-flex' : 'none';
        }
        document.querySelectorAll('.btn-author-donate, .btn-compact-donate, .account-donate-links').forEach(el => {
            el.style.display = isDonateEnabled ? '' : 'none';
        });
    }

    if (typeof updateDonateMethodsState === 'function') {
        updateDonateMethodsState(data);
    }

    // Profile options in Settings
    const selectProfile = document.getElementById('select-profile');
    if (selectProfile && data.available_profiles && data.available_profiles.length > 0) {
        const currentOptions = Array.from(selectProfile.options).map(o => o.value);
        const newOptions = data.available_profiles;
        if (currentOptions.join(',') !== newOptions.join(',')) {
            selectProfile.innerHTML = '';
            newOptions.forEach(p => {
                const opt = document.createElement('option');
                opt.value = p;
                opt.textContent = p;
                selectProfile.appendChild(opt);
            });
        }
        if (data.profile) {
            selectProfile.value = data.profile;
        }
    }

    // Auto-hosts count
    const autoCountEl = document.getElementById('auto-hosts-count');
    if (autoCountEl && typeof data.auto_hosts_count === 'number') {
        autoCountEl.textContent = data.auto_hosts_count;
    }

    // Circular orchestrator strategy indicator
    const circularRow = document.getElementById('circular-strategy-row');
    if (circularRow) {
        circularRow.style.display = data.circular_active ? 'flex' : 'none';
    }

    try {
        if (typeof localStorage !== 'undefined' && data) {
            const statusKey = `${data.is_connected}_${data.is_busy}_${data.free_internet}_${data.selected_game_id}_${data.last_error}_${data.gateway_ping}_${data.gateway_slots}_${data.october_pool_rub}`;
            if (statusKey !== lastSavedStatusKey) {
                lastSavedStatusKey = statusKey;
                localStorage.setItem('warlink_status_cache', JSON.stringify(data));
            }
        }
    } catch(e) {}
}

async function toggleConnect() {
    lastShownError = '';
    if (isBusy || isDownloadingDeps || isInitializing) return;

    if (isConnected) {
        try {
            await fetch('/api/disconnect');
        } catch (e) {}
    } else {
        lastShownError = '';
        launchingGameId = selectedGameId;
        renderShowcase(cachedGames, selectedGameId);
        try {
            const resp = await fetch('/api/connect');
            if (!resp.ok) {
                const errData = await resp.json().catch(() => ({}));
                if (errData && errData.error) {
                    showToast(errData.error);
                }
            }
        } catch (e) {}
    }
    fetchStatus();
}

async function onProfileChange(val) {
    if (!val) return;
    try {
        await fetch('/api/profile', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ profile: val })
        });
    } catch (e) {
        console.error('Profile change error:', e);
    }
}

const ROUTE_DESCRIPTIONS = {
    'direct_moscow': 'Прямое подключение по России (минимальный пинг ~15-25 мс)',
    'direct_frankfurt': 'Прямое европейское подключение, Франкфурт (~40-50 мс)',
    'transit': 'Транзитный коридор через Москву во Франкфурт (~50-60 мс)',
    'direct_stockholm': 'Прямое европейское подключение, Франкфурт (~40-50 мс)'
};

function updateRouteModeDescription(mode) {
    const descEl = document.getElementById('route-mode-desc-val');
    if (descEl) {
        descEl.textContent = ROUTE_DESCRIPTIONS[mode] || ROUTE_DESCRIPTIONS['transit'];
    }
}

async function onNetworkRouteModeChange(mode) {
    updateRouteModeDescription(mode);
    try {
        const res = await fetch('/api/network-route', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ mode })
        });
        const data = await res.json();
        if (data && data.success) {
            const badge = mode === 'direct_frankfurt' ? 'Франкфурт' : (mode === 'direct_moscow' ? 'Москва' : (mode === 'direct_stockholm' ? 'Франкфурт' : 'Мск → Франкфурт'));
            showToast('Маршрут переключен: ' + badge);
            if (typeof UIStore !== 'undefined') {
                UIStore.invalidate('/api/status');
            }
        }
    } catch (e) {
        showToast('Ошибка переключения маршрута');
    }
}

// --- Quick Gateway Route Popover & Routing Feedback ---
let currentRouteModes = [
    {
        id: 'direct_moscow',
        title: 'Москва',
        region: 'RU-DIRECT',
        iconSvg: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" class="server-node-icon"><rect x="2" y="3" width="20" height="5" rx="1"/><rect x="2" y="10" width="20" height="5" rx="1"/><rect x="2" y="17" width="20" height="5" rx="1"/><circle cx="6" cy="5.5" r="1" fill="currentColor"/><circle cx="6" cy="12.5" r="1" fill="currentColor"/><circle cx="6" cy="19.5" r="1" fill="currentColor"/></svg>`
    },
    {
        id: 'direct_frankfurt',
        title: 'Франкфурт',
        region: 'EU-DIRECT',
        iconSvg: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" class="server-node-icon"><circle cx="12" cy="12" r="9"/><path d="M12 3a14.5 14.5 0 0 0 0 18"/><path d="M12 3a14.5 14.5 0 0 1 0 18"/><line x1="3" y1="12" x2="21" y2="12"/></svg>`
    },
    {
        id: 'transit',
        title: 'Москва → Франкфурт',
        region: 'TRANSIT',
        iconSvg: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" class="server-node-icon"><rect x="2" y="4" width="7" height="6" rx="1"/><rect x="15" y="14" width="7" height="6" rx="1"/><path d="M9 7h4a3 3 0 0 1 3 3v4"/><polyline points="14 12 16 14 18 12"/></svg>`
    }
];

async function toggleGatewayRoutePopover(e) {
    if (e) e.stopPropagation();
    const popover = document.getElementById('gateway-route-popover');
    const chevron = document.getElementById('gw-chevron');
    if (!popover) return;

    const isOpen = popover.style.display !== 'none';
    if (isOpen) {
        closeGatewayRoutePopover();
    } else {
        popover.style.display = 'flex';
        if (chevron) chevron.classList.add('is-open');
        await loadAndRenderGatewayRoutePopover();
    }
}

function closeGatewayRoutePopover() {
    const popover = document.getElementById('gateway-route-popover');
    const chevron = document.getElementById('gw-chevron');
    if (popover) popover.style.display = 'none';
    if (chevron) chevron.classList.remove('is-open');
}

async function loadAndRenderGatewayRoutePopover() {
    const listEl = document.getElementById('route-popover-list');
    if (!listEl) return;

    let activeMode = 'transit';
    try {
        const res = await fetch('/api/network-route');
        if (res.ok) {
            const data = await res.json();
            if (data && data.current_mode) {
                activeMode = data.current_mode;
            }
        }
    } catch (e) {}

    listEl.innerHTML = currentRouteModes.map(m => `
        <button type="button" class="route-popover-item ${m.id === activeMode ? 'active' : ''}" onclick="selectRouteModeFromPopover('${m.id}')">
            <div class="route-item-left">
                <div class="route-node-icon ${m.id}">
                    ${m.iconSvg}
                </div>
                <div class="route-node-meta">
                    <span class="route-node-name">${m.title}</span>
                    <span class="route-region-tag font-mono">${m.region}</span>
                </div>
            </div>
            <div class="route-item-right">
                <div class="route-check-indicator">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#FF5E1F" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                        <polyline points="20 6 9 17 4 12"/>
                    </svg>
                </div>
            </div>
        </button>
    `).join('');
}

async function selectRouteModeFromPopover(mode) {
    closeGatewayRoutePopover();
    await onNetworkRouteModeChange(mode);
    const radio = document.querySelector(`input[name="route-mode"][value="${mode}"]`);
    if (radio) radio.checked = true;
}

// Global click outside to dismiss popover
document.addEventListener('click', (e) => {
    const popover = document.getElementById('gateway-route-popover');
    const footerLeft = document.getElementById('gw-footer-left');
    if (popover && popover.style.display !== 'none') {
        if (!popover.contains(e.target) && (!footerLeft || !footerLeft.contains(e.target))) {
            closeGatewayRoutePopover();
        }
    }
});

// --- Multi-Step Routing Feedback Modal ---
const FEEDBACK_STEPS = [
    {
        mode: 'direct_moscow',
        title: '1. МОСКВА — ПРЯМОЙ УЗЕЛ РФ',
        sub: 'Прямое подключение по России (минимальный пинг ~15-25 мс)',
        label: 'Москва'
    },
    {
        mode: 'direct_frankfurt',
        title: '2. ФРАНКФУРТ — ЕВРОПЕЙСКИЙ ШЛЮЗ',
        sub: 'Прямое европейское подключение (~40-50 мс)',
        label: 'Франкфурт'
    },
    {
        mode: 'transit',
        title: '3. ТРАНЗИТ — ГИБРИДНЫЙ МАРШРУТ',
        sub: 'Москва → Франкфурт (~50-60 мс)',
        label: 'Транзит'
    }
];

let currentFeedbackStepIndex = 0;
let feedbackDraft = {
    'direct_moscow': { status: '', ping: '', match: 'perfect', discord: 'clean', comment: '' },
    'direct_frankfurt': { status: '', ping: '', match: 'perfect', discord: 'clean', comment: '' },
    'transit': { status: '', ping: '', match: 'perfect', discord: 'clean', comment: '' }
};

function loadFeedbackDraftFromStorage() {
    try {
        const raw = localStorage.getItem('warlink_routing_feedback_draft_v2');
        if (raw) {
            const parsed = JSON.parse(raw);
            if (parsed && typeof parsed === 'object') {
                for (const k of ['direct_moscow', 'direct_frankfurt', 'direct_stockholm', 'transit']) {
                    if (parsed[k]) feedbackDraft[k] = Object.assign({}, feedbackDraft[k], parsed[k]);
                }
            }
        }
    } catch(e) {}
}

function saveFeedbackDraftToStorage() {
    try {
        localStorage.setItem('warlink_routing_feedback_draft_v2', JSON.stringify(feedbackDraft));
    } catch(e) {}
}

function resetFeedbackDraft() {
    feedbackDraft = {
        'direct_moscow': { status: '', ping: '', match: 'perfect', discord: 'clean', comment: '' },
        'direct_frankfurt': { status: '', ping: '', match: 'perfect', discord: 'clean', comment: '' },
        'transit': { status: '', ping: '', match: 'perfect', discord: 'clean', comment: '' }
    };
    try { localStorage.removeItem('warlink_routing_feedback_draft_v2'); } catch(e) {}
    renderFeedbackCurrentStep();
    showToast('Черновик замеров сброшен');
}

function openRoutingFeedbackModal(e) {
    if (e) e.stopPropagation();
    closeGatewayRoutePopover();

    const modal = document.getElementById('modal-routing-feedback');
    const formCont = document.getElementById('rf-form-container');
    const successCont = document.getElementById('rf-success-container');

    if (!modal) return;
    if (formCont) formCont.style.display = 'flex';
    if (successCont) successCont.style.display = 'none';

    loadFeedbackDraftFromStorage();

    // Default to the first uncompleted step, or current active route
    const gwTitle = document.getElementById('gw-title');
    const currentActiveText = (gwTitle ? gwTitle.textContent : '').toLowerCase();
    let initialStep = 0;
    if (currentActiveText.includes('стокгольм') && !currentActiveText.includes('->')) {
        initialStep = 1;
    } else if (currentActiveText.includes('транзит') || currentActiveText.includes('->')) {
        initialStep = 2;
    }

    currentFeedbackStepIndex = initialStep;
    renderFeedbackCurrentStep();
    modal.style.display = 'flex';
}

function closeRoutingFeedbackModal() {
    const modal = document.getElementById('modal-routing-feedback');
    if (modal) modal.style.display = 'none';
}

function renderFeedbackCurrentStep() {
    const step = FEEDBACK_STEPS[currentFeedbackStepIndex];
    const data = feedbackDraft[step.mode] || {};

    const badge = document.getElementById('rf-step-badge');
    if (badge) badge.textContent = `ШАГ ${currentFeedbackStepIndex + 1} ИЗ 3`;

    // Update tabs
    for (let i = 0; i < FEEDBACK_STEPS.length; i++) {
        const tabEl = document.getElementById(`rf-tab-step-${i}`);
        const checkEl = document.getElementById(`rf-tab-check-${i}`);
        const m = FEEDBACK_STEPS[i].mode;
        const d = feedbackDraft[m];
        const isComplete = d && d.status && (parseInt(d.ping, 10) > 0);

        if (tabEl) {
            tabEl.classList.toggle('active', i === currentFeedbackStepIndex);
            tabEl.classList.toggle('completed', isComplete);
        }
        if (checkEl) {
            checkEl.innerHTML = isComplete ? '<svg width="9" height="9" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>' : `${i + 1}`;
        }
    }

    // Check if this route is currently active for ping auto-fill
    const gwTitle = document.getElementById('gw-title');
    const currentActiveText = (gwTitle ? gwTitle.textContent : '').toLowerCase();
    let isActive = false;
    if (step.mode === 'transit' && (currentActiveText.includes('транзит') || currentActiveText.includes('->') || currentActiveText.includes('→'))) isActive = true;
    else if ((step.mode === 'direct_frankfurt' || step.mode === 'direct_stockholm') && (currentActiveText.includes('франкфурт') || currentActiveText.includes('стокгольм')) && !currentActiveText.includes('->') && !currentActiveText.includes('→')) isActive = true;
    else if (step.mode === 'direct_moscow' && currentActiveText.includes('москва') && !currentActiveText.includes('->') && !currentActiveText.includes('→')) isActive = true;

    // Status pills
    const pills = document.querySelectorAll('.rf-status-pill');
    pills.forEach(pill => {
        pill.classList.toggle('active', pill.getAttribute('data-status') === (data.status || ''));
    });

    // Ping input
    const pingInput = document.getElementById('rf-step-ping');
    if (pingInput) {
        pingInput.value = data.ping || '';
        if (!data.ping && isActive) {
            const gwPingEl = document.getElementById('gw-ping');
            if (gwPingEl) {
                const match = (gwPingEl.textContent || '').match(/\d+/);
                if (match) {
                    pingInput.value = match[0];
                    data.ping = match[0];
                    saveFeedbackDraftToStorage();
                }
            }
        }
    }

    // Match quality
    const matchEl = document.getElementById('rf-step-match');
    if (matchEl) matchEl.value = data.match || 'perfect';

    // Discord status
    const discordEl = document.getElementById('rf-step-discord');
    if (discordEl) discordEl.value = data.discord || 'clean';

    // Comment
    const commentEl = document.getElementById('rf-step-comment');
    const counterEl = document.getElementById('rf-step-counter');
    if (commentEl) commentEl.value = data.comment || '';
    if (counterEl) counterEl.textContent = `${(data.comment || '').length} / 300`;

    // Error hide
    const warn = document.getElementById('rf-status-error');
    if (warn) warn.style.display = 'none';

    // Summary count
    let completedCount = 0;
    for (const s of FEEDBACK_STEPS) {
        const d = feedbackDraft[s.mode];
        if (d && d.status && parseInt(d.ping, 10) > 0) completedCount++;
    }
    const summaryEl = document.getElementById('rf-progress-summary');
    if (summaryEl) summaryEl.textContent = `Заполнено: ${completedCount} / 3`;

    // Footer buttons
    const btnPrev = document.getElementById('btn-rf-prev');
    const btnNext = document.getElementById('btn-rf-next');
    const btnSubmit = document.getElementById('btn-rf-submit');

    if (btnPrev) {
        btnPrev.style.display = currentFeedbackStepIndex > 0 ? '' : 'none';
        btnPrev.textContent = 'Назад';
    }

    if (currentFeedbackStepIndex < 2) {
        if (btnNext) {
            btnNext.style.display = '';
            btnNext.textContent = 'Далее';
        }
        if (btnSubmit) btnSubmit.style.display = 'none';
    } else {
        if (btnNext) btnNext.style.display = 'none';
        if (btnSubmit) {
            btnSubmit.style.display = '';
            if (completedCount < 3) {
                btnSubmit.style.opacity = '0.6';
                btnSubmit.title = 'Для отправки нужно протестировать все 3 маршрута';
            } else {
                btnSubmit.style.opacity = '1';
                btnSubmit.title = '';
            }
        }
    }
}

function goToFeedbackStep(stepIdx) {
    if (stepIdx < 0 || stepIdx >= FEEDBACK_STEPS.length) return;
    currentFeedbackStepIndex = stepIdx;
    renderFeedbackCurrentStep();
}

function prevFeedbackStep() {
    if (currentFeedbackStepIndex > 0) {
        currentFeedbackStepIndex--;
        renderFeedbackCurrentStep();
    }
}

function validateCurrentStep() {
    const step = FEEDBACK_STEPS[currentFeedbackStepIndex];
    const data = feedbackDraft[step.mode];
    const warn = document.getElementById('rf-status-error');

    if (!data.status) {
        if (warn) {
            warn.textContent = 'Выберите статус подключения';
            warn.style.display = '';
        }
        showToast('Пожалуйста, выбери статус подключения');
        return false;
    }
    const pingVal = parseInt(data.ping, 10);
    if (!pingVal || pingVal <= 0) {
        if (warn) {
            warn.textContent = 'Укажите пинг в игре (мс)';
            warn.style.display = '';
        }
        const input = document.getElementById('rf-step-ping');
        if (input) input.focus();
        showToast('Пожалуйста, укажи пинг в игре');
        return false;
    }
    if (warn) warn.style.display = 'none';
    return true;
}

function nextFeedbackStep() {
    if (!validateCurrentStep()) return;
    if (currentFeedbackStepIndex < FEEDBACK_STEPS.length - 1) {
        currentFeedbackStepIndex++;
        renderFeedbackCurrentStep();
    }
}


function onStepStatusSelect(status) {
    const step = FEEDBACK_STEPS[currentFeedbackStepIndex];
    feedbackDraft[step.mode].status = status;
    saveFeedbackDraftToStorage();
    renderFeedbackCurrentStep();
}

function onStepPingInput(val) {
    const step = FEEDBACK_STEPS[currentFeedbackStepIndex];
    feedbackDraft[step.mode].ping = val.replace(/\D/g, '');
    saveFeedbackDraftToStorage();
    let completedCount = 0;
    for (const s of FEEDBACK_STEPS) {
        const d = feedbackDraft[s.mode];
        if (d && d.status && parseInt(d.ping, 10) > 0) completedCount++;
    }
    const summaryEl = document.getElementById('rf-progress-summary');
    if (summaryEl) summaryEl.textContent = `Заполнено: ${completedCount} / 3`;
}

function onStepMatchChange(val) {
    const step = FEEDBACK_STEPS[currentFeedbackStepIndex];
    feedbackDraft[step.mode].match = val;
    saveFeedbackDraftToStorage();
}

function onStepDiscordChange(val) {
    const step = FEEDBACK_STEPS[currentFeedbackStepIndex];
    feedbackDraft[step.mode].discord = val;
    saveFeedbackDraftToStorage();
}

function onStepCommentInput(val) {
    const step = FEEDBACK_STEPS[currentFeedbackStepIndex];
    feedbackDraft[step.mode].comment = val;
    saveFeedbackDraftToStorage();
    const counterEl = document.getElementById('rf-step-counter');
    if (counterEl) counterEl.textContent = `${val.length} / 300`;
}

async function submitMultiRouteFeedback() {
    // Validate that all 3 routes are complete!
    for (let i = 0; i < FEEDBACK_STEPS.length; i++) {
        const s = FEEDBACK_STEPS[i];
        const d = feedbackDraft[s.mode];
        if (!d || !d.status || !parseInt(d.ping, 10)) {
            currentFeedbackStepIndex = i;
            renderFeedbackCurrentStep();
            validateCurrentStep();
            showToast(`Маршрут "${s.label}" еще не протестирован! Пожалуйста, заполни все 3 шага.`);
            return;
        }
    }

    const submitBtn = document.getElementById('btn-rf-submit');
    const spinner = document.getElementById('rf-btn-spinner');
    const btnText = document.getElementById('rf-btn-text');

    if (submitBtn) submitBtn.disabled = true;
    if (spinner) spinner.style.display = 'inline-block';
    if (btnText) btnText.style.display = 'none';

    try {
        const reviews = FEEDBACK_STEPS.map(s => {
            const d = feedbackDraft[s.mode];
            return {
                route_mode: s.mode,
                status: d.status,
                in_game_ping: parseInt(d.ping, 10) || 0,
                match_quality: d.match || 'perfect',
                discord_status: d.discord || 'clean',
                user_comment: d.comment || ''
            };
        });

        const res = await fetch('/api/routing-feedback', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                reviews: reviews,
                telemetry_data: {
                    tested_all_three: true,
                    timestamp: new Date().toISOString()
                }
            })
        });

        const data = await res.json();
        if (res.ok && data && data.success) {
            try { localStorage.removeItem('warlink_routing_feedback_draft_v2'); } catch(e) {}
            feedbackDraft = {
                'direct_moscow': { status: '', ping: '', match: 'perfect', discord: 'clean', comment: '' },
                'direct_frankfurt': { status: '', ping: '', match: 'perfect', discord: 'clean', comment: '' },
                'transit': { status: '', ping: '', match: 'perfect', discord: 'clean', comment: '' }
            };

            const formCont = document.getElementById('rf-form-container');
            const successCont = document.getElementById('rf-success-container');
            if (formCont) formCont.style.display = 'none';
            if (successCont) successCont.style.display = 'flex';
        } else {
            showToast('Ошибка отправки: ' + (data.error || 'попробуйте позже'));
        }
    } catch (e) {
        showToast('Не удалось отправить отчет: ' + e.message);
    } finally {
        if (submitBtn) submitBtn.disabled = false;
        if (btnText) btnText.style.display = 'inline';
        if (spinner) spinner.style.display = 'none';
    }
}

function openLogFile() {
    fetch('/api/open-log').catch(() => {});
}

function openSingboxLogFile() {
    fetch('/api/open-singbox-log').catch(() => {});
}

function openLogsFolder() {
    fetch('/api/open-logs-folder').catch(() => {});
}

async function runBenchmark() {
    const btn = document.getElementById('btn-run-benchmark');
    if (btn) btn.disabled = true;
    try {
        await fetch('/api/run-benchmark');
        fetchStatus();
    } catch (e) {
        console.error('Benchmark error:', e);
    } finally {
        setTimeout(() => {
            if (btn) btn.disabled = false;
        }, 1500);
    }
}

async function resetAutoHosts() {
    try {
        await fetch('/api/reset-auto-hosts', { method: 'POST' });
        fetchStatus();
    } catch (e) {
        console.error('Reset auto hosts error:', e);
    }
}

// --- Settings Subtabs & WARDOGS Tweaks ---
let wardogsTweakStatus = null;
let isTogglingWardogsShadows = false;

function switchSettingsTab(tabName, e) {
    if (e) e.stopPropagation();
    const btnGeneral = document.getElementById('tab-btn-settings-general');
    const btnTweaks = document.getElementById('tab-btn-settings-tweaks');
    const panelGeneral = document.getElementById('settings-panel-general');
    const panelTweaks = document.getElementById('settings-panel-tweaks');

    if (tabName === 'tweaks') {
        if (btnGeneral) btnGeneral.classList.remove('active');
        if (btnTweaks) btnTweaks.classList.add('active');
        if (panelGeneral) panelGeneral.style.display = 'none';
        if (panelTweaks) panelTweaks.style.display = 'block';
        loadWardogsTweaksStatus();
    } else {
        if (btnGeneral) btnGeneral.classList.add('active');
        if (btnTweaks) btnTweaks.classList.remove('active');
        if (panelGeneral) panelGeneral.style.display = 'block';
        if (panelTweaks) panelTweaks.style.display = 'none';
    }
}

async function loadWardogsTweaksStatus() {
    try {
        const res = await fetch('/api/tweaks/wardogs/status');
        if (!res.ok) return;
        const data = await res.json();
        if (data && data.success && data.status) {
            updateWardogsTweaksUI(data.status);
        }
    } catch (e) {
        console.error('Failed to load WARDOGS tweaks status:', e);
    }
}

function updateWardogsTweaksUI(status) {
    wardogsTweakStatus = status;

    const badge = document.getElementById('wardogs-shadows-badge');
    const metaVal = document.getElementById('wardogs-shadows-meta-val');
    const metaAttr = document.getElementById('wardogs-shadows-meta-attr');
    const switchEl = document.getElementById('wardogs-shadows-switch');
    const switchLabel = document.getElementById('wardogs-shadows-switch-label');
    const runningAlert = document.getElementById('wardogs-running-alert');
    const backupInfo = document.getElementById('wardogs-backup-info');

    if (!status.config_found) {
        if (badge) {
            badge.textContent = 'НЕ НАЙДЕН';
            badge.classList.remove('active');
        }
        if (metaVal) metaVal.textContent = 'Конфиг не найден';
        if (metaAttr) metaAttr.textContent = 'Атрибут: —';
        if (switchEl) switchEl.classList.remove('active');
        if (switchLabel) switchLabel.textContent = 'ВЫКЛ';
        if (runningAlert) runningAlert.style.display = 'none';
        if (backupInfo) backupInfo.style.display = 'none';
        return;
    }

    // Status Badge
    if (badge) {
        if (status.shadows_disabled) {
            badge.textContent = 'ТЕНИ ОТКЛЮЧЕНЫ';
            badge.classList.add('active');
        } else {
            badge.textContent = 'СТАНДАРТНЫЕ ТЕНИ';
            badge.classList.remove('active');
        }
    }

    // Meta labels
    if (metaVal) {
        metaVal.textContent = 'Параметр: sg.ShadowQuality=' + status.shadow_quality;
    }
    if (metaAttr) {
        metaAttr.textContent = 'Атрибут: ' + (status.is_read_only ? 'Только для чтения' : 'Обычный');
    }

    // Airplane-style switch
    if (switchEl) {
        switchEl.classList.toggle('active', !!status.shadows_disabled);
    }
    if (switchLabel) {
        switchLabel.textContent = status.shadows_disabled ? 'ВКЛ' : 'ВЫКЛ';
    }

    // Alert if game is running
    if (runningAlert) {
        runningAlert.style.display = status.game_running ? 'flex' : 'none';
    }

    // Backup note
    if (backupInfo) {
        backupInfo.style.display = status.has_backup ? 'block' : 'none';
    }
}

async function toggleWardogsShadows(e) {
    if (e) e.stopPropagation();
    if (isTogglingWardogsShadows) return;

    const switchEl = document.getElementById('wardogs-shadows-switch');
    const currentDisabled = wardogsTweakStatus ? wardogsTweakStatus.shadows_disabled : (switchEl && switchEl.classList.contains('active'));
    const targetDisable = !currentDisabled;

    isTogglingWardogsShadows = true;
    if (switchEl) {
        switchEl.style.pointerEvents = 'none';
        switchEl.style.opacity = '0.6';
    }

    try {
        const res = await fetch('/api/tweaks/wardogs/shadows', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ disable_shadows: targetDisable })
        });
        const data = await res.json();
        if (data && data.success && data.status) {
            updateWardogsTweaksUI(data.status);
            showToast(targetDisable ? 'Тени отключены' : 'Тени включены (восстановлены)');
        } else {
            showToast('Ошибка применения твика: ' + (data && data.error ? data.error : 'неизвестная ошибка'));
            await loadWardogsTweaksStatus();
        }
    } catch (err) {
        console.error('Failed to toggle WARDOGS shadows:', err);
        showToast('Сетевой сбой при применении твика');
    } finally {
        isTogglingWardogsShadows = false;
        if (switchEl) {
            switchEl.style.pointerEvents = '';
            switchEl.style.opacity = '';
        }
    }
}

async function openWardogsConfigFolder() {
    try {
        const res = await fetch('/api/tweaks/wardogs/open-folder', { method: 'POST' });
        const data = await res.json();
        if (!data || !data.success) {
            showToast('Не удалось открыть папку конфигурации');
        }
    } catch (e) {
        console.error('Failed to open config folder:', e);
        showToast('Ошибка при открытии папки');
    }
}

window.switchSettingsTab = switchSettingsTab;
window.toggleWardogsShadows = toggleWardogsShadows;
window.openWardogsConfigFolder = openWardogsConfigFolder;
window.loadWardogsTweaksStatus = loadWardogsTweaksStatus;


// --- In-App Notifications Center ---
let cachedNotifications = [];
let seenNotifIds = new Set();
let isFirstNotifFetch = true;

function onToggleSoundEffects(enabled) {
    if (window.WarLinkAudio && typeof window.WarLinkAudio.setEnabled === 'function') {
        window.WarLinkAudio.setEnabled(enabled);
    }
}

function toggleNotifications(e) {
    if (e) e.stopPropagation();
    switchView('view-notifications');
}


async function fetchNotifications() {
    if (typeof UIStore !== 'undefined' && typeof UIStore.requestSWR === 'function') {
        await UIStore.requestSWR(
            '/api/notifications',
            async () => {
                const resp = await fetch('/api/notifications');
                if (!resp.ok) return null;
                return await resp.json();
            },
            (data, isFresh) => {
                if (!data) return;
                const incoming = data.notifications || [];
                if (isFirstNotifFetch) {
                    incoming.forEach(n => seenNotifIds.add(n.id));
                    isFirstNotifFetch = false;
                } else if (isFresh) {
                    let maxSeverity = null;
                    let newestUnread = null;
                    for (const n of incoming) {
                        if (!seenNotifIds.has(n.id) && !n.is_read) {
                            seenNotifIds.add(n.id);
                            if (!newestUnread) newestUnread = n;
                            if (n.severity === 'urgent' || (n.severity === 'warning' && maxSeverity !== 'urgent')) {
                                maxSeverity = n.severity;
                            } else if (!maxSeverity) {
                                maxSeverity = n.severity || 'info';
                            }
                        }
                    }
                    if (newestUnread) {
                        showToast(`${newestUnread.title}: ${newestUnread.message}`);
                        if (window.WarLinkAudio && typeof window.WarLinkAudio.playNotification === 'function') {
                            window.WarLinkAudio.playNotification(maxSeverity || newestUnread.severity);
                        }
                    }
                }
                cachedNotifications = incoming;
                updateNotificationsUI(cachedNotifications, data.unread_count || 0);
            },
            () => {
                const listEl = document.getElementById('notifications-list');
                if (listEl && listEl.children.length === 0) {
                    listEl.innerHTML = getNotificationsSkeletonHtml(2);
                }
            }
        );
        return;
    }

    const listEl = document.getElementById('notifications-list');
    if (cachedNotifications && cachedNotifications.length > 0) {
        updateNotificationsUI(cachedNotifications);
    } else if (listEl && (listEl.children.length === 0 || !listEl.querySelector('.notification-card'))) {
        listEl.innerHTML = getNotificationsSkeletonHtml(2);
    }

    try {
        const resp = await fetch('/api/notifications');
        if (!resp.ok) return;
        const data = await resp.json();
        const incoming = data.notifications || [];
        if (isFirstNotifFetch) {
            incoming.forEach(n => seenNotifIds.add(n.id));
            isFirstNotifFetch = false;
        }
        cachedNotifications = incoming;
        updateNotificationsUI(cachedNotifications, data.unread_count || 0);
    } catch (e) {
        console.error('Fetch notifications error:', e);
    }
}

function formatNotificationTime(isoStr) {
    if (!isoStr) return '';
    try {
        const date = new Date(isoStr);
        if (isNaN(date.getTime())) return isoStr;

        const now = new Date();
        const diffMs = now - date;
        const diffSec = Math.floor(diffMs / 1000);

        const hours = String(date.getHours()).padStart(2, '0');
        const mins = String(date.getMinutes()).padStart(2, '0');
        const timeStr = `${hours}:${mins}`;

        const isToday = date.toDateString() === now.toDateString();
        if (isToday) {
            if (diffSec >= 0 && diffSec < 60) {
                return 'Только что';
            }
            return `Сегодня, ${timeStr}`;
        }

        const yesterday = new Date(now);
        yesterday.setDate(yesterday.getDate() - 1);
        if (date.toDateString() === yesterday.toDateString()) {
            return `Вчера, ${timeStr}`;
        }

        const day = String(date.getDate()).padStart(2, '0');
        const month = String(date.getMonth() + 1).padStart(2, '0');
        const year = date.getFullYear();
        return `${day}.${month}.${year}, ${timeStr}`;
    } catch (e) {
        return isoStr;
    }
}

function formatNotificationTooltip(isoStr) {
    if (!isoStr) return '';
    try {
        const date = new Date(isoStr);
        if (isNaN(date.getTime())) return isoStr;
        return date.toLocaleString('ru-RU');
    } catch (e) {
        return isoStr;
    }
}

function updateNotificationsUI(notifs, unreadCount) {
    const list = Array.isArray(notifs) ? notifs : [];
    const count = (typeof unreadCount === 'number') ? unreadCount : list.filter(x => !x.is_read).length;

    const badge = document.getElementById('notif-badge');
    if (badge) {
        if (count > 0) {
            badge.textContent = count > 9 ? '9+' : count;
            badge.style.display = 'block';
        } else {
            badge.style.display = 'none';
        }
    }
    const countTag = document.getElementById('notif-count-tag');
    if (countTag) {
        countTag.textContent = `${list.length}`;
    }

    const listEl = document.getElementById('notifications-list');
    if (!listEl) return;

    if (list.length === 0) {
        listEl.innerHTML = `
            <div style="text-align: center; color: var(--text-muted); padding: 40px 10px; font-size: 12px;">
                Нет новых уведомлений
            </div>
        `;
        return;
    }

    // Keyed reconciliation: preserve existing DOM elements if present
    const existingCards = new Map();
    Array.from(listEl.querySelectorAll('.notification-card[data-notif-id]')).forEach(el => {
        existingCards.set(el.dataset.notifId, el);
    });

    if (existingCards.size === 0 && (listEl.querySelector('.skeleton-card') || listEl.querySelector('div:not(.notification-card)'))) {
        listEl.innerHTML = '';
    }

    const currentIds = new Set();

    list.forEach(n => {
        const nId = String(n.id);
        currentIds.add(nId);
        let pillClass = 'pill-info';
        let pillText = 'Инфо';
        if (n.severity === 'update') { pillClass = 'pill-update'; pillText = 'Обновление'; }
        else if (n.severity === 'warning') { pillClass = 'pill-warning'; pillText = 'Важно'; }
        else if (n.severity === 'urgent') { pillClass = 'pill-urgent'; pillText = 'Срочно'; }

        const actionBtnHtml = n.action_label && n.action_url ? `
            <button class="notif-action-btn" onclick="openNotifAction('${encodeURIComponent(n.action_url)}')">${escapeHtml(n.action_label)}</button>
        ` : '';

        const existing = existingCards.get(nId);
        if (existing) {
            existing.classList.toggle('notif-unread', !n.is_read);
            const timeEl = existing.querySelector('.notif-time');
            if (timeEl) {
                timeEl.textContent = formatNotificationTime(n.created_at);
                timeEl.title = formatNotificationTooltip(n.created_at);
            }
            listEl.appendChild(existing);
        } else {
            const card = document.createElement('div');
            card.className = `notification-card ${n.is_read ? '' : 'notif-unread'}`;
            card.dataset.notifId = nId;
            card.onclick = () => markNotifRead(n.id);
            card.innerHTML = `
                <div class="notif-header">
                    <div class="notif-title-row">
                        <span class="notif-pill ${pillClass}">${pillText}</span>
                        <span class="notif-title">${escapeHtml(n.title)}</span>
                    </div>
                    <span class="notif-time" title="${escapeHtml(formatNotificationTooltip(n.created_at))}">${escapeHtml(formatNotificationTime(n.created_at))}</span>
                </div>
                <div class="notif-msg">${escapeHtml(n.message)}</div>
                ${actionBtnHtml}
            `;
            listEl.appendChild(card);
        }
    });

    existingCards.forEach((el, id) => {
        if (!currentIds.has(id) && el.parentNode === listEl) {
            listEl.removeChild(el);
        }
    });
}

async function markNotifRead(id) {
    try {
        await fetch('/api/notifications/read', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ notification_id: id })
        });
        if (typeof UIStore !== 'undefined') {
            UIStore.invalidate('/api/notifications');
        }
        if (Array.isArray(cachedNotifications)) {
            const n = cachedNotifications.find(x => x.id === id);
            if (n) n.is_read = true;
            const unread = cachedNotifications.filter(x => !x.is_read).length;
            updateNotificationsUI(cachedNotifications, unread);
        }
    } catch (e) {}
}

async function markAllNotificationsRead() {
    if (!Array.isArray(cachedNotifications)) return;
    for (const n of cachedNotifications) {
        if (!n.is_read) {
            markNotifRead(n.id);
        }
    }
}

async function openNotifAction(encodedURL) {
    const rawURL = decodeURIComponent(encodedURL);
    if (rawURL === '#action-routing-feedback' || rawURL === '#routing-feedback' || rawURL.includes('routing-feedback')) {
        if (typeof openRoutingFeedbackModal === 'function') {
            openRoutingFeedbackModal();
        }
        return;
    }
    if (rawURL === '#view-progression' || rawURL === 'view-progression' || rawURL === '#progression' || rawURL === 'progression' || rawURL.includes('view-progression') || rawURL.includes('#progression')) {
        if (typeof toggleProgression === 'function') {
            toggleProgression();
        } else if (typeof switchView === 'function') {
            switchView('view-progression');
        }
        return;
    }
    if (rawURL.startsWith('#view-')) {
        const viewId = rawURL.slice(1);
        if (typeof switchView === 'function') {
            switchView(viewId);
        }
        return;
    }
    if (rawURL.startsWith('http://') || rawURL.startsWith('https://')) {
        try {
            await fetch('/api/open-external-url', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ url: rawURL })
            });
        } catch(e) {
            window.open(rawURL, '_blank');
        }
    }
}


async function checkAnnouncements() {
    try {
        const res = await fetch('/api/announcements');
        if (!res.ok) return;
        const data = await res.json();
        if (data && data.success && data.announcement && data.announcement.active) {
            const ann = data.announcement;
            if (ann.type === 'toast' && ann.message) {
                showToast(ann.message);
            }
        }
    } catch(e) {}
}

// Global external link interceptor - guarantees that all links open via external browser/Steam and never inside WebView2
document.addEventListener('click', (e) => {
    const link = e.target.closest('a');
    if (!link) return;
    const href = link.getAttribute('href');
    if (!href || href.startsWith('#') || href.startsWith('javascript:')) return;
    if (href.startsWith('http://') || href.startsWith('https://') || href.startsWith('steam://')) {
        e.preventDefault();
        e.stopPropagation();
        if (typeof window.openSteamLink === 'function' && href.includes('steamcommunity.com')) {
            window.openSteamLink(href);
        } else if (typeof window.openExternal === 'function') {
            window.openExternal(href);
        } else {
            fetch('/api/open-url?url=' + encodeURIComponent(href));
        }
    }
}, true);

// Real-time SSE Hub Connection (0ms Push Updates for Tickets, Notifications, and Status)
function initLiveEventStream() {
    if (typeof EventSource === 'undefined') return;

    let eventSource = null;
    let reconnectTimer = null;

    function connect() {
        if (eventSource) {
            try { eventSource.close(); } catch(e) {}
            eventSource = null;
        }

        try {
            eventSource = new EventSource('/api/events');

            eventSource.addEventListener('ticket_message', (e) => {
                try {
                    const msg = JSON.parse(e.data);
                    if (window.onLiveTicketMessage) {
                        window.onLiveTicketMessage(msg);
                    }
                } catch(err) {}
            });

            eventSource.addEventListener('ticket_updated', (e) => {
                try {
                    const tId = e.data;
                    if (window.onLiveTicketUpdated) {
                        window.onLiveTicketUpdated(tId);
                    }
                } catch(err) {}
            });

            eventSource.addEventListener('notification', (e) => {
                try {
                    const notif = JSON.parse(e.data);
                    if (notif && notif.id) {
                        if (!seenNotifIds.has(notif.id)) {
                            seenNotifIds.add(notif.id);
                            showToast(notif.title + ': ' + notif.message);
                            if (window.WarLinkAudio && typeof window.WarLinkAudio.playNotification === 'function') {
                                window.WarLinkAudio.playNotification(notif.severity);
                            }
                            const badge = document.getElementById('notif-badge');
                            if (badge) {
                                const cur = parseInt(badge.textContent || '0', 10) || 0;
                                badge.textContent = cur + 1;
                                badge.style.display = 'block';
                            }
                        }
                    }
                } catch(err) {}
            });

            eventSource.addEventListener('slots', (e) => {
                try {
                    const s = JSON.parse(e.data);
                    if (s && s.gateway_slots) {
                        const gwSlotsEl = document.getElementById('gw-slots');
                        if (gwSlotsEl) gwSlotsEl.textContent = s.gateway_slots;
                    }
                } catch(err) {}
            });

            eventSource.addEventListener('flags:updated', () => {
                if (window.WarLinkFlags && typeof window.WarLinkFlags.fetch === 'function') {
                    window.WarLinkFlags.fetch();
                }
            });

            eventSource.onerror = () => {
                try { eventSource.close(); } catch(e) {}
                eventSource = null;
                if (!reconnectTimer) {
                    reconnectTimer = setTimeout(() => {
                        reconnectTimer = null;
                        connect();
                    }, 5000);
                }
            };
        } catch(err) {
            console.warn('Failed to start SSE stream:', err);
        }
    }

    connect();
}

// Initial boot
document.addEventListener('DOMContentLoaded', async () => {
    // 1. Immediately hydrate last known non-network status before reveal
    const cachedStatus = loadCachedStatus();
    if (cachedStatus) {
        const nonNetworkStatus = { ...cachedStatus };
        delete nonNetworkStatus.ping_label;
        delete nonNetworkStatus.gateway_ping;
        delete nonNetworkStatus.total_ping;
        delete nonNetworkStatus.ping_ms;
        delete nonNetworkStatus.gateway_slots;
        delete nonNetworkStatus.gateway_days;
        updateUI(nonNetworkStatus);
    }

    // 2. Render default state immediately before revealing window to eliminate layout shifts
    renderShowcase(cachedGames, selectedGameId);
    applyInitialRoute();

    // 3. Hydrate sub-views from persistent cache before window is revealed to ensure Zero-CLS
    if (typeof window.initAccountCache === 'function') {
        window.initAccountCache();
    }
    if (window.ProgressionController && typeof window.ProgressionController.init === 'function') {
        window.ProgressionController.init();
    }
    if (typeof window.initSupportCache === 'function') {
        window.initSupportCache();
    }

    // 4. Reveal window IMMEDIATELY (instant display, zero startup latency)
    if (typeof window.revealWindow === 'function') {
        window.revealWindow();
    }

    // 5. Query initial status, notifications, feature flags and start live event stream
    fetchStatus();
    fetchNotifications();
    checkAnnouncements();
    if (window.WarLinkFlags && typeof window.WarLinkFlags.fetch === 'function') {
        window.WarLinkFlags.fetch();
    }
    initLiveEventStream();

    // 6. Stagger background prefetch after window is visible to prevent thread contention
    setTimeout(() => {
        if (typeof UIStore !== 'undefined' && typeof UIStore.prefetchAll === 'function') {
            UIStore.prefetchAll();
        }
    }, 2500);

    setInterval(fetchStatus, 1500);
    setInterval(fetchNotifications, 10000);
});
