// ============================================================================
// МОДУЛЬ ГОЛОСОВАНИЯ СООБЩЕСТВА ЗА ИГРЫ STEAM
// ============================================================================

let selectedSteamGame = null;
let voteSearchTimer = null;
let voteAutoPollTimer = null;
let cachedCommunityVotes = null;

function getVoteGamesSkeletonHtml(count = 3) {
    let html = '';
    for (let i = 0; i < count; i++) {
        html += `
            <div class="vote-game-card skeleton-card">
                <div class="vote-card-main">
                    <div class="skeleton" style="width: 32px; height: 32px; border-radius: 2px; flex-shrink: 0;"></div>
                    <div class="vote-card-info" style="gap: 6px;">
                        <div class="vote-card-topline">
                            <div class="skeleton" style="width: ${110 + (i % 3) * 25}px; height: 12px;"></div>
                            <div class="skeleton" style="width: 40px; height: 10px;"></div>
                        </div>
                        <div class="skeleton" style="width: 100%; height: 3px; border-radius: 1px;"></div>
                    </div>
                </div>
                <div class="skeleton" style="width: 72px; height: 24px; border-radius: 2px; flex-shrink: 0;"></div>
            </div>
        `;
    }
    return html;
}

function getSteamSearchSkeletonHtml() {
    return `
        <div class="vote-autocomplete-item skeleton-card" style="pointer-events: none; display: flex; align-items: center; gap: 8px; padding: 6px 10px;">
            <div class="skeleton" style="width: 20px; height: 20px; border-radius: 2px; flex-shrink: 0;"></div>
            <div class="skeleton" style="width: 140px; height: 11px;"></div>
        </div>
        <div class="vote-autocomplete-item skeleton-card" style="pointer-events: none; display: flex; align-items: center; gap: 8px; padding: 6px 10px;">
            <div class="skeleton" style="width: 20px; height: 20px; border-radius: 2px; flex-shrink: 0;"></div>
            <div class="skeleton" style="width: 110px; height: 11px;"></div>
        </div>
        <div class="vote-autocomplete-item skeleton-card" style="pointer-events: none; display: flex; align-items: center; gap: 8px; padding: 6px 10px;">
            <div class="skeleton" style="width: 20px; height: 20px; border-radius: 2px; flex-shrink: 0;"></div>
            <div class="skeleton" style="width: 160px; height: 11px;"></div>
        </div>
    `;
}

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
    hideVotePreview();
    if (voteSearchTimer) clearTimeout(voteSearchTimer);
    if (!query || query.length < 2) {
        hideVoteAutocomplete();
        return;
    }

    const box = document.getElementById('vote-autocomplete');
    if (box) {
        box.innerHTML = getSteamSearchSkeletonHtml();
        box.style.display = 'block';
    }

    voteSearchTimer = setTimeout(async () => {
        try {
            const res = await fetch(`/api/search-steam?term=${encodeURIComponent(query)}`);
            const data = await res.json();
            const items = (data && data.items) ? data.items : [];
            renderVoteAutocomplete(items);
        } catch (e) {
            hideVoteAutocomplete();
        }
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
        cachedCommunityVotes = null;
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
        cachedCommunityVotes = null;
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
        cachedCommunityVotes = null;
        await loadCommunityVotes();
    } catch (e) {}
}

async function loadCommunityVotes() {
    const listEl = document.getElementById('vote-games-list');
    if (!listEl) return;

    if (cachedCommunityVotes !== null) {
        renderCommunityVotes(cachedCommunityVotes);
    } else if (listEl.children.length === 0 || !listEl.querySelector('.vote-game-card:not(.skeleton-card)')) {
        listEl.innerHTML = getVoteGamesSkeletonHtml(3);
    }

    try {
        const res = await fetch('/api/votes');
        if (!res.ok) {
            if (cachedCommunityVotes === null) {
                listEl.innerHTML = '<div style="color: var(--text-muted); font-size: 11px; padding: 10px 0; text-align: center;">Голосование временно недоступно</div>';
            }
            return;
        }
        const data = await res.json();
        cachedCommunityVotes = data;
        renderCommunityVotes(data);
    } catch (e) {
        if (cachedCommunityVotes === null) {
            listEl.innerHTML = '<div style="color: var(--text-muted); font-size: 11px; padding: 10px 0; text-align: center;">Не удалось загрузить список</div>';
        }
    }
}

function renderCommunityVotes(data) {
    const listEl = document.getElementById('vote-games-list');
    const badgeEl = document.getElementById('user-votes-badge');
    if (!listEl || !data) return;

    if (badgeEl) {
        badgeEl.textContent = `Голоса: ${data.user_votes_used || 0} из ${data.max_user_votes || 3}`;
    }

    const votePower = data.user_vote_power || 1;
    const powerBadge = document.getElementById('user-vote-power-badge');
    if (powerBadge) {
        if (votePower === 3) {
            powerBadge.className = 'badge-vote-power power-sponsor';
            powerBadge.textContent = 'Сила: x3 (Спонсор)';
            powerBadge.title = 'Привилегия спонсора шлюза: 1 голос = 3 очка';
        } else {
            powerBadge.className = 'badge-vote-power';
            powerBadge.textContent = 'Сила: x1';
            powerBadge.title = 'Базовый вес голоса игрока: 1 очко';
        }
    }

    const submitBtn = document.getElementById('btn-submit-propose');
    if (submitBtn) {
        const word = votePower === 1 ? 'голос' : (votePower < 5 ? 'голоса' : 'голосов');
        submitBtn.textContent = `Предложить (+${votePower} ${word})`;
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
            actionEl.textContent = votePower > 1 ? `Голосовать (+${votePower})` : 'Голосовать';
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
}
