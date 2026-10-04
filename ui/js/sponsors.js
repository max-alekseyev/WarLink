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

async function updateCommunityGoal(sponsors) {
    updateCommunityGoalVisibility();
    updateCommunityGoalTimer();

    if (!goalCountdownTimer) {
        goalCountdownTimer = setInterval(updateCommunityGoalTimer, 1000);
    }

    const infraFillEl = document.getElementById('infra-progress-fill');
    const infraTextEl = document.getElementById('infra-progress-text');
    const infraPctEl = document.getElementById('infra-progress-pct');
    const infraNode = document.getElementById('goal-node-infra');

    const frankFillEl = document.getElementById('frankfurt-progress-fill');
    const frankTextEl = document.getElementById('frankfurt-progress-text');
    const frankPctEl = document.getElementById('frankfurt-progress-pct');
    const frankNode = document.getElementById('goal-node-frankfurt');

    const bf6FillEl = document.getElementById('bf6-progress-fill');
    const bf6TextEl = document.getElementById('bf6-progress-text');
    const bf6PctEl = document.getElementById('bf6-progress-pct');
    const bf6Card = document.getElementById('goal-special-bf6');
    const bf6TargetLabel = document.getElementById('bf6-target-label');

    try {
        const res = await fetch('/api/community-goal');
        if (res.ok) {
            const data = await res.json();
            if (data && data.success) {
                // 1. Unified Cluster infrastructure (13 EUR / 1690 RUB / mo)
                if (data.infrastructure && infraFillEl) {
                    const infra = data.infrastructure;
                    const pct = Math.min(100, Math.max(0, infra.percent || 0));
                    infraFillEl.style.width = pct + '%';
                    if (infraTextEl) {
                        infraTextEl.textContent = 'Баланс: ' + (infra.current_balance_rub || 0).toLocaleString('ru-RU') + ' ₽ (' + infra.days_left + ' дн.)';
                    }
                    if (infraPctEl) {
                        infraPctEl.textContent = pct + '%';
                    }
                    if (infraNode) {
                        infraNode.title = 'Инфраструктура кластера: Стокгольм + Москва + Франкфурт (13 € / мес)';
                        const titleSpan = infraNode.querySelector('.compact-node-title');
                        if (titleSpan) {
                            titleSpan.textContent = 'Инфраструктура кластера (13 € / мес):';
                        }
                        const flagsCont = infraNode.querySelector('.compact-flags');
                        if (flagsCont && flagsCont.children.length === 2) {
                            const deFlag = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
                            deFlag.setAttribute('class', 'node-flag');
                            deFlag.setAttribute('width', '12');
                            deFlag.setAttribute('height', '9');
                            deFlag.setAttribute('viewBox', '0 0 16 11');
                            deFlag.setAttribute('fill', 'none');
                            deFlag.innerHTML = '<rect width="16" height="3.67" rx="1" fill="#202020"/><rect y="3.67" width="16" height="3.67" fill="#DD1111"/><rect y="7.33" width="16" height="3.67" rx="1" fill="#FFCE00"/>';
                            flagsCont.appendChild(deFlag);
                        }
                        if (infra.is_covered || pct >= 100) {
                            infraNode.classList.add('covered');
                            infraNode.classList.remove('target');
                        } else {
                            infraNode.classList.remove('covered');
                            infraNode.classList.add('target');
                        }
                    }
                }

                // If legacy DOM has separate Frankfurt strip item, hide it and divider
                if (frankNode) {
                    frankNode.style.display = 'none';
                    const divEl = document.querySelector('.compact-strip-divider');
                    if (divEl) divEl.style.display = 'none';
                }

                // 3. Battlefield 6 Special Project
                if (data.special_projects && data.special_projects.length > 0 && bf6FillEl) {
                    const bf6 = data.special_projects[0];
                    const pct = Math.min(100, Math.max(0, bf6.percent || 0));
                    bf6FillEl.style.width = pct + '%';
                    if (bf6TargetLabel) {
                        bf6TargetLabel.textContent = (bf6.target_amount_rub || 1600).toLocaleString('ru-RU') + ' ₽';
                    }
                    if (bf6TextEl) {
                        if (bf6.is_completed) {
                            bf6TextEl.textContent = 'Цель достигнута! Игра получена разработчиком';
                        } else {
                            bf6TextEl.textContent = 'Собрано: ' + (bf6.current_amount_rub || 0).toLocaleString('ru-RU') + ' ₽ из ' + (bf6.target_amount_rub || 1600).toLocaleString('ru-RU') + ' ₽';
                        }
                    }
                    if (bf6PctEl) {
                        bf6PctEl.textContent = pct + '%';
                    }
                    if (bf6Card) {
                        if (bf6.is_completed) {
                            bf6Card.classList.add('completed');
                        } else {
                            bf6Card.classList.remove('completed');
                        }
                    }
                }
                return;
            }
        }
    } catch (err) {
        console.warn('Failed to load server community goals:', err);
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
    const usedCallsigns = new Set();

    displayedSponsors.forEach((s) => {
        const originalIdx = cachedSponsors ? cachedSponsors.indexOf(s) : 0;
        const sponsorKey = s.account_number || s.nickname || ('idx-' + originalIdx);
        currentKeys.add(sponsorKey);

        const cleanNick = (window.NobelCallsigns && window.NobelCallsigns.sanitizeNickname)
            ? window.NobelCallsigns.sanitizeNickname(s.nickname, s.account_number, usedCallsigns)
            : (s.nickname && !s.nickname.includes('****') ? s.nickname : 'Оператор 101');
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
