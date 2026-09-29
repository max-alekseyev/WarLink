// ui/js/sponsors.js - Sponsors Hall of Fame Logic

let cachedSponsors = null;
let currentSponsorCategory = 'all'; // 'all' | 'month' | 'new'

function getSponsorsSkeletonHtml(count = 4) {
    let html = '';
    for (let i = 0; i < count; i++) {
        html += `
            <div class="sponsor-card skeleton-card">
                <div class="skeleton skeleton-circle" style="width: 36px; height: 36px; flex-shrink: 0;"></div>
                <div class="sponsor-card-info" style="gap: 6px;">
                    <div class="skeleton" style="width: ${90 + (i % 3) * 20}px; height: 12px;"></div>
                    <div class="skeleton" style="width: ${130 + (i % 2) * 25}px; height: 10px;"></div>
                </div>
            </div>
        `;
    }
    return html;
}

function toggleSponsors(e) {
    if (e) e.stopPropagation();
    switchView('view-sponsors');
}

function closeSponsors() {
    switchView(null);
}

function switchSponsorCategory(cat) {
    currentSponsorCategory = cat;
    const tabAll = document.getElementById('tab-sponsors-all');
    const tabMonth = document.getElementById('tab-sponsors-month');
    const tabNew = document.getElementById('tab-sponsors-new');

    if (tabAll) tabAll.classList.toggle('active', cat === 'all');
    if (tabMonth) tabMonth.classList.toggle('active', cat === 'month');
    if (tabNew) tabNew.classList.toggle('active', cat === 'new');

    if (cachedSponsors) {
        renderSponsors(cachedSponsors);
    }
}

async function fetchSponsors() {
    const grid = document.getElementById('sponsors-grid');
    if (!grid) return;

    if (typeof UIStore !== 'undefined' && typeof UIStore.requestSWR === 'function') {
        await UIStore.requestSWR(
            '/api/sponsors',
            async () => {
                const resp = await fetch('/api/sponsors');
                if (!resp.ok) return null;
                return await resp.json();
            },
            (data, isFresh) => {
                const sponsors = (data && data.sponsors) ? data.sponsors : [];
                cachedSponsors = sponsors;
                updateCommunityGoal(cachedSponsors);
                renderSponsors(cachedSponsors);
            },
            () => {
                if (grid.children.length === 0) {
                    grid.innerHTML = getSponsorsSkeletonHtml(4);
                }
            }
        );
        return;
    }

    if (cachedSponsors !== null) {
        renderSponsors(cachedSponsors);
        updateCommunityGoal(cachedSponsors);
    } else if (grid.children.length === 0 || !grid.querySelector('.sponsor-card')) {
        grid.innerHTML = getSponsorsSkeletonHtml(4);
    }

    try {
        const resp = await fetch('/api/sponsors');
        if (!resp.ok) return;
        const data = await resp.json();
        cachedSponsors = data.sponsors || [];
        updateCommunityGoal(cachedSponsors);
        renderSponsors(cachedSponsors);
    } catch (e) {
        console.error('Fetch sponsors error:', e);
    }
}

let goalCountdownTimer = null;
const OCT_START_MSK = new Date('2026-10-01T00:00:00+03:00').getTime();

function updateCommunityGoalVisibility(visible) {
    if (visible === undefined && window.enableCommunityGoal !== undefined) {
        visible = window.enableCommunityGoal;
    }
    const goalBox = document.querySelector('.community-goal-box');
    if (goalBox) {
        goalBox.style.display = (visible === false) ? 'none' : 'flex';
    }
}

function updateCommunityGoalTimer() {
    const labelEl = document.getElementById('goal-timer-label');
    const timeEl = document.getElementById('goal-timer-countdown');
    if (!labelEl || !timeEl) return;

    const now = Date.now();
    if (now < OCT_START_MSK) {
        const diff = OCT_START_MSK - now;
        const days = Math.floor(diff / (1000 * 60 * 60 * 24));
        const hours = Math.floor((diff / (1000 * 60 * 60)) % 24);
        const mins = Math.floor((diff / (1000 * 60)) % 60);
        const secs = Math.floor((diff / 1000) % 60);

        labelEl.textContent = 'Старт статистики с 1 октября:';
        timeEl.textContent = `${days}д ${String(hours).padStart(2, '0')}:${String(mins).padStart(2, '0')}:${String(secs).padStart(2, '0')}`;
    } else {
        labelEl.textContent = 'Период сбора:';
        timeEl.textContent = 'Октябрь 2026';
    }
}

function updateCommunityGoal(sponsors) {
    updateCommunityGoalVisibility();
    updateCommunityGoalTimer();

    if (!goalCountdownTimer) {
        goalCountdownTimer = setInterval(updateCommunityGoalTimer, 1000);
    }

    const fillEl = document.getElementById('frankfurt-progress-fill');
    const textEl = document.getElementById('frankfurt-progress-text');
    const pctEl = document.getElementById('frankfurt-progress-pct');
    const stockholmBadge = document.getElementById('stockholm-goal-badge');

    if (!fillEl || !textEl || !pctEl) return;

    const now = Date.now();
    if (now < OCT_START_MSK) {
        // До 1 октября 00:00 МСК сбор еще не стартовал
        if (stockholmBadge) {
            stockholmBadge.className = 'badge-covered';
            stockholmBadge.textContent = 'АКТИВЕН';
        }
        fillEl.style.width = '0%';
        textEl.textContent = 'Статистика начнется с 1 октября';
        pctEl.textContent = '0%';
        return;
    }

    // Начиная с 1 октября 00:00 МСК: Модель А (Накопительный фонд)
    let octPoolRub = (typeof window.octoberPoolRub === 'number') ? window.octoberPoolRub : 0;
    if (typeof window.octoberPoolRub !== 'number' && Array.isArray(sponsors)) {
        sponsors.forEach(s => {
            const sDate = s.created_at ? new Date(s.created_at).getTime() : 0;
            if (sDate >= OCT_START_MSK) {
                octPoolRub += (s.total_donated_rub || 0);
            }
        });
    }

    const stockholmCostRub = 200; // 2 €
    if (octPoolRub < stockholmCostRub) {
        if (stockholmBadge) {
            const stockPct = Math.round((octPoolRub / stockholmCostRub) * 100);
            stockholmBadge.className = 'badge-covered';
            stockholmBadge.textContent = `Собрано ${stockPct}%`;
        }
        fillEl.style.width = '0%';
        textEl.textContent = 'Собрано: 0 € из 12 €';
        pctEl.textContent = '0%';
    } else {
        if (stockholmBadge) {
            stockholmBadge.className = 'badge-covered';
            stockholmBadge.textContent = 'ПОКРЫТО 100%';
        }
        const extraRub = octPoolRub - stockholmCostRub;
        const frankfurtEuro = Math.min(12, Math.floor(extraRub / 100));
        const pct = Math.min(100, Math.round((frankfurtEuro / 12) * 100));

        fillEl.style.width = `${pct}%`;
        textEl.textContent = `Собрано: ${frankfurtEuro} € из 12 €`;
        pctEl.textContent = `${pct}%`;
    }
}

function renderSponsors(sponsors) {
    const grid = document.getElementById('sponsors-grid');
    const badge = document.getElementById('sponsors-count-badge');
    if (!grid) return;

    if (badge) {
        badge.textContent = `Спонсоров: ${sponsors.length}`;
    }

    if (sponsors.length === 0) {
        grid.innerHTML = `
            <div style="grid-column: 1 / -1; text-align: center; color: var(--text-muted); padding: 30px 10px; font-size: 11px;">
                Пока нет активных спонсоров. Станьте первым спонсором шлюза!
            </div>
        `;
        return;
    }

    // Filter and sort by active category
    let displayedSponsors = [...sponsors];
    if (currentSponsorCategory === 'month') {
        displayedSponsors = displayedSponsors.filter(s => s.is_active);
    } else if (currentSponsorCategory === 'new') {
        displayedSponsors.sort((a, b) => {
            const dateA = a.joined_date || '';
            const dateB = b.joined_date || '';
            return dateB.localeCompare(dateA);
        });
    }

    if (displayedSponsors.length === 0) {
        const emptyMsg = currentSponsorCategory === 'month' 
            ? 'В этом месяце нет активных спонсоров.' 
            : 'В данной категории пока нет записей.';
        grid.innerHTML = `
            <div style="grid-column: 1 / -1; text-align: center; color: var(--text-muted); padding: 24px 10px; font-size: 11px;">
                ${emptyMsg}
            </div>
        `;
        return;
    }

    // Keyed reconciliation to prevent DOM re-creation
    const existingCards = new Map();
    Array.from(grid.querySelectorAll('.sponsor-card[data-sponsor-key]')).forEach(el => {
        existingCards.set(el.dataset.sponsorKey, el);
    });

    if (existingCards.size === 0 && (grid.querySelector('.skeleton-card') || grid.querySelector('div:not(.sponsor-card)'))) {
        grid.innerHTML = '';
    }

    const currentKeys = new Set();

    displayedSponsors.forEach((s) => {
        const originalIdx = cachedSponsors ? cachedSponsors.indexOf(s) : 0;
        const sponsorKey = s.account_number || s.nickname || ('idx-' + originalIdx);
        currentKeys.add(sponsorKey);

        const cleanNick = (window.NobelCallsigns && window.NobelCallsigns.sanitizeNickname)
            ? window.NobelCallsigns.sanitizeNickname(s.nickname, s.account_number)
            : (s.nickname && !s.nickname.includes('****') ? s.nickname : 'Аноним');
        const nick = escapeHtml(cleanNick);
        const isSecret = Boolean(s.hide_donation_amount || s.is_secret || s.secret_donations);
        let metaText = `Спонсор сервера • ${escapeHtml(s.joined_date || '')}`;
        if (isSecret) {
            metaText = `[Вклад: засекречен] • ${escapeHtml(s.joined_date || '')}`;
        } else if (s.total_donated_rub > 0) {
            metaText = `Вклад: ${s.total_donated_rub} ₽ • ${escapeHtml(s.joined_date || '')}`;
        }

        const existing = existingCards.get(sponsorKey);
        if (existing) {
            existing.onclick = () => openSponsorDossierByIndex(originalIdx >= 0 ? originalIdx : 0);
            const nickEl = existing.querySelector('.sponsor-card-nick');
            if (nickEl && nickEl.textContent !== nick) nickEl.textContent = nick;
            const metaEl = existing.querySelector('.sponsor-card-meta');
            if (metaEl && metaEl.textContent !== metaText) metaEl.textContent = metaText;

            const imgEl = existing.querySelector('img.sponsor-card-avatar');
            if (s.avatar_url) {
                if (imgEl) {
                    if (imgEl.src !== s.avatar_url) imgEl.src = s.avatar_url;
                } else {
                    const placeholder = existing.querySelector('.sponsor-card-placeholder');
                    if (placeholder) {
                        const newImg = document.createElement('img');
                        newImg.className = 'sponsor-card-avatar';
                        newImg.src = escapeHtml(s.avatar_url);
                        newImg.alt = '';
                        placeholder.replaceWith(newImg);
                    }
                }
            } else if (imgEl) {
                const placeholder = document.createElement('div');
                placeholder.className = 'sponsor-card-placeholder';
                placeholder.innerHTML = `
                    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2"/>
                        <circle cx="12" cy="7" r="4"/>
                    </svg>
                `;
                imgEl.replaceWith(placeholder);
            }
            grid.appendChild(existing);
        } else {
            const card = document.createElement('div');
            card.className = 'sponsor-card';
            card.dataset.sponsorKey = sponsorKey;
            card.title = 'Посмотреть боевое досье оператора';
            card.onclick = () => openSponsorDossierByIndex(originalIdx >= 0 ? originalIdx : 0);

            const avatarHtml = s.avatar_url ? `
                <img class="sponsor-card-avatar" src="${escapeHtml(s.avatar_url)}" alt="">
            ` : `
                <div class="sponsor-card-placeholder">
                    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2"/>
                        <circle cx="12" cy="7" r="4"/>
                    </svg>
                </div>
            `;

            card.innerHTML = `
                ${avatarHtml}
                <div class="sponsor-card-info">
                    <span class="sponsor-card-nick">${nick}</span>
                    <span class="sponsor-card-meta">${metaText}</span>
                </div>
                <div class="sponsor-card-action">
                    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>
                        <polyline points="14 2 14 8 20 8"/>
                        <line x1="16" y1="13" x2="8" y2="13"/>
                        <line x1="16" y1="17" x2="8" y2="17"/>
                        <polyline points="10 9 9 9 8 9"/>
                    </svg>
                </div>
            `;
            grid.appendChild(card);
        }
    });

    existingCards.forEach((el, key) => {
        if (!currentKeys.has(key) && el.parentNode === grid) {
            grid.removeChild(el);
        }
    });
}
