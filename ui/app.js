let isConnected = false;
let isBusy = false;
let isDownloadingDeps = false;
let isInitializing = false;
let freeInternetEnabled = false;
let currentView = 'showcase'; // 'showcase' or 'game'
let selectedGameId = 'wardogs';
const defaultGames = [
    {
        id: 'wardogs',
        title: 'WARDOGS',
        steam_app_id: '1867240',
        icon_url: 'wardogs_icon.png',
        last_played: 1789653625,
        is_default: true,
        launch_count: 0
    }
];
let cachedGames = defaultGames;

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

async function triggerBenchmark() {
    toggleDetails();
    try {
        await fetch('/api/run-benchmark', { method: 'POST' });
    } catch (e) {
        console.error('Benchmark error:', e);
    }
}

// --- View Switching ---
function showShowcaseView() {
    currentView = 'showcase';
    const vShowcase = document.getElementById('view-showcase');
    const vGame = document.getElementById('view-game');
    if (vShowcase) vShowcase.style.display = 'flex';
    if (vGame) vGame.style.display = 'none';
}

function showGameView(gameId) {
    if (gameId) {
        selectedGameId = gameId;
        selectGame(gameId);
        const game = cachedGames.find(g => g.id === gameId);
        if (game) {
            const titleEl = document.getElementById('game-title');
            const badgeEl = document.getElementById('game-badge-type');
            const imgEl = document.getElementById('game-identity-img');
            const fbEl = document.getElementById('game-identity-fallback');
            if (titleEl) titleEl.textContent = game.title || 'Игра';
            if (badgeEl) badgeEl.textContent = game.steam_app_id ? 'STEAM' : 'EXE';
            if (imgEl) {
                const iconSrc = game.icon_url || (game.id === 'wardogs' ? 'wardogs_icon.png' : '');
                if (iconSrc) {
                    imgEl.src = iconSrc;
                    imgEl.style.display = 'block';
                    if (fbEl) fbEl.style.display = 'none';
                } else {
                    imgEl.style.display = 'none';
                    if (fbEl) fbEl.style.display = 'flex';
                }
            }
        }
    }
    currentView = 'game';
    const vShowcase = document.getElementById('view-showcase');
    const vGame = document.getElementById('view-game');
    if (vShowcase) vShowcase.style.display = 'none';
    if (vGame) vGame.style.display = 'flex';
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
        }
    } catch (err) {
        console.error('Free internet toggle error:', err);
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
let lastShowcaseStateKey = '';
let launchingGameId = null;
let isVotingEnabled = true;
let isDonateEnabled = true;

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

function onGameClick(gameId) {
    const game = cachedGames.find(g => g.id === gameId);
    const launchCount = (game && typeof game.launch_count === 'number') ? game.launch_count : 0;
    if (launchCount === 0) {
        // First launch: inspect settings and routing details
        showGameView(gameId);
    } else {
        // Subsequent launches: quick connect and launch game with spinning indicator
        launchingGameId = gameId;
        renderShowcase(cachedGames, gameId);
        quickLaunchGame(gameId);
    }
}

async function quickLaunchGame(gameId) {
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

    const menu = document.getElementById('game-context-menu');
    if (!menu) return;

    const btnDelete = document.getElementById('ctx-btn-delete');
    if (btnDelete) {
        if (activeContextMenuIsDefault) {
            btnDelete.classList.add('is-disabled');
            btnDelete.disabled = true;
        } else {
            btnDelete.classList.remove('is-disabled');
            btnDelete.disabled = false;
        }
    }

    menu.style.display = 'flex';
    const menuWidth = menu.offsetWidth || 150;
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
        quickLaunchGame(gid);
    }
}

function onContextSettings() {
    const gid = activeContextMenuGameId;
    hideGameContextMenu();
    if (gid) {
        showGameView(gid);
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

document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
        hideGameContextMenu();
    }
});

async function selectGame(gameId) {
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
        fetchStatus();
    } catch (e) {}
}

// --- Add Game Modal & Unified Input Resolution ---
let steamResolveTimeout = null;
let resolvedIconUrl = '';
let resolvedSteamAppId = '';
let resolvedTitle = '';
let selectedExePath = '';

function onGameInput(val) {
    val = (val || '').trim();
    if (steamResolveTimeout) clearTimeout(steamResolveTimeout);

    const previewBox = document.getElementById('game-preview-box');
    const previewImg = document.getElementById('game-preview-img');
    const previewName = document.getElementById('game-preview-name');
    const previewDesc = document.getElementById('game-preview-desc');

    if (!val) {
        if (previewBox) previewBox.style.display = 'none';
        resolvedIconUrl = '';
        resolvedSteamAppId = '';
        resolvedTitle = '';
        selectedExePath = '';
        return;
    }

    // If it's a file path (.exe)
    if (val.toLowerCase().endsWith('.exe')) {
        selectedExePath = val;
        const base = val.split(/[/\\]/).pop().replace(/\.exe$/i, '');
        if (previewBox) {
            previewBox.style.display = 'flex';
            if (previewImg) previewImg.style.display = 'none';
            if (previewName) previewName.textContent = base;
            if (previewDesc) previewDesc.textContent = 'Исполняемый файл: ' + val;
        }
        return;
    }

    steamResolveTimeout = setTimeout(async () => {
        try {
            const res = await fetch(`/api/resolve-steam?appid=${encodeURIComponent(val)}`);
            if (res.ok) {
                const data = await res.json();
                if (data.title || data.icon_url) {
                    resolvedIconUrl = data.icon_url || '';
                    resolvedTitle = data.title || '';
                    const match = val.match(/\b\d{3,9}\b/);
                    if (match) resolvedSteamAppId = match[0];

                    if (previewBox) {
                        previewBox.style.display = 'flex';
                        if (previewImg) {
                            if (resolvedIconUrl) {
                                previewImg.src = resolvedIconUrl;
                                previewImg.style.display = 'block';
                            } else {
                                previewImg.style.display = 'none';
                            }
                        }
                        if (previewName) previewName.textContent = resolvedTitle || val;
                        if (previewDesc) previewDesc.textContent = resolvedSteamAppId ? ('Steam AppID: ' + resolvedSteamAppId) : 'Найдено в Steam';
                    }
                    return;
                }
            }
        } catch (e) {}

        if (!/\b\d{3,9}\b/.test(val)) {
            if (previewBox) previewBox.style.display = 'none';
        }
    }, 150);
}

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
    const fallbackSvg = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <line x1="6" x2="10" y1="11" y2="11" />
        <line x1="8" x2="8" y1="9" y2="13" />
        <line x1="15" x2="15.01" y1="12" y2="12" />
        <line x1="18" x2="18.01" y1="10" y2="10" />
        <path d="M17.32 5H6.68a4 4 0 0 0-3.978 3.59c-.006.052-.01.101-.017.152C2.604 9.416 2 14.456 2 16a3 3 0 0 0 3 3c1 0 1.5-.5 2-1l1.414-1.414A2 2 0 0 1 9.828 16h4.344a2 2 0 0 1 1.414.586L17 18c.5.5 1 1 2 1a3 3 0 0 0 3-3c0-1.545-.604-6.584-.685-7.258-.007-.05-.011-.1-.017-.151A4 4 0 0 0 17.32 5z" />
    </svg>`;

    items.slice(0, 6).forEach(item => {
        const div = document.createElement('div');
        div.className = 'vote-autocomplete-item';
        const iconSrc = item.tiny_image || item.icon_url || item.icon || '';
        div.innerHTML = `
            <div class="vote-item-icon-box">
                ${iconSrc ? `<img class="vote-item-icon" src="${escapeHtml(iconSrc)}" alt="" onerror="this.style.display='none'; if (this.nextElementSibling) this.nextElementSibling.style.display='flex';">` : ''}
                <div class="vote-fallback-icon" style="${iconSrc ? 'display:none;' : 'display:flex;'}">
                    ${fallbackSvg}
                </div>
            </div>
            <span class="vote-item-title">${escapeHtml(item.name || 'Игра')}</span>
        `;
        div.onclick = () => selectSteamGameForVote(item);
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
            alert(data.error || 'Ошибка при отправке голоса');
            return;
        }
        hideVotePreview();
        const input = document.getElementById('vote-search-input');
        if (input) input.value = '';
        await loadCommunityVotes();
    } catch (e) {
        alert('Не удалось связаться с сервером голосования');
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
            alert(data.error || 'Ошибка при голосовании');
            return;
        }
        await loadCommunityVotes();
    } catch (e) {
        alert('Ошибка связи с сервером');
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

            const pct = Math.min(100, Math.round((g.votes_count / 50) * 100));
            const isWinner = g.status === 'queue_integration' || g.votes_count >= 50;

            let actionBtn = '';
            if (isWinner) {
                actionBtn = '<span class="badge-winner">В очереди на интеграцию</span>';
            } else if (g.user_voted) {
                actionBtn = `<button class="btn-vote active" onclick="retractGameVote(${g.steam_app_id})" title="Нажмите, чтобы отозвать голос">Отдано</button>`;
            } else {
                const titleEsc = escapeHtml(g.title).replace(/'/g, "\\'");
                const iconEsc = escapeHtml(g.icon_url).replace(/'/g, "\\'");
                actionBtn = `<button class="btn-vote" onclick="voteForGame(${g.steam_app_id}, '${titleEsc}', '${iconEsc}')">Голосовать</button>`;
            }

            const iconSrc = g.icon_url || '';
            const fallbackSvg = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <line x1="6" x2="10" y1="11" y2="11" />
                <line x1="8" x2="8" y1="9" y2="13" />
                <line x1="15" x2="15.01" y1="12" y2="12" />
                <line x1="18" x2="18.01" y1="10" y2="10" />
                <path d="M17.32 5H6.68a4 4 0 0 0-3.978 3.59c-.006.052-.01.101-.017.152C2.604 9.416 2 14.456 2 16a3 3 0 0 0 3 3c1 0 1.5-.5 2-1l1.414-1.414A2 2 0 0 1 9.828 16h4.344a2 2 0 0 1 1.414.586L17 18c.5.5 1 1 2 1a3 3 0 0 0 3-3c0-1.545-.604-6.584-.685-7.258-.007-.05-.011-.1-.017-.151A4 4 0 0 0 17.32 5z" />
            </svg>`;

            card.innerHTML = `
                <div class="vote-card-main">
                    <div class="vote-card-icon-box">
                        ${iconSrc ? `<img class="vote-card-icon" src="${escapeHtml(iconSrc)}" alt="" onerror="this.style.display='none'; if (this.nextElementSibling) this.nextElementSibling.style.display='flex';">` : ''}
                        <div class="vote-fallback-icon" style="${iconSrc ? 'display:none;' : 'display:flex;'}">
                            ${fallbackSvg}
                        </div>
                    </div>
                    <div class="vote-card-info">
                        <div class="vote-card-topline">
                            <span class="vote-card-name" title="${escapeHtml(g.title)}">${escapeHtml(g.title)}</span>
                            <span class="vote-card-stats">${g.votes_count} / 50</span>
                        </div>
                        <div class="vote-progress-track">
                            <div class="vote-progress-fill ${isWinner ? 'winner' : ''}" style="width: ${pct}%;"></div>
                        </div>
                    </div>
                </div>
                ${actionBtn}
            `;
            listEl.appendChild(card);
        });
    } catch (e) {
        listEl.innerHTML = '<div style="color: var(--text-muted); font-size: 11px; padding: 10px 0; text-align: center;">Не удалось загрузить список</div>';
    }
}

// --- API / State Sync ---
async function fetchStatus() {
    try {
        const res = await fetch('/api/status');
        const data = await res.json();
        updateUI(data);
    } catch (e) {}
}

function updateUI(data) {
    isConnected = data.is_connected;
    isBusy = data.is_busy;
    isDownloadingDeps = !!data.is_downloading_deps;
    isInitializing = !!data.is_initializing;

    if (isConnected || (!isBusy && !isDownloadingDeps)) {
        if (launchingGameId && !isBusy) {
            launchingGameId = null;
        }
    }

    if (data.version) {
        const verEl = document.querySelector('.brand-version');
        if (verEl) verEl.textContent = data.version;
    }

    // Check blocking overlay (updater or first-time component initialization)
    const updateOverlay = document.getElementById('view-update-overlay');
    if (updateOverlay) {
        if (data.is_updating) {
            updateOverlay.style.display = 'flex';
            const title = document.getElementById('update-title');
            const bar = document.getElementById('update-bar');
            const msg = document.getElementById('update-msg');
            const note = document.getElementById('update-note');
            if (title) title.textContent = 'Обновление WarLink...';
            if (bar) bar.style.width = (data.update_pct || 0) + '%';
            if (msg && data.update_msg) msg.textContent = data.update_msg;
            if (note) note.textContent = 'Пользовательские настройки и список игр гарантированно сохранены';
            return; // Lock interface while updating!
        } else if (data.is_initializing) {
            updateOverlay.style.display = 'flex';
            const title = document.getElementById('update-title');
            const bar = document.getElementById('update-bar');
            const msg = document.getElementById('update-msg');
            const note = document.getElementById('update-note');
            if (title) title.textContent = data.init_title || 'Первичная настройка WarLink...';
            if (bar) bar.style.width = (data.init_pct || 0) + '%';
            if (msg && data.init_msg) msg.textContent = data.init_msg;
            if (note) note.textContent = 'Выполняется один раз при первом запуске приложения';
            return; // Lock interface while initializing components!
        } else {
            updateOverlay.style.display = 'none';
        }
    }

    // Update Free Internet toggle
    freeInternetEnabled = !!data.free_internet;
    updateFreeInternetUI(freeInternetEnabled);

    // Update showcase grid
    if (data.games) {
        renderShowcase(data.games, data.selected_game_id);
    }

    // Update selected game drill-down
    if (data.selected_game) {
        const titleEl = document.getElementById('game-title');
        const badgeEl = document.getElementById('game-badge-type');
        const imgEl = document.getElementById('game-identity-img');
        if (titleEl) titleEl.textContent = data.selected_game.title || 'Игра';
        if (badgeEl) {
            badgeEl.textContent = data.selected_game.steam_app_id ? 'STEAM' : 'EXE';
        }
        const fbEl = document.getElementById('game-identity-fallback');
        if (imgEl) {
            if (data.selected_game.icon_url) {
                imgEl.src = data.selected_game.icon_url;
                imgEl.style.display = 'block';
                if (fbEl) fbEl.style.display = 'none';
            } else {
                imgEl.style.display = 'none';
                if (fbEl) fbEl.style.display = 'flex';
            }
        }
    }

    // Update routing profile metric
    const profMetric = document.getElementById('metric-profile');
    if (profMetric && data.profile) {
        profMetric.textContent = `Профиль: ${data.profile}`;
    }

    // Update real ping & packet loss telemetry metrics
    const pingMetric = document.getElementById('metric-ping');
    const lossMetric = document.getElementById('metric-loss');
    if (pingMetric) {
        if (isConnected && data.ping_ms && data.ping_ms > 0) {
            pingMetric.textContent = `Задержка: ${data.ping_ms} ms`;
        } else if (isConnected) {
            pingMetric.textContent = 'Задержка: измеряется...';
        } else {
            pingMetric.textContent = 'Задержка: -- ms';
        }
    }
    if (lossMetric) {
        if (isConnected && typeof data.packet_loss === 'number') {
            lossMetric.textContent = `Потери: ${data.packet_loss}%`;
        } else {
            lossMetric.textContent = 'Потери: 0%';
        }
    }

    // Update Stockholm Gateway footer metrics
    const gwPingEl = document.getElementById('gw-ping');
    const gwSlotsEl = document.getElementById('gw-slots');
    const gwDaysEl = document.getElementById('gw-days');
    const btnDonateText = document.getElementById('btn-donate-text');
    if (gwPingEl) {
        if (data.gateway_ping && data.gateway_ping > 1) {
            gwPingEl.textContent = data.gateway_ping + ' мс';
        } else {
            gwPingEl.textContent = '— мс';
        }
    }
    if (gwSlotsEl) {
        gwSlotsEl.textContent = data.gateway_slots || '—';
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

    const btnToggle = document.getElementById('btn-toggle');
    const badgeStatus = document.getElementById('badge-status');
    const badgeText = document.getElementById('badge-text');
    const statusDesc = document.getElementById('status-desc');
    const selectProfile = document.getElementById('select-profile');
    const chkAuto = document.getElementById('chk-autolaunch');

    if (chkAuto && chkAuto.checked !== data.autolaunch_game) {
        chkAuto.checked = data.autolaunch_game;
    }

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
        if (data.profile && selectProfile.value !== data.profile) {
            selectProfile.value = data.profile;
        }
        selectProfile.disabled = (isConnected || isBusy || isDownloadingDeps);
    }

    let targetText = 'ПОДКЛЮЧИТЬ';
    let targetClass = 'btn btn-primary';
    let targetBadgeText = 'ГОТОВ';
    let targetBadgeClass = 'status-line status-standby';
    let targetDesc = 'Сетевой маршрут готов к подключению';
    let targetDisabled = false;

    if (isDownloadingDeps) {
        targetDisabled = true;
        targetText = 'ЗАГРУЗКА КОМПОНЕНТОВ…';
        targetClass = 'btn btn-primary btn-busy';
        targetBadgeClass = 'status-line status-busy';
        targetBadgeText = 'ЗАГРУЗКА…';
        targetDesc = data.deps_msg || 'Первичное скачивание сетевых компонентов с GitHub…';
    } else if (isBusy) {
        targetDisabled = true;
        targetText = 'ПОДКЛЮЧЕНИЕ…';
        targetClass = 'btn btn-primary btn-busy';
        targetBadgeClass = 'status-line status-busy';
        targetBadgeText = 'ПОДКЛЮЧЕНИЕ…';
        targetDesc = 'Выполняется синхронизация сетевого туннеля';
    } else if (isConnected) {
        targetDisabled = false;
        targetText = 'ОТКЛЮЧИТЬ';
        targetClass = 'btn btn-primary btn-active';
        targetBadgeClass = 'status-line status-active';
        targetBadgeText = 'ПОДКЛЮЧЕНО';
        targetDesc = 'Сетевой фильтр десинхронизации активен (WinDivert)';
    }

    if (btnToggle) {
        btnToggle.disabled = targetDisabled;
        const btnSpan = document.getElementById('btn-text');
        if (btnSpan) btnSpan.textContent = targetText;
        btnToggle.className = targetClass;
    }

    const btnSpinner = document.getElementById('btn-spinner');
    if (btnSpinner) {
        btnSpinner.style.display = (isBusy || isDownloadingDeps) ? 'inline-block' : 'none';
    }

    if (badgeStatus) badgeStatus.className = targetBadgeClass;
    if (badgeText) badgeText.textContent = targetBadgeText;
    if (statusDesc) statusDesc.textContent = targetDesc;

    // Progress bar
    const b = data.progress;
    const mainBox = document.getElementById('main-progress-box');
    const mainMsg = document.getElementById('main-progress-msg');
    const mainPct = document.getElementById('main-progress-pct');
    const mainFill = document.getElementById('main-progress-fill');

    if (b && b.is_running) {
        if (mainBox) mainBox.style.display = 'flex';
        if (mainMsg && b.message) mainMsg.textContent = b.message;
        if (mainPct) mainPct.textContent = `${b.percent}%`;
        if (mainFill) mainFill.style.width = `${b.percent}%`;
    } else {
        if (mainBox) mainBox.style.display = 'none';
    }

    // Footer last log message
    if (data.logs && data.logs.length > 0) {
        const last = data.logs[data.logs.length - 1];
        const lastEl = document.getElementById('last-log-msg');
        if (lastEl) lastEl.textContent = last;
    }
}

async function toggleConnect() {
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

async function onAutoLaunchChange() {
    const chk = document.getElementById('chk-autolaunch');
    if (!chk) return;
    try {
        await fetch('/api/settings', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ autolaunch_game: chk.checked })
        });
    } catch (e) {}
}

async function onProfileChange(val) {
    if (!val) return;
    try {
        await fetch('/api/profile', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ profile: val })
        });
    } catch (e) {}
}

function openLogFile() {
    fetch('/api/open-log').catch(() => {});
}

function openSingboxLogFile() {
    fetch('/api/open-singbox-log').catch(() => {});
}

const defaultProfiles = [
    'general (ALT9)',
    'general (ALT11)',
    'general (ALT)',
    'general (ALT1)',
    'general (ALT2)',
    'general (ALT3)',
    'general (ALT4)',
    'general (ALT5)',
    'general (ALT6)',
    'general (ALT7)',
    'general (ALT8)',
    'general (ALT10)',
    'general (ALT12)',
    'general (ALT13)',
    'general (FAKE TLS AUTO)',
    'general (SIMPLE FAKE)',
    'general'
];

function initProfileOptions() {
    const sel = document.getElementById('select-profile');
    if (sel && sel.options.length <= 1) {
        sel.innerHTML = '';
        defaultProfiles.forEach(p => {
            const opt = document.createElement('option');
            opt.value = p;
            opt.textContent = p;
            sel.appendChild(opt);
        });
        sel.value = 'general (ALT9)';
    }
}

function triggerReveal() {
    if (window.revealWindow) {
        window.revealWindow();
    }
}

// Initial showcase render & status poll
initProfileOptions();
renderShowcase(defaultGames, selectedGameId);
document.addEventListener('DOMContentLoaded', () => {
    initProfileOptions();
    renderShowcase(defaultGames, selectedGameId);
    triggerReveal();
});
window.addEventListener('load', triggerReveal);
setTimeout(triggerReveal, 100);

setInterval(fetchStatus, 800);
fetchStatus();

async function donateServer(e) {
    if (e) e.stopPropagation();
    const btn = document.getElementById('btn-donate-server');
    if (btn) {
        btn.disabled = true;
        btn.style.opacity = '0.6';
    }
    try {
        await fetch('/api/server-donate', { method: 'POST' });
    } catch (err) {
        console.error('Donate error:', err);
    } finally {
        setTimeout(() => {
            if (btn) {
                btn.disabled = false;
                btn.style.opacity = '1';
            }
        }, 1500);
    }
}

// --- Keyboard Layout Switching (Alt+Shift, Ctrl+Shift) ---
let isAltDown = false;
let isShiftDown = false;
let isCtrlDown = false;

window.addEventListener('keydown', (e) => {
    if (e.key === 'Alt') isAltDown = true;
    if (e.key === 'Shift') isShiftDown = true;
    if (e.key === 'Control') isCtrlDown = true;

    if ((e.key === 'Shift' && (isAltDown || e.altKey || isCtrlDown || e.ctrlKey)) ||
        (e.key === 'Alt' && (isShiftDown || e.shiftKey)) ||
        (e.key === 'Control' && (isShiftDown || e.shiftKey))) {
        if (window.switchKeyboardLayout) {
            window.switchKeyboardLayout();
        }
    }
}, true);

window.addEventListener('keyup', (e) => {
    if (e.key === 'Alt') isAltDown = false;
    if (e.key === 'Shift') isShiftDown = false;
    if (e.key === 'Control') isCtrlDown = false;
}, true);

