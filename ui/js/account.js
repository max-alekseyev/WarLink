function loadCachedAccountProfile() {
    try {
        const raw = localStorage.getItem('warlink_account_profile');
        if (raw) return JSON.parse(raw);
    } catch (e) {}
    return null;
}

let cachedAccountProfile = loadCachedAccountProfile();
let currentDonateAmount = 100;

window.initAccountCache = function() {
    if (cachedAccountProfile) {
        renderAccountData(cachedAccountProfile);
        if (cachedAccountProfile.avatar_url) {
            updateAvatarDisplays(cachedAccountProfile.avatar_url);
        }
    }
};
if (cachedAccountProfile) {
    window.initAccountCache();
}

function getDonationsSkeletonHtml(count = 2) {
    let html = '';
    for (let i = 0; i < count; i++) {
        html += `
            <div class="donation-row skeleton-card">
                <div class="donation-left" style="gap: 8px;">
                    <div class="skeleton" style="width: 70px; height: 11px;"></div>
                    <div class="skeleton" style="width: 45px; height: 11px;"></div>
                </div>
                <div class="skeleton" style="width: 62px; height: 16px; border-radius: 2px;"></div>
            </div>
        `;
    }
    return html;
}

function toggleAccount(e) {
    if (e) e.stopPropagation();
    switchView('view-account');
}

async function fetchAccountProfile() {
    const list = document.getElementById('account-donations-list');

    if (typeof UIStore !== 'undefined' && typeof UIStore.requestSWR === 'function') {
        await UIStore.requestSWR(
            '/api/user-profile',
            async () => {
                const resp = await fetch('/api/user-profile');
                if (!resp.ok) return null;
                return await resp.json();
            },
            (p, isFresh) => {
                if (!p) return;
                cachedAccountProfile = p;
                renderAccountData(p);
            },
            () => {
                if (list && list.children.length === 0) {
                    list.innerHTML = getDonationsSkeletonHtml(2);
                }
            }
        );
        return;
    }

    if (cachedAccountProfile !== null) {
        renderAccountData(cachedAccountProfile);
    } else if (list && (list.children.length === 0 || !list.querySelector('.donation-row'))) {
        list.innerHTML = getDonationsSkeletonHtml(2);
    }
    try {
        const resp = await fetch('/api/user-profile');
        if (!resp.ok) return;
        const p = await resp.json();
        cachedAccountProfile = p;
        renderAccountData(p);
    } catch (e) {
        console.error('Error fetching account profile:', e);
    }
}

function renderAccountData(p) {
    if (!p) return;
    cachedAccountProfile = p;
    try {
        localStorage.setItem('warlink_account_profile', JSON.stringify(p));
        if (p.account_number) {
            localStorage.setItem('warlink_account_number', p.account_number);
        }
    } catch (e) {}
    const accNumEl = document.getElementById('val-account-number');
    if (accNumEl && p.account_number) {
        accNumEl.textContent = p.account_number;
    }

    const nickInput = document.getElementById('input-nickname');
    if (nickInput && document.activeElement !== nickInput && p.nickname) {
        nickInput.value = p.nickname;
    }

    const createdEl = document.getElementById('val-account-created');
    if (createdEl) {
        createdEl.textContent = formatAccountDate(p.created_at);
    }

    const devicesEl = document.getElementById('val-account-devices');
    if (devicesEl) {
        const count = p.device_count || 1;
        devicesEl.textContent = `${count} ПК`;
    }
    const resetDevBtn = document.getElementById('btn-reset-devices');
    if (resetDevBtn) {
        resetDevBtn.style.display = 'inline-block';
    }

    const steamInput = document.getElementById('input-dossier-steam');
    if (steamInput && document.activeElement !== steamInput) {
        steamInput.value = p.steam_id || '';
    }

    const mottoInput = document.getElementById('input-dossier-motto');
    if (mottoInput && document.activeElement !== mottoInput) {
        mottoInput.value = p.motto || '';
    }

    const hideAmountCheck = document.getElementById('check-dossier-hide-amount');
    if (hideAmountCheck) {
        hideAmountCheck.checked = Boolean(p.hide_donation_amount);
    }

    const sponsorUntilEl = document.getElementById('val-account-sponsor-until');
    if (sponsorUntilEl) {
        if (p.is_admin || p.account_tier === 'admin') {
            sponsorUntilEl.textContent = 'Администратор (Бессрочно)';
            sponsorUntilEl.style.color = 'var(--action)';
        } else if (p.is_sponsor && p.days_remaining > 0) {
            sponsorUntilEl.textContent = `Активен (осталось ${p.days_remaining} дн.)`;
            sponsorUntilEl.style.color = '#10b981';
        } else {
            sponsorUntilEl.textContent = 'Базовый доступ';
            sponsorUntilEl.style.color = 'var(--text-muted)';
        }
    }

    const totalDonatedEl = document.getElementById('val-account-total-donated');
    if (totalDonatedEl) {
        totalDonatedEl.textContent = (p.total_donated_rub || 0) + ' ₽';
    }

    updateAvatarDisplays(p.avatar_url);

    const tierBadge = document.getElementById('profile-tier-badge');
    if (tierBadge) {
        if (p.account_tier === 'admin' || p.is_admin) {
            tierBadge.textContent = 'Статус: Администратор (Админ-слот 61/61)';
            tierBadge.classList.remove('tier-sponsor');
            tierBadge.classList.add('tier-admin');
        } else if (p.is_sponsor) {
            tierBadge.textContent = 'Статус: Спонсор шлюза';
            tierBadge.classList.remove('tier-admin');
            tierBadge.classList.add('tier-sponsor');
        } else {
            tierBadge.textContent = 'Статус: Базовый доступ';
            tierBadge.classList.remove('tier-sponsor', 'tier-admin');
        }
    }

    // Hero Card Identity updates
    const heroNick = document.getElementById('hero-display-nickname');
    if (heroNick) {
        heroNick.textContent = p.nickname || 'Боец WarLink';
    }

    const heroTier = document.getElementById('hero-tier-tag');
    if (heroTier) {
        if (p.account_tier === 'admin' || p.is_admin) {
            heroTier.textContent = 'Администратор';
            heroTier.className = 'profile-hero-badge tier-admin';
        } else if (p.is_sponsor) {
            heroTier.textContent = 'Спонсор WarLink';
            heroTier.className = 'profile-hero-badge tier-sponsor';
        } else {
            heroTier.textContent = 'Боец WarLink';
            heroTier.className = 'profile-hero-badge';
        }
    }

    // Discord Status Synchronization
    const unlinkedCard = document.getElementById('discord-card-unlinked');
    const linkedCard = document.getElementById('discord-card-linked');
    const discordTagEl = document.getElementById('discord-linked-tag');
    const discordDot = document.getElementById('tab-discord-dot');
    const sponsorRolePill = document.getElementById('role-pill-sponsor');

    if (p.is_discord_linked || (p.discord_tag && p.discord_tag !== '')) {
        if (unlinkedCard) unlinkedCard.style.display = 'none';
        if (linkedCard) linkedCard.style.display = 'block';
        if (discordTagEl) {
            const cleanTag = p.discord_tag.startsWith('@') ? p.discord_tag : '@' + p.discord_tag;
            discordTagEl.textContent = cleanTag;
        }
        if (discordDot) discordDot.style.display = 'inline-block';
        if (sponsorRolePill) {
            sponsorRolePill.style.display = p.is_sponsor ? 'inline-block' : 'none';
        }
    } else {
        if (unlinkedCard) unlinkedCard.style.display = 'block';
        if (linkedCard) linkedCard.style.display = 'none';
        if (discordDot) discordDot.style.display = 'none';
    }

    renderDonations(p.donations);
}

function updateAvatarDisplays(avatarUrl) {
    const headerImg = document.getElementById('header-avatar-img');
    const headerSvg = document.getElementById('header-avatar-svg');
    const profImg = document.getElementById('profile-avatar-img');
    const profPlaceholder = document.getElementById('profile-avatar-placeholder');

    if (avatarUrl) {
        if (headerImg) {
            headerImg.src = avatarUrl;
            headerImg.style.display = 'block';
        }
        if (headerSvg) headerSvg.style.display = 'none';

        if (profImg) {
            profImg.src = avatarUrl;
            profImg.style.display = 'block';
        }
        if (profPlaceholder) profPlaceholder.style.display = 'none';
    } else {
        if (headerImg) headerImg.style.display = 'none';
        if (headerSvg) headerSvg.style.display = 'block';

        if (profImg) profImg.style.display = 'none';
        if (profPlaceholder) profPlaceholder.style.display = 'flex';
    }
}

function formatAccountDate(val) {
    if (!val || val === '—') return '—';
    try {
        if (typeof val === 'string') {
            if (/^\d{2}\.\d{2}\.\d{4}$/.test(val)) return val;
            if (val.includes('-')) return val.slice(0, 10).split('-').reverse().join('.');
        }
        const d = new Date(val);
        if (isNaN(d.getTime())) return val || '—';
        const day = String(d.getDate()).padStart(2, '0');
        const month = String(d.getMonth() + 1).padStart(2, '0');
        const year = d.getFullYear();
        return `${day}.${month}.${year}`;
    } catch (e) {
        return val || '—';
    }
}

function formatDonationDateTime(val) {
    if (!val || val === '—') return '—';
    try {
        const d = new Date(val);
        if (isNaN(d.getTime())) return val;
        const day = String(d.getDate()).padStart(2, '0');
        const month = String(d.getMonth() + 1).padStart(2, '0');
        const year = d.getFullYear();
        const hours = String(d.getHours()).padStart(2, '0');
        const mins = String(d.getMinutes()).padStart(2, '0');
        return `${day}.${month}.${year}, ${hours}:${mins}`;
    } catch (e) {
        return val;
    }
}

function renderDonations(donations) {
    const list = document.getElementById('account-donations-list');
    if (!list) return;

    if (!donations || donations.length === 0) {
        list.innerHTML = '<div class="account-donations-empty">История пополнений пуста. Поддержите сервер, чтобы стать спонсором и получить выделенный слот.</div>';
        return;
    }

    // Keyed reconciliation: preserve existing DOM elements if present
    const existingRows = new Map();
    Array.from(list.querySelectorAll('.donation-row[data-donation-id]')).forEach(el => {
        existingRows.set(el.dataset.donationId, el);
    });

    if (existingRows.size === 0 && (list.querySelector('.skeleton-card') || list.querySelector('.account-donations-empty'))) {
        list.innerHTML = '';
    }

    const currentIds = new Set();

    donations.forEach(d => {
        const rowId = d.id ? String(d.id) : `${d.created_at}_${d.amount_rub}`;
        currentIds.add(rowId);

        const isPaid = d.status === 'paid';
        const statusClass = isPaid ? 'status-paid' : 'status-pending';
        const statusText = isPaid ? 'Зачислено' : 'Ожидает оплаты';
        const dateText = formatDonationDateTime(d.created_at);
        const amountText = `${d.amount_rub} ₽`;

        const existing = existingRows.get(rowId);
        if (existing) {
            const dateEl = existing.querySelector('.donation-date');
            if (dateEl && dateEl.textContent !== dateText) dateEl.textContent = dateText;

            const amtEl = existing.querySelector('.donation-amount');
            if (amtEl && amtEl.textContent !== amountText) amtEl.textContent = amountText;

            const statusEl = existing.querySelector('.donation-status');
            if (statusEl) {
                statusEl.className = `donation-status ${statusClass}`;
                statusEl.textContent = statusText;
            }
            list.appendChild(existing);
        } else {
            const row = document.createElement('div');
            row.className = 'donation-row';
            row.dataset.donationId = rowId;
            row.innerHTML = `
                <div class="donation-left">
                    <span class="donation-date font-mono">${escapeHtml(dateText)}</span>
                    <span class="donation-amount font-mono">${amountText}</span>
                </div>
                <span class="donation-status ${statusClass}">${statusText}</span>
            `;
            list.appendChild(row);
        }
    });

    existingRows.forEach((el, id) => {
        if (!currentIds.has(id) && el.parentNode === list) {
            list.removeChild(el);
        }
    });
}

// --- Custom Donation Modal (SBP, Bank Cards, Boosty) ---
let currentDonateMethod = 'sbp';
let currentDonateTab = 'server';

function openExternal(url) {
    if (!url) return;
    fetch('/api/open-url?url=' + encodeURIComponent(url)).catch(() => {
        window.open(url, '_blank');
    });
}

function openBoostyLink(url) {
    if (!url) return;
    openExternal(url);
    showToast('Страница Boosty открыта в браузере');
}

function openSteamLink(url) {
    if (!url) return;
    let target = url;
    if (url.startsWith('https://steamcommunity.com') || url.startsWith('http://steamcommunity.com')) {
        target = 'steam://openurl/' + url;
    }
    openExternal(target);
    showToast('Профиль Steam открывается в клиенте');
}

window.openExternal = openExternal;
window.openExternalUrl = openExternal;
window.openBoostyLink = openBoostyLink;
window.openSteamLink = openSteamLink;

async function loadBoostyGoal() {
    try {
        const res = await fetch('/api/community-goal');
        if (!res.ok) return;
        const data = await res.json();
        if (data && data.success) {
            // 1. Author boosty goal
            const author = data.author_boosty || {};
            const current = author.current_amount || 0;
            const target = author.target_amount || 100000;
            const pct = Math.min(100, Math.max(0, (current / target) * 100));

            const titleEl = document.getElementById('boosty-goal-title');
            if (titleEl && author.title) titleEl.textContent = author.title;

            const targetEl = document.getElementById('boosty-goal-target');
            if (targetEl) targetEl.textContent = target.toLocaleString('ru-RU') + ' ₽';

            const fillEl = document.getElementById('boosty-goal-fill');
            if (fillEl) fillEl.style.width = Math.max(pct > 0 ? 2 : 0, pct) + '%';

            const metaEl = document.getElementById('boosty-goal-text');
            if (metaEl) {
                metaEl.textContent = `Собрано: ${current.toLocaleString('ru-RU')} ₽ из ${target.toLocaleString('ru-RU')} ₽ (${pct.toFixed(1)}%)`;
            }

            // 2. Battlefield 6 special project goal
            if (data.special_projects && data.special_projects.length > 0) {
                const bf6 = data.special_projects[0];
                const bf6Target = bf6.target_amount_rub || 1600;
                const bf6Current = bf6.current_amount_rub || 0;
                const bf6Pct = Math.min(100, Math.max(0, bf6.percent || 0));

                const bf6TargetEl = document.getElementById('donate-bf6-target');
                if (bf6TargetEl) bf6TargetEl.textContent = bf6Target.toLocaleString('ru-RU') + ' ₽';

                const bf6FillEl = document.getElementById('donate-bf6-fill');
                if (bf6FillEl) bf6FillEl.style.width = Math.max(bf6Pct > 0 ? 2 : 0, bf6Pct) + '%';

                const bf6TextEl = document.getElementById('donate-bf6-text');
                if (bf6TextEl) {
                    if (bf6.is_completed) {
                        bf6TextEl.textContent = 'Цель достигнута! Игра получена разработчиком';
                    } else {
                        bf6TextEl.textContent = `Собрано: ${bf6Current.toLocaleString('ru-RU')} ₽ из ${bf6Target.toLocaleString('ru-RU')} ₽ (${bf6Pct.toFixed(1)}%)`;
                    }
                }
            }
        }
    } catch(e) {
        console.error('loadBoostyGoal error:', e);
    }
}

function openDonateModal(e) {
    if (e) e.stopPropagation();
    openDonateModalWithTab('boosty');
}

function openDonateModalWithTab(tab, e) {
    if (e) e.stopPropagation();
    if (window.isDonateEnabled === false) {
        if (typeof showToast === 'function') {
            showToast('Прием пожертвований временно приостановлен');
        }
        return;
    }
    const modal = document.getElementById('modal-donate-custom');
    if (modal) {
        modal.style.display = 'flex';
        const modalBody = document.querySelector('.donate-modal-card .modal-body');
        if (modalBody) {
            modalBody.scrollTop = 0;
        }
        applyDonateMethodsVisibility();
        switchDonateTab(tab || 'boosty');
        loadBoostyGoal();
        if (tab === 'server') {
            selectDonateMethod('sbp');
            selectDonatePreset(100);
        }
    }
}

window.donateMethodsConfig = window.donateMethodsConfig || {
    boosty: true,
    sbp: false, // Boosty is prioritised by default per instructions
    crypto: false
};

function updateDonateMethodsState(cfg) {
    if (!cfg) return;
    if (typeof cfg.donate_boosty_enabled === 'boolean') window.donateMethodsConfig.boosty = cfg.donate_boosty_enabled;
    if (typeof cfg.donate_sbp_enabled === 'boolean') window.donateMethodsConfig.sbp = cfg.donate_sbp_enabled;
    if (typeof cfg.donate_crypto_enabled === 'boolean') window.donateMethodsConfig.crypto = cfg.donate_crypto_enabled;
    if (typeof cfg.donate_paused_notice === 'string') window.donateMethodsConfig.notice = cfg.donate_paused_notice;
    applyDonateMethodsVisibility();
}

function applyDonateMethodsVisibility() {
    const isBoostyOn = window.donateMethodsConfig.boosty !== false;
    const isSbpOn = window.donateMethodsConfig.sbp !== false;
    const isCryptoOn = window.donateMethodsConfig.crypto !== false;

    if (window.donateMethodsConfig.notice && window.donateMethodsConfig.notice.trim() !== '') {
        const descServer = document.querySelector('#donate-stub-server .donate-stub-desc');
        if (descServer) descServer.textContent = window.donateMethodsConfig.notice;
        const descCrypto = document.querySelector('#donate-stub-crypto .donate-stub-desc');
        if (descCrypto) descCrypto.textContent = window.donateMethodsConfig.notice;
    }

    // Server / SBP Tab
    const panelServerContent = document.getElementById('donate-panel-server-content');
    const stubServer = document.getElementById('donate-stub-server');
    if (panelServerContent && stubServer) {
        panelServerContent.style.display = isSbpOn ? 'block' : 'none';
        stubServer.style.display = isSbpOn ? 'none' : 'flex';
    }

    // Crypto Tab
    const panelCryptoContent = document.getElementById('donate-panel-crypto-content');
    const stubCrypto = document.getElementById('donate-stub-crypto');
    if (panelCryptoContent && stubCrypto) {
        panelCryptoContent.style.display = isCryptoOn ? 'flex' : 'none';
        stubCrypto.style.display = isCryptoOn ? 'none' : 'flex';
    }

    // Boosty Tab
    const panelBoostyContent = document.getElementById('donate-panel-boosty-content');
    const stubBoosty = document.getElementById('donate-stub-boosty');
    if (panelBoostyContent && stubBoosty) {
        panelBoostyContent.style.display = isBoostyOn ? 'flex' : 'none';
        stubBoosty.style.display = isBoostyOn ? 'none' : 'flex';
    }
}

function switchDonateTab(tab) {
    if (tab === 'crypto' || tab === 'usdt') {
        currentDonateTab = 'crypto';
    } else if (tab === 'server') {
        currentDonateTab = 'server';
    } else {
        currentDonateTab = 'boosty';
    }

    const tabServer = document.getElementById('tab-donate-server');
    const tabBoosty = document.getElementById('tab-donate-boosty');
    const tabCrypto = document.getElementById('tab-donate-crypto');
    const panelServer = document.getElementById('donate-panel-server');
    const panelBoosty = document.getElementById('donate-panel-boosty');
    const panelCrypto = document.getElementById('donate-panel-crypto');

    if (tabServer) {
        tabServer.classList.toggle('active', currentDonateTab === 'server');
        tabServer.classList.toggle('tab-server-active', currentDonateTab === 'server');
    }
    if (tabBoosty) {
        tabBoosty.classList.toggle('active', currentDonateTab === 'boosty');
        tabBoosty.classList.toggle('tab-boosty-active', currentDonateTab === 'boosty');
    }
    if (tabCrypto) {
        tabCrypto.classList.toggle('active', currentDonateTab === 'crypto');
        tabCrypto.classList.toggle('tab-crypto-active', currentDonateTab === 'crypto');
    }

    // Apply visibility of active forms vs priority stubs
    applyDonateMethodsVisibility();

    if (panelServer) panelServer.style.display = currentDonateTab === 'server' ? 'block' : 'none';
    if (panelBoosty) panelBoosty.style.display = currentDonateTab === 'boosty' ? 'flex' : 'none';
    if (panelCrypto) {
        panelCrypto.style.display = currentDonateTab === 'crypto' ? 'flex' : 'none';
        if (currentDonateTab === 'crypto' && window.donateMethodsConfig.crypto !== false) {
            renderCryptoQR();
        }
    }

    const modalBody = document.querySelector('.donate-modal-card .modal-body');
    if (modalBody) {
        modalBody.scrollTop = 0;
    }
}

const TRC20_USDT_ADDRESS = 'TE1hrh5yy7hnHpCX7fz51rPvWubpcJJgaM';

function renderCryptoQR() {
    const container = document.getElementById('crypto-qr-container');
    if (!container) return;
    if (container.dataset.rendered === 'true') return;

    try {
        if (typeof qrcode === 'function') {
            const qr = qrcode(0, 'M');
            qr.addData(TRC20_USDT_ADDRESS);
            qr.make();
            container.innerHTML = qr.createSvgTag(4, 2);
            container.dataset.rendered = 'true';
        }
    } catch (err) {
        console.error('Failed to generate crypto QR code:', err);
    }
}

function copyCryptoAddress() {
    const addr = TRC20_USDT_ADDRESS;
    const input = document.getElementById('input-crypto-addr');
    if (input) {
        input.select();
    }
    if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(addr).then(() => {
            showCryptoCopyFeedback();
        }).catch(() => {
            legacyCopyCrypto(addr);
        });
    } else {
        legacyCopyCrypto(addr);
    }
}

function showCryptoCopyFeedback() {
    const btn = document.getElementById('btn-copy-crypto-addr');
    if (btn) {
        const originalHTML = btn.innerHTML;
        btn.innerHTML = `
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="#22c55e" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
            <span style="color: #22c55e;">Скопировано!</span>
        `;
        setTimeout(() => {
            btn.innerHTML = originalHTML;
        }, 2200);
    }
    if (typeof showToast === 'function') {
        showToast('Адрес кошелька TRC20 скопирован в буфер обмена');
    }
}

function legacyCopyCrypto(text) {
    const input = document.getElementById('input-crypto-addr');
    if (input) {
        input.select();
        document.execCommand('copy');
        showCryptoCopyFeedback();
    }
}

function closeDonateModal() {
    const modal = document.getElementById('modal-donate-custom');
    if (modal) modal.style.display = 'none';
}

function selectDonateMethod(method) {
    currentDonateMethod = (method === 'card') ? 'card' : 'sbp';
    document.querySelectorAll('.method-chip').forEach(btn => {
        btn.classList.remove('active');
    });
    const chipSbp = document.getElementById('chip-method-sbp');
    const chipCard = document.getElementById('chip-method-card');

    if (currentDonateMethod === 'card' && chipCard) {
        chipCard.classList.add('active');
    } else if (chipSbp) {
        chipSbp.classList.add('active');
    }

    const hintEl = document.getElementById('donate-method-hint');
    if (hintEl) {
        if (currentDonateMethod === 'card') {
            hintEl.textContent = 'Банковские карты МИР, Visa, Mastercard РФ (комиссия 5%)';
        } else {
            hintEl.textContent = 'Комиссия 1% • Моментальная оплата по QR-коду СБП через любое банковское приложение';
        }
    }
}

function selectDonatePreset(amount) {
    currentDonateAmount = amount;
    const input = document.getElementById('input-donate-custom');
    if (input) input.value = amount;

    document.querySelectorAll('.preset-chip').forEach(btn => {
        const val = parseInt(btn.textContent);
        btn.classList.toggle('active', val === amount);
    });
    updateDonateCalc(amount);
}

function onCustomDonateInput(val) {
    let amt = parseInt(val) || 100;
    if (amt < 100) amt = 100;
    currentDonateAmount = amt;

    document.querySelectorAll('.preset-chip').forEach(btn => {
        const bVal = parseInt(btn.textContent);
        btn.classList.toggle('active', bVal === amt);
    });
    updateDonateCalc(amt);
}

function updateDonateCalc(amt) {
    const calcEl = document.getElementById('donate-calc-summary');
    if (calcEl) {
        const days = Math.floor(amt / 100) * 30;
        calcEl.textContent = `Статус «Спонсор» на ${days} дн.`;
    }
}

async function submitCustomDonation() {
    const amt = Math.max(100, currentDonateAmount || 100);
    const method = currentDonateMethod === 'card' ? 'card' : 'sbp';
    try {
        await fetch('/api/server-donate', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ amount_rub: amt, payment_method: method })
        });
        closeDonateModal();
        const methodLabel = method === 'card' ? 'банковской картой' : 'СБП';
        showToast(`Создан счет на ${amt} ₽ (${methodLabel}). Страница оплаты открыта в браузере.`);
    } catch (e) {
        console.error('Submit donation error:', e);
    }
}

// --- Account & Profile Actions ---
function copyAccountNumber() {
    const accEl = document.getElementById('val-account-number');
    if (!accEl) return;
    const text = accEl.textContent.trim();
    if (text && text !== '—') {
        navigator.clipboard.writeText(text).then(() => {
            showToast('16-значный номер аккаунта скопирован в буфер обмена');
        }).catch(() => {
            showToast('Номер: ' + text);
        });
    }
}

function validateNickname(nick) {
    const trimmed = nick.trim();
    if (!trimmed) return null;
    if (trimmed.length < 2 || trimmed.length > 20) {
        return 'Длина никнейма должна быть от 2 до 20 символов';
    }
    const regex = /^[a-zA-Zа-яА-ЯёЁ0-9_\-\s]+$/;
    if (!regex.test(trimmed)) {
        return 'Никнейм может содержать только буквы, цифры, дефис и подчеркивание';
    }
    if (!/[a-zA-Zа-яА-ЯёЁ0-9]/.test(trimmed)) {
        return 'Никнейм должен содержать буквы или цифры';
    }

    // Check mixed script spoofing (Latin + Cyrillic in a single word)
    const words = trimmed.split(/[\s\-_.]+/);
    for (const w of words) {
        const hasLat = /[a-zA-Z]/.test(w);
        const hasCyr = /[а-яА-ЯёЁ]/.test(w);
        if (hasLat && hasCyr) {
            return 'Смешивание латиницы и кириллицы в одном слове запрещено';
        }
    }

    // Nazi numeric codes
    if (/(?:^|[^0-9])14[_\-\s/]?88(?:[^0-9]|$)/i.test(trimmed) ||
        /(?:^|[^0-9])88[_\-\s/]?14(?:[^0-9]|$)/i.test(trimmed) ||
        /(?:^|[^a-z0-9])(?:c18|combat18)(?:[^a-z0-9]|$)/i.test(trimmed)) {
        return 'Никнейм содержит экстремистские или запрещенные числовые коды';
    }

    // Toxic gaming mother insults
    if (/(?:^|[^a-z0-9])(?:mq|mky|m-q|m_q)(?:[^a-z0-9]|$)/i.test(trimmed)) {
        return 'Никнейм содержит токсичные игровые оскорбления';
    }

    // Safe words whitelist (Scunthorpe problem prevention)
    const safeWhitelist = [
        'колебан', 'загребать', 'огребать', 'хлеб', 'стебель', 'грабеж',
        'употреблять', 'оскорблять', 'влюбляться', 'сабля', 'рубль', 'рубля', 'гребля',
        'ссуда', 'пассат', 'скипидар', 'застраховать', 'художник', 'худой', 'хутор',
        'педикюр', 'википедия', 'ортопедик', 'барсук', 'посукно', 'мудрый', 'мудрец',
        'мудрость', 'замудреный', 'изумруд', 'парикмахер', 'парикмахерская', 'чмоканье',
        'пассаж', 'касса', 'трасса', 'масса', 'класс', 'колосс', 'сша', 'высший',
        'classic', 'assassin', 'cocktail', 'cockpit', 'dickens', 'tombstone', 'basement',
        'therapist', 'pushing', 'butter', 'title', 'titov', 'document', 'button'
    ];
    for (const safe of safeWhitelist) {
        if (trimmed.toLowerCase().includes(safe)) return null;
    }

    const lower = trimmed.toLowerCase().replace(/[\s\-_.,]/g, '')
        .replace(/0/g, 'o').replace(/1/g, 'i').replace(/3/g, 'e').replace(/4/g, 'a').replace(/@/g, 'a').replace(/\$/g, 's');

    const cyrToLat = lower.replace(/а/g, 'a').replace(/в/g, 'b').replace(/е/g, 'e').replace(/к/g, 'k')
        .replace(/м/g, 'm').replace(/н/g, 'h').replace(/о/g, 'o').replace(/р/g, 'p').replace(/с/g, 'c')
        .replace(/т/g, 't').replace(/у/g, 'y').replace(/х/g, 'x');

    const latToCyr = lower.replace(/a/g, 'а').replace(/b/g, 'в').replace(/e/g, 'е').replace(/k/g, 'к')
        .replace(/m/g, 'м').replace(/h/g, 'н').replace(/o/g, 'о').replace(/p/g, 'р').replace(/c/g, 'с')
        .replace(/t/g, 'т').replace(/y/g, 'у').replace(/x/g, 'х');

    const collapsed = lower.replace(/(.)\1+/g, '$1');
    const collapsedCyrToLat = cyrToLat.replace(/(.)\1+/g, '$1');
    const collapsedLatToCyr = latToCyr.replace(/(.)\1+/g, '$1');
    const collapsedLatToCyrLower = latToCyr.replace(/(.)\1+/g, '$1');

    const variants = [lower, cyrToLat, latToCyr, collapsed, collapsedCyrToLat, collapsedLatToCyrLower];

    const impersonation = [
        'admin', 'administrator', 'админ', 'администратор',
        'warlink', 'варлинк', 'support', 'саппорт', 'техподдержка',
        'moderator', 'модератор', 'root', 'system', 'систем',
        'developer', 'разработчик', 'owner', 'владелец',
        'official', 'официальный', 'security', 'безопасность',
        'aeza', 'аеза', 'hysteria', 'singbox'
    ];
    for (const v of variants) {
        for (const imp of impersonation) {
            if (v.includes(imp)) {
                return 'Этот никнейм зарезервирован администрацией WarLink';
            }
        }
    }

    const profanities = [
        'хуй', 'хуе', 'хуя', 'хули', 'пизд', 'ебат', 'ебан', 'ебли', 'ебл',
        'бляд', 'блят', 'сука', 'сучк', 'мудак', 'мудил', 'гандон', 'гондон',
        'шлюх', 'чмо', 'залуп', 'говно', 'гавно', 'пидор', 'пидар', 'педик',
        'уеб', 'жопа', 'дроч',
        'hui', 'xui', 'xyu', 'pizd', 'ebat', 'eban', 'ebal', 'blyad', 'blyat',
        'suka', 'mudak', 'gandon', 'gavno', 'govno', 'pidor', 'pedik', 'zalup',
        'fuck', 'shit', 'bitch', 'cunt', 'dick', 'asshole',
        'nigger', 'nigga', 'niger', 'niga', 'n1gger', 'n1gga', 'whore', 'bastard', 'cock', 'fag', 'slut'
    ];
    for (const v of variants) {
        for (const prof of profanities) {
            if (v.includes(prof)) {
                return 'Никнейм содержит недопустимые или нецензурные выражения';
            }
        }
    }
    return null;
}

async function saveNickname() {
    const input = document.getElementById('input-nickname');
    if (!input) return;
    const nick = input.value.trim();
    const valErr = validateNickname(nick);
    if (valErr) {
        showToast(valErr);
        return;
    }
    const btn = document.getElementById('btn-save-nickname');
    if (btn) {
        btn.disabled = true;
        btn.textContent = 'Проверка...';
    }
    try {
        const resp = await fetch('/api/user-profile', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ nickname: nick })
        });
        const data = await resp.json();
        if (resp.ok && data.success) {
            showToast('Никнейм успешно проверен и сохранен');
            if (typeof UIStore !== 'undefined') {
                UIStore.invalidate('/api/user-profile');
                UIStore.invalidate('/api/sponsors');
            }
            fetchStatus();
            fetchAccountProfile();
        } else {
            showToast(data.message || data.error || 'Ошибка сохранения никнейма');
        }
    } catch (e) {
        console.error('Save nickname error:', e);
        showToast('Ошибка сети при сохранении никнейма');
    } finally {
        if (btn) {
            btn.disabled = false;
            btn.textContent = 'Сохранить';
        }
    }
}

async function linkAccountNumber() {
    const input = document.getElementById('input-link-account');
    if (!input) return;
    const acc = input.value.trim();
    if (!acc) return;
    try {
        const resp = await fetch('/api/user-profile/link', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ account_number: acc })
        });
        const data = await resp.json();
        if (resp.ok && data.success) {
            showToast('Аккаунт успешно привязан к этому ПК');
            input.value = '';
            if (typeof UIStore !== 'undefined') {
                UIStore.invalidate('/api/user-profile');
                UIStore.invalidate('/api/sponsors');
            }
            fetchStatus();
            fetchAccountProfile();
        } else {
            showToast(data.error || 'Ошибка привязки аккаунта');
        }
    } catch (e) {
        console.error('Link account error:', e);
    }
}

function switchAccountTab(tabKey) {
    const tabs = ['profile', 'discord', 'security', 'billing'];
    tabs.forEach(t => {
        const btn = document.getElementById(`tab-btn-${t}`);
        const panel = document.getElementById(`acc-panel-${t}`);
        if (btn) btn.classList.toggle('active', t === tabKey);
        if (panel) {
            panel.style.display = (t === tabKey) ? 'block' : 'none';
            panel.classList.toggle('active', t === tabKey);
        }
    });
}

async function requestDiscordLinkCode() {
    const btn = document.getElementById('btn-discord-link-code');
    const resBox = document.getElementById('discord-link-code-result');
    const codeVal = document.getElementById('val-discord-code');
    const expVal = document.getElementById('val-discord-expires');
    if (!btn || !resBox || !codeVal) return;

    btn.disabled = true;
    btn.textContent = 'Генерация...';
    try {
        const resp = await fetch('/api/discord-link-code');
        const data = await resp.json();
        if (resp.ok && data.success && data.code) {
            codeVal.textContent = data.code;
            const mins = Math.max(1, Math.round((data.expires_in || 900) / 60));
            if (expVal) expVal.textContent = `(${mins} мин)`;
            resBox.style.display = 'block';
            btn.textContent = 'Обновить код';

            // Автоматическое копирование кода в буфер обмена
            if (navigator.clipboard) {
                try {
                    await navigator.clipboard.writeText(data.code);
                    showToast('Код ' + data.code + ' скопирован в буфер обмена');
                } catch (clipErr) {
                    showToast('Код получен: ' + data.code);
                }
            } else {
                showToast('Код получен: ' + data.code);
            }
        } else {
            showToast(data.error || 'Ошибка получения кода');
            btn.textContent = 'Получить код для Discord';
        }
    } catch (e) {
        console.error('Discord link code error:', e);
        showToast('Ошибка связи с сервером');
        btn.textContent = 'Получить код для Discord';
    } finally {
        btn.disabled = false;
    }
}

function copyDiscordCode() {
    const codeVal = document.getElementById('val-discord-code');
    if (!codeVal) return;
    const code = codeVal.textContent.trim();
    if (code && code !== '------') {
        if (navigator.clipboard) {
            navigator.clipboard.writeText(code).then(() => {
                showToast('Код ' + code + ' скопирован в буфер обмена');
            }).catch(() => {
                showToast('Код: ' + code);
            });
        } else {
            showToast('Код: ' + code);
        }
    }
}

async function unlinkDiscordAccount() {
    if (!confirm('Вы действительно хотите отвязать ваш Discord-аккаунт?')) {
        return;
    }
    showToast('Отвязка Discord...');
    try {
        const resp = await fetch('/api/discord-unlink', { method: 'POST' });
        const data = await resp.json();
        if (resp.ok && data.success) {
            showToast('Discord успешно отвязан');
            if (typeof UIStore !== 'undefined') {
                UIStore.invalidate('/api/user-profile');
            }
            await fetchAccountProfile();
        } else {
            showToast(data.error || 'Ошибка при отвязке Discord');
        }
    } catch (e) {
        console.error('Unlink discord error:', e);
        showToast('Ошибка соединения');
    }
}

async function openDiscordCommunity() {
    try {
        await fetch('/api/open-discord');
    } catch (e) {
        window.open('https://discord.gg/2h8nVRUBeT', '_blank');
    }
}

function triggerAvatarUpload() {
    const fileInput = document.getElementById('input-avatar-file');
    if (fileInput) fileInput.click();
}

async function handleAvatarFileSelected(e) {
    const file = e.target.files && e.target.files[0];
    if (!file) return;

    const formData = new FormData();
    formData.append('avatar', file);

    showToast('Загрузка и оптимизация аватарки...');
    try {
        const resp = await fetch('/api/user-profile/avatar', {
            method: 'POST',
            body: formData
        });
        const data = await resp.json();
        if (resp.ok && data.success) {
            showToast('Аватарка успешно обновлена');
            if (typeof UIStore !== 'undefined') {
                UIStore.invalidate('/api/user-profile');
                UIStore.invalidate('/api/sponsors');
            }
            fetchStatus();
            fetchAccountProfile();
        } else {
            showToast(data.error || 'Ошибка загрузки аватарки');
        }
    } catch (err) {
        console.error('Upload avatar error:', err);
        showToast('Не удалось загрузить аватарку');
    }
}

async function toggleHideDonationAmount(hideVal) {
    try {
        const resp = await fetch('/api/user-profile', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                hide_donation_amount: hideVal
            })
        });
        const data = await resp.json();
        if (resp.ok && data.success) {
            showToast(hideVal ? 'Сумма взноса засекречена' : 'Сумма взноса открыта');
            if (typeof UIStore !== 'undefined') {
                UIStore.invalidate('/api/user-profile');
                UIStore.invalidate('/api/sponsors');
            }
            fetchAccountProfile();
        } else {
            showToast(data.message || data.error || 'Ошибка изменения статуса');
        }
    } catch (e) {
        console.error('Toggle hide amount error:', e);
        showToast('Ошибка сети при изменении статуса');
    }
}

async function saveDossierSettings() {
    const steamInput = document.getElementById('input-dossier-steam');
    const mottoInput = document.getElementById('input-dossier-motto');
    const hideCheck = document.getElementById('check-dossier-hide-amount');
    const steamVal = steamInput ? steamInput.value.trim() : '';
    const mottoVal = mottoInput ? mottoInput.value.trim() : '';
    const hideVal = hideCheck ? hideCheck.checked : false;

    const btn = document.getElementById('btn-save-dossier');
    if (btn) {
        btn.disabled = true;
        btn.textContent = 'Сохранение...';
    }
    try {
        const resp = await fetch('/api/user-profile', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                steam_id: steamVal,
                motto: mottoVal,
                hide_donation_amount: hideVal
            })
        });
        const data = await resp.json();
        if (resp.ok && data.success) {
            showToast('Описание досье сохранено');
            if (typeof UIStore !== 'undefined') {
                UIStore.invalidate('/api/user-profile');
                UIStore.invalidate('/api/sponsors');
            }
            fetchAccountProfile();
        } else {
            showToast(data.message || data.error || 'Ошибка сохранения настроек');
        }
    } catch (e) {
        console.error('Save dossier settings error:', e);
        showToast('Ошибка сети при сохранении досье');
    } finally {
        if (btn) {
            btn.disabled = false;
            btn.textContent = 'Сохранить описание';
        }
    }
}

async function resetOtherDevices() {
    try {
        const resp = await fetch('/api/user-profile/reset-devices', {
            method: 'POST'
        });
        const data = await resp.json();
        if (resp.ok && data.success) {
            showToast('Все лишние устройства отвязаны (активен 1 ПК)');
            if (typeof UIStore !== 'undefined') {
                UIStore.invalidate('/api/user-profile');
            }
            fetchAccountProfile();
        } else {
            showToast(data.error || 'Ошибка сброса устройств');
        }
    } catch (e) {
        console.error('Reset devices error:', e);
        showToast('Ошибка сети при сбросе устройств');
    }
}
