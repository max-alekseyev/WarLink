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
    }
];
let cachedGames = defaultGames;
let launchingGameId = null;
let lastShowcaseStateKey = '';
let isVotingEnabled = true;
let isDonateEnabled = true;

const GAME_ICON_FALLBACK_SVG = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
    <line x1="6" x2="10" y1="11" y2="11" />
    <line x1="8" x2="8" y1="9" y2="13" />
    <line x1="15" x2="15.01" y1="12" y2="12" />
    <line x1="18" x2="18.01" y1="10" y2="10" />
    <path d="M17.32 5H6.68a4 4 0 0 0-3.978 3.59c-.006.052-.01.101-.017.152C2.604 9.416 2 14.456 2 16a3 3 0 0 0 3 3c1 0 1.5-.5 2-1l1.414-1.414A2 2 0 0 1 9.828 16h4.344a2 2 0 0 1 1.414.586L17 18c.5.5 1 1 2 1a3 3 0 0 0 3-3c0-1.545-.604-6.584-.685-7.258-.007-.05-.011-.1-.017-.151A4 4 0 0 0 17.32 5z" />
</svg>`;

// --- View Partials Loader ---
async function loadPartials() {
    const mounts = document.querySelectorAll('[data-partial]');
    await Promise.all(Array.from(mounts).map(async (el) => {
        const url = el.getAttribute('data-partial');
        try {
            const resp = await fetch(url);
            if (resp.ok) {
                const html = await resp.text();
                el.outerHTML = html;
            }
        } catch (e) {
            console.error('Failed loading partial:', url, e);
        }
    }));
}

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
    const allViews = ['view-details', 'view-notifications', 'view-sponsors', 'view-account'];
    const showcase = document.getElementById('view-showcase');
    const targetEl = targetViewId ? document.getElementById(targetViewId) : null;
    const isAlreadyOpen = targetEl && targetEl.style.display === 'flex';

    // Hide all sub-views atomically
    for (const vId of allViews) {
        const el = document.getElementById(vId);
        if (el) el.style.display = 'none';
    }

    const nextActiveId = (!targetViewId || isAlreadyOpen) ? null : targetViewId;

    if (!nextActiveId) {
        // Return to showcase
        if (showcase) showcase.style.display = 'flex';
    } else {
        if (showcase) showcase.style.display = 'none';
        if (targetEl) targetEl.style.display = 'flex';

        // Trigger cached/background data refreshes without wiping DOM
        if (nextActiveId === 'view-sponsors') fetchSponsors();
        if (nextActiveId === 'view-account') fetchAccountProfile();
        if (nextActiveId === 'view-notifications') fetchNotifications();
    }

    updateTitlebarActiveState(nextActiveId);
}

function updateTitlebarActiveState(activeViewId) {
    const btnMap = {
        'view-account': 'btn-account-toggle',
        'view-sponsors': 'btn-sponsors-toggle',
        'view-notifications': 'btn-notif-toggle',
        'view-details': 'btn-settings-toggle'
    };
    for (const [vId, btnId] of Object.entries(btnMap)) {
        const btn = document.getElementById(btnId);
        if (btn) {
            btn.classList.toggle('is-active', vId === activeViewId);
        }
    }
}

function toggleDetails(e) {
    if (e) e.stopPropagation();
    switchView('view-details');
}

function closeDetails() {
    switchView(null);
}

// --- Free Internet Toggle (Titlebar) ---
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
            freeInternetEnabled = !newTarget;
            updateFreeInternetUI(freeInternetEnabled);
        }
    } catch (err) {
        console.error('Free internet toggle error:', err);
        freeInternetEnabled = !newTarget;
        updateFreeInternetUI(freeInternetEnabled);
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

    // Sort: last played first
    const sorted = [...cachedGames].sort((a, b) => (b.last_played || 0) - (a.last_played || 0));

    // Avoid destroying and recreating DOM on every poll if state hasn't changed (prevents hover flickering)
    const stateKey = JSON.stringify(sorted) + '_' + activeId + '_' + isConnected + '_' + isBusy + '_' + launchingGameId + '_' + isVotingEnabled;
    if (stateKey === lastShowcaseStateKey && grid.children.length > 0) {
        return;
    }
    lastShowcaseStateKey = stateKey;

    let html = '';

    sorted.forEach(g => {
        const isSelected = (g.id === activeId);
        const isDefault = !!g.is_default;
        const isLaunching = (launchingGameId === g.id) || (isBusy && !isConnected && isSelected);

        let iconHtml = '';
        const iconSrc = g.icon_url || (g.id === 'wardogs' ? 'wardogs_icon.png' : '');
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
            <div class="game-shortcut add-shortcut" onclick="openAddGameModal()" title="Добавить игру">
                <div class="add-shortcut-btn">
                    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M5 12h14" />
                        <path d="M12 5v14" />
                    </svg>
                </div>
                <div class="shortcut-title">Добавить</div>
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

// Backward-compatibility aliases for static HTML markup
function onGameCardClick(gameId) {
    onGameClick(gameId);
}

function handleGameContextMenu(e, gameId) {
    showGameContextMenu(e, gameId, gameId === 'wardogs');
}

// 1-Click Game Action (One action, one screen)
function onGameClick(gameId) {
    if (isBusy || isDownloadingDeps || isInitializing) return;

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
            gwDot.title = 'Шлюз Стокгольм онлайн';
        } else {
            gwDot.classList.add('is-offline');
            gwDot.title = 'Шлюз недоступен';
        }
    }

    // Update Stockholm Gateway footer metrics
    const gwPingEl = document.getElementById('gw-ping');
    const gwSlotsEl = document.getElementById('gw-slots');
    const gwDaysEl = document.getElementById('gw-days');
    const btnDonateText = document.getElementById('btn-donate-text');
    if (gwPingEl) {
        if (data.ping_label) {
            gwPingEl.textContent = data.ping_label;
        } else if (data.gateway_ping && data.gateway_ping > 1) {
            gwPingEl.textContent = data.gateway_ping + ' мс';
        } else {
            gwPingEl.textContent = '— мс';
        }
        if (data.gateway_ping && data.gateway_ping > 1) {
            gwPingEl.title = `Пинг ПК -> Шлюз Стокгольм: ${data.gateway_ping} мс`;
        }
    }
    if (gwSlotsEl) {
        const slots = data.gateway_slots || '—';
        gwSlotsEl.textContent = slots;
        const isFull = slots.includes('50/50') || slots.includes('60/60') || (data.gateway_full === true);
        gwSlotsEl.classList.toggle('slots-full', isFull);
        gwSlotsEl.title = data.gateway_slots_tooltip || 'Активные слоты шлюза Стокгольм';
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
    if (gwDaysEl) {
        if (data.gateway_days !== undefined && data.gateway_days > 0) {
            gwDaysEl.textContent = data.gateway_days + ' дн.';
        } else {
            gwDaysEl.textContent = '— дн.';
        }
    }
    const gwTitleEl = document.querySelector('.gateway-title');
    if (gwTitleEl && data.gateway_location) {
        gwTitleEl.textContent = data.gateway_location.split(',')[0].trim();
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
        if (btnDonate) {
            btnDonate.style.display = isDonateEnabled ? 'inline-flex' : 'none';
        }
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
}

async function toggleConnect() {
    lastShownError = '';
    if (isBusy || isDownloadingDeps || isInitializing) return;

    if (isConnected) {
        try {
            await fetch('/api/disconnect');
        } catch (e) {}
    } else {
        launchingGameId = selectedGameId;
        renderShowcase(cachedGames, selectedGameId);
        try {
            await fetch('/api/connect');
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

function openLogFile() {
    fetch('/api/open-log').catch(() => {});
}

function openSingboxLogFile() {
    fetch('/api/open-singbox-log').catch(() => {});
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

async function donateServer(e) {
    if (e) e.stopPropagation();
    openDonateModal(e);
}

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

function closeNotifications() {
    switchView(null);
}

async function fetchNotifications() {
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
        } else {
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

    listEl.innerHTML = list.map(n => {
        let pillClass = 'pill-info';
        let pillText = 'Инфо';
        if (n.severity === 'update') { pillClass = 'pill-update'; pillText = 'Обновление'; }
        else if (n.severity === 'warning') { pillClass = 'pill-warning'; pillText = 'Важно'; }
        else if (n.severity === 'urgent') { pillClass = 'pill-urgent'; pillText = 'Срочно'; }

        const unreadClass = n.is_read ? '' : 'notif-unread';
        const actionBtn = n.action_label && n.action_url ? `
            <button class="notif-action-btn" onclick="openNotifAction('${encodeURIComponent(n.action_url)}')">${escapeHtml(n.action_label)}</button>
        ` : '';

        return `
            <div class="notification-card ${unreadClass}" onclick="markNotifRead(${n.id})">
                <div class="notif-header">
                    <div class="notif-title-row">
                        <span class="notif-pill ${pillClass}">${pillText}</span>
                        <span class="notif-title">${escapeHtml(n.title)}</span>
                    </div>
                    <span class="notif-time" title="${escapeHtml(formatNotificationTooltip(n.created_at))}">${escapeHtml(formatNotificationTime(n.created_at))}</span>
                </div>
                <div class="notif-msg">${escapeHtml(n.message)}</div>
                ${actionBtn}
            </div>
        `;
    }).join('');
}

async function markNotifRead(id) {
    try {
        await fetch('/api/notifications/read', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ notification_id: id })
        });
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


// Initial boot
document.addEventListener('DOMContentLoaded', async () => {
    // Render default state immediately before revealing window to eliminate layout shifts
    renderShowcase(cachedGames, selectedGameId);
    await loadPartials();
    if (typeof window.revealWindow === 'function') {
        window.revealWindow();
    }
    fetchStatus();
    fetchNotifications();
    if (typeof fetchAccountProfile === 'function') {
        fetchAccountProfile();
    }
    setInterval(fetchStatus, 1500);
    setInterval(fetchNotifications, 10000);
});
