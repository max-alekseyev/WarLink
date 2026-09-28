// ui/js/account.js - User Account, Profile & Donation Handling

let cachedAccountProfile = null;
let currentDonateAmount = 100;

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

function closeAccount() {
    switchView(null);
}

async function fetchAccountProfile() {
    const list = document.getElementById('account-donations-list');
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
        resetDevBtn.style.display = (p.device_count > 1) ? 'inline-block' : 'none';
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

    let html = '';
    for (const d of donations) {
        const isPaid = d.status === 'paid';
        const statusClass = isPaid ? 'status-paid' : 'status-pending';
        const statusText = isPaid ? 'Зачислено' : 'Ожидает оплаты';
        html += `
            <div class="donation-row">
                <div class="donation-left">
                    <span class="donation-date font-mono">${escapeHtml(formatDonationDateTime(d.created_at))}</span>
                    <span class="donation-amount font-mono">${d.amount_rub} ₽</span>
                </div>
                <span class="donation-status ${statusClass}">${statusText}</span>
            </div>
        `;
    }
    list.innerHTML = html;
}

// --- Custom SBP Donation Modal ---
function openDonateModal(e) {
    if (e) e.stopPropagation();
    const modal = document.getElementById('modal-donate-custom');
    if (modal) {
        modal.style.display = 'flex';
        selectDonatePreset(100);
    }
}

function closeDonateModal() {
    const modal = document.getElementById('modal-donate-custom');
    if (modal) modal.style.display = 'none';
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
    const amt = currentDonateAmount || 100;
    try {
        await fetch('/api/server-donate', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ amount_rub: amt })
        });
        closeDonateModal();
        showToast(`Создан счет на ${amt} ₽. Страница оплаты открыта в браузере.`);
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
            fetchStatus();
            fetchAccountProfile();
        } else {
            showToast(data.error || 'Ошибка привязки аккаунта');
        }
    } catch (e) {
        console.error('Link account error:', e);
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
            fetchAccountProfile();
        } else {
            showToast(data.error || 'Ошибка сброса устройств');
        }
    } catch (e) {
        console.error('Reset devices error:', e);
        showToast('Ошибка сети при сбросе устройств');
    }
}
