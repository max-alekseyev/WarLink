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

function toggleDetails(e) {
    if (e) e.stopPropagation();
    const detailsView = document.getElementById('view-details');
    if (detailsView) {
        const isHidden = detailsView.style.display === 'none' || detailsView.style.display === '';
        detailsView.style.display = isHidden ? 'flex' : 'none';
    }
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
                <svg class="shortcut-spinner-icon" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M21 12a9 9 0 1 1-6.219-8.56" />
                </svg>
            </div>
        ` : '';

        let deleteBtn = '';
        if (!isDefault) {
            deleteBtn = `
                <button class="shortcut-delete-btn" onclick="deleteGame(event, '${g.id}')" title="Удалить ярлык">
                    <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>
                </button>
            `;
        }

        html += `
            <div class="game-shortcut ${isSelected ? 'is-selected' : ''} ${isLaunching ? 'is-launching' : ''}" 
                 onclick="onGameClick('${g.id}')" 
                 oncontextmenu="showGameContextMenu(event, '${g.id}', ${isDefault})"
                 title="${escapeHtml(g.title)}">
                <div class="shortcut-icon-wrapper">
                    ${iconHtml}
                    ${activeDot}
                    ${spinnerOverlay}
                </div>
                <div class="shortcut-title">${escapeHtml(g.title)}</div>
                ${deleteBtn}
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
    if (!str) return '';
    return String(str)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;');
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
        btnConnect.querySelector('span').textContent = isGameActive ? 'Отключиться' : 'Подключиться';
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
function onModalBackdropMouseDown(e) {
    if (e.target === e.currentTarget || e.target.id === 'modal-add-game') {
        closeAddGameModal();
    }
}

function onModalBackdropClick(e) {
    if (e.target === e.currentTarget || e.target.id === 'modal-add-game') {
        closeAddGameModal();
    }
}

// Global Keyboard Navigation (Escape to dismiss modals, sheets, and menus)
document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
        const modal = document.getElementById('modal-add-game');
        if (modal && modal.style.display !== 'none') {
            closeAddGameModal();
            return;
        }
        const details = document.getElementById('view-details');
        if (details && details.style.display !== 'none' && details.style.display !== '') {
            toggleDetails();
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

// --- Community Game Voting Modal ---
let selectedSteamGame = null;
let voteSearchTimer = null;
let voteAutoPollTimer = null;

function openAddGameModal() {
    if (!isVotingEnabled) return;
    const modal = document.getElementById('modal-add-game');
    if (modal) {
        modal.style.display = 'flex';
        const input = document.getElementById('vote-search-input');
        if (input) {
            input.value = '';
            input.focus();
        }
        hideVoteAutocomplete();
        hideVotePreview();
        loadCommunityVotes();
        if (voteAutoPollTimer) clearInterval(voteAutoPollTimer);
        voteAutoPollTimer = setInterval(loadCommunityVotes, 5000);
    }
}

function closeAddGameModal() {
    const modal = document.getElementById('modal-add-game');
    if (modal) modal.style.display = 'none';
    hideVoteAutocomplete();
    hideVotePreview();
    if (voteAutoPollTimer) {
        clearInterval(voteAutoPollTimer);
        voteAutoPollTimer = null;
    }
}

function hideVoteAutocomplete() {
    const box = document.getElementById('vote-autocomplete');
    if (box) box.style.display = 'none';
}

function hideVotePreview() {
    selectedSteamGame = null;
    const box = document.getElementById('vote-selected-preview');
    if (box) box.style.display = 'none';
}

function onVoteSearchInput(query) {
    query = (query || '').trim();
    hideVoteAutocomplete();
    hideVotePreview();
    if (voteSearchTimer) clearTimeout(voteSearchTimer);
    if (!query || query.length < 2) return;

    voteSearchTimer = setTimeout(async () => {
        try {
            const res = await fetch(`/api/search-steam?term=${encodeURIComponent(query)}`);
            const data = await res.json();
            const items = (data && data.items) ? data.items : [];
            renderVoteAutocomplete(items);
        } catch (e) {}
    }, 250);
}

function renderVoteAutocomplete(items) {
    const box = document.getElementById('vote-autocomplete');
    if (!box) return;
    if (!items || items.length === 0) {
        box.style.display = 'none';
        return;
    }
    box.innerHTML = '';

    // Exclude already supported games (WARDOGS)
    const validItems = items.filter(it => it && parseInt(it.id) !== 1867240 && (!it.name || !it.name.toLowerCase().includes('wardogs')));

    if (validItems.length === 0) {
        box.style.display = 'none';
        return;
    }

    validItems.slice(0, 6).forEach(item => {
        const div = document.createElement('div');
        div.className = 'vote-autocomplete-item';
        div.dataset.appId = item.id;
        const iconSrc = item.tiny_image || item.icon_url || item.icon || '';
        div.innerHTML = `
            <div class="vote-item-icon-box">
                ${iconSrc ? `<img class="vote-item-icon" src="${escapeHtml(iconSrc)}" alt="" onerror="this.style.display='none'; if (this.nextElementSibling) this.nextElementSibling.style.display='flex';">` : ''}
                <div class="vote-fallback-icon" style="${iconSrc ? 'display:none;' : 'display:flex;'}">
                    ${GAME_ICON_FALLBACK_SVG}
                </div>
            </div>
            <span class="vote-item-title">${escapeHtml(item.name || 'Игра')}</span>
        `;
        div.addEventListener('click', () => selectSteamGameForVote(item));
        box.appendChild(div);
    });
    box.style.display = 'block';
}

function selectSteamGameForVote(item) {
    hideVoteAutocomplete();
    const iconSrc = item.tiny_image || item.icon_url || item.icon || '';
    selectedSteamGame = { ...item, icon_url: iconSrc };
    const input = document.getElementById('vote-search-input');
    if (input) input.value = item.name;

    const preview = document.getElementById('vote-selected-preview');
    const img = document.getElementById('vote-selected-img');
    const title = document.getElementById('vote-selected-title');
    if (preview && img && title) {
        img.src = iconSrc;
        title.textContent = item.name;
        preview.style.display = 'flex';
    }
}

async function submitProposedGame() {
    if (!selectedSteamGame || !selectedSteamGame.id) return;
    const iconSrc = selectedSteamGame.icon_url || selectedSteamGame.tiny_image || selectedSteamGame.icon || '';

    try {
        const res = await fetch('/api/votes', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                steam_app_id: parseInt(selectedSteamGame.id),
                title: selectedSteamGame.name,
                icon_url: iconSrc
            })
        });
        const data = await res.json();
        if (!res.ok || !data.success) {
            showToast(data.error || 'Ошибка при отправке голоса');
            return;
        }
        hideVotePreview();
        const input = document.getElementById('vote-search-input');
        if (input) input.value = '';
        await loadCommunityVotes();
    } catch (e) {
        showToast('Не удалось связаться с сервером голосования');
    }
}

async function voteForGame(appId, title, iconUrl) {
    try {
        const res = await fetch('/api/votes', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                steam_app_id: appId,
                title: title,
                icon_url: iconUrl
            })
        });
        const data = await res.json();
        if (!res.ok || !data.success) {
            showToast(data.error || 'Ошибка при голосовании');
            return;
        }
        await loadCommunityVotes();
    } catch (e) {
        showToast('Ошибка связи с сервером');
    }
}

async function retractGameVote(appId) {
    try {
        const res = await fetch('/api/votes', {
            method: 'DELETE',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ steam_app_id: appId })
        });
        await loadCommunityVotes();
    } catch (e) {}
}

async function loadCommunityVotes() {
    const listEl = document.getElementById('vote-games-list');
    const badgeEl = document.getElementById('user-votes-badge');
    if (!listEl) return;

    try {
        const res = await fetch('/api/votes');
        if (!res.ok) {
            listEl.innerHTML = '<div style="color: var(--text-muted); font-size: 11px; padding: 10px 0; text-align: center;">Голосование временно недоступно</div>';
            return;
        }
        const data = await res.json();
        if (badgeEl) {
            badgeEl.textContent = `Голоса: ${data.user_votes_used || 0} из ${data.max_user_votes || 3}`;
        }

        const games = data.games || [];
        if (games.length === 0) {
            listEl.innerHTML = '<div style="color: var(--text-muted); font-size: 11px; padding: 14px 0; text-align: center;">Пока нет предложенных игр. Найдите игру выше и будьте первым!</div>';
            return;
        }

        listEl.innerHTML = '';
        games.forEach(g => {
            const card = document.createElement('div');
            card.className = 'vote-game-card';

            const votesCount = g.votes_count || 0;
            const targetVotes = data.target_votes || 50;
            const pct = Math.min(100, Math.round((votesCount / targetVotes) * 100));
            const isWinner = g.status === 'queue_integration' || votesCount >= targetVotes;

            let actionEl;
            if (isWinner) {
                actionEl = document.createElement('span');
                actionEl.className = 'badge-winner';
                actionEl.textContent = 'В очереди на интеграцию';
            } else if (g.user_voted) {
                actionEl = document.createElement('button');
                actionEl.className = 'btn-vote active';
                actionEl.title = 'Нажмите, чтобы отозвать голос';
                actionEl.textContent = 'Отдано';
                actionEl.dataset.appId = g.steam_app_id;
                actionEl.addEventListener('click', () => retractGameVote(g.steam_app_id));
            } else {
                actionEl = document.createElement('button');
                actionEl.className = 'btn-vote';
                actionEl.textContent = 'Голосовать';
                actionEl.dataset.appId = g.steam_app_id;
                actionEl.dataset.title = g.title || '';
                actionEl.dataset.iconUrl = g.icon_url || '';
                actionEl.addEventListener('click', () => voteForGame(g.steam_app_id, g.title, g.icon_url));
            }

            const iconSrc = g.icon_url || '';

            card.innerHTML = `
                <div class="vote-card-main">
                    <div class="vote-card-icon-box">
                        ${iconSrc ? `<img class="vote-card-icon" src="${escapeHtml(iconSrc)}" alt="" onerror="this.style.display='none'; if (this.nextElementSibling) this.nextElementSibling.style.display='flex';">` : ''}
                        <div class="vote-fallback-icon" style="${iconSrc ? 'display:none;' : 'display:flex;'}">
                            ${GAME_ICON_FALLBACK_SVG}
                        </div>
                    </div>
                    <div class="vote-card-info">
                        <div class="vote-card-topline">
                            <span class="vote-card-name" title="${escapeHtml(g.title)}">${escapeHtml(g.title)}</span>
                            <span class="vote-card-stats">${votesCount} / ${targetVotes}</span>
                        </div>
                        <div class="vote-progress-track">
                            <div class="vote-progress-fill ${isWinner ? 'winner' : ''}" style="width: ${pct}%;"></div>
                        </div>
                    </div>
                </div>
            `;
            if (actionEl) {
                card.appendChild(actionEl);
            }
            listEl.appendChild(card);
        });
    } catch (e) {
        listEl.innerHTML = '<div style="color: var(--text-muted); font-size: 11px; padding: 10px 0; text-align: center;">Не удалось загрузить список</div>';
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

    // Handle error notifications (e.g. 100/100 slots full or network failure)
    if (data.last_error) {
        if (data.last_error !== lastShownError) {
            lastShownError = data.last_error;
            showToast(data.last_error);
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
        const isFull = slots.startsWith('100/') || (data.gateway_full === true);
        gwSlotsEl.classList.toggle('slots-full', isFull);
        if (isFull) {
            gwSlotsEl.title = 'Все слоты шлюза заняты (100/100). Новые подключения временно недоступны.';
        } else {
            gwSlotsEl.title = 'Активные слоты шлюза Стокгольм';
        }
    }
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
        const amt = data.donate_amount_rub;
        if (amt && amt > 0) {
            btnDonateText.textContent = `Поддержать сервер (~${amt} ₽)`;
            if (btnDonate) {
                btnDonate.title = `Поддержать сервер через СБП (~${amt} ₽)`;
            }
        } else {
            btnDonateText.textContent = 'Поддержать сервер';
            if (btnDonate) {
                btnDonate.title = 'Поддержать сервер через СБП';
            }
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

async function donateServer(e) {
    if (e) e.stopPropagation();
    const btn = document.getElementById('btn-donate-server');
    if (btn) {
        btn.style.pointerEvents = 'none';
        btn.style.opacity = '0.7';
    }
    try {
        await fetch('/api/server-donate', { method: 'POST' });
    } catch (err) {
        console.error('Donate error:', err);
    } finally {
        if (btn) {
            btn.style.pointerEvents = '';
            btn.style.opacity = '';
        }
    }
}

// Initial boot
document.addEventListener('DOMContentLoaded', () => {
    fetchStatus();
    if (typeof window.revealWindow === 'function') {
        window.revealWindow();
    }
    setInterval(fetchStatus, 1500);
});
