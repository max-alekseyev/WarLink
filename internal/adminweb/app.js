// WarLink Control Panel - Native Admin SPA Controller
// Dark Cloudflare Utility - Zero Emoji - Strictly DD.MM.YYYY

let currentUser = null;
let setupData = null;
let currentTicketId = null;
let draggedCardAppId = null;

// =============================================================================
// Robust Fetch JSON Helper (Guards against empty bodies, 204s, and non-JSON HTML)
// =============================================================================

async function fetchJSON(url, options = {}) {
    const res = await fetch(url, options);
    const text = await res.text();
    let data = null;
    if (text && text.trim().length > 0) {
        try {
            data = JSON.parse(text);
        } catch (e) {
            if (!res.ok) {
                throw new Error(`Ошибка сервера (${res.status}): ${text.slice(0, 100)}`);
            }
            throw new Error('Некорректный ответ сервера (не JSON)');
        }
    }
    if (!res.ok) {
        const errMsg = (data && (data.error || data.message)) || `HTTP ${res.status}`;
        throw new Error(errMsg);
    }
    return data || {};
}

// =============================================================================
// Initialization & Authentication Routing
// =============================================================================

document.addEventListener('DOMContentLoaded', async () => {
    handleHashNavigation();
    window.addEventListener('hashchange', handleHashNavigation);

    const urlParams = new URLSearchParams(window.location.search);
    const keyParam = urlParams.get('key');
    if (keyParam) {
        try {
            await submitLoginByKey(keyParam, false);
            return;
        } catch (e) {
            console.error('Key auto-login failed:', e);
        }
    }

    await checkAuthStatus();
});

function switchLoginMode(mode) {
    const tabKeyBtn = document.getElementById('tab-auth-key-btn');
    const tabCredBtn = document.getElementById('tab-auth-cred-btn');
    const stepKey = document.getElementById('login-step-key');
    const stepCred = document.getElementById('login-step-credentials');
    const stepOtp = document.getElementById('login-step-otp');

    if (tabKeyBtn) tabKeyBtn.classList.toggle('active', mode === 'key');
    if (tabCredBtn) tabCredBtn.classList.toggle('active', mode === 'cred');

    if (mode === 'key') {
        if (stepKey) stepKey.style.display = 'block';
        if (stepCred) stepCred.style.display = 'none';
        if (stepOtp) stepOtp.style.display = 'none';
    } else {
        if (stepKey) stepKey.style.display = 'none';
        if (stepCred) stepCred.style.display = 'block';
        if (stepOtp) stepOtp.style.display = 'none';
    }
}

async function submitLoginByKey(keyInput, showErrors = true) {
    let key = keyInput;
    if (!key) {
        const input = document.getElementById('login-dashboard-key');
        key = input ? input.value.trim() : '';
    }
    if (!key) {
        if (showErrors) showToast('Введите секретный мастер-ключ', 'error');
        return;
    }

    try {
        const data = await fetchJSON('/api/v1/admin/auth/key', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ key })
        });
        if (!data.success) throw new Error(data.error || 'Неверный секретный ключ');

        currentUser = data.username || 'admin';
        document.getElementById('header-admin-user').textContent = currentUser;
        showScreen('screen-dashboard');
        loadActiveTabData();
        showToast('Успешный вход в систему', 'success');
    } catch (err) {
        if (showErrors) showToast(err.message, 'error');
        throw err;
    }
}

async function checkAuthStatus() {
    try {
        const data = await fetchJSON('/api/v1/admin/auth/status');

        if (data.needs_setup) {
            showScreen('screen-setup');
            return;
        }

        if (data.authenticated) {
            currentUser = data.username || 'admin';
            document.getElementById('header-admin-user').textContent = currentUser;
            showScreen('screen-dashboard');
            loadActiveTabData();
        } else {
            showScreen('screen-login');
        }
    } catch (err) {
        console.error('Failed to check auth status:', err);
        showScreen('screen-login');
    }
}

function showScreen(screenId) {
    document.getElementById('screen-setup').style.display = (screenId === 'screen-setup') ? 'flex' : 'none';
    document.getElementById('screen-login').style.display = (screenId === 'screen-login') ? 'flex' : 'none';
    document.getElementById('screen-dashboard').style.display = (screenId === 'screen-dashboard') ? 'block' : 'none';
}

function handleHashNavigation() {
    let rawHash = window.location.hash.replace('#', '').trim();
    let queryParams = new URLSearchParams(window.location.search);

    // Also parse query params from hash if present (e.g. #view-monetization?account=1428-2283-7267-7319)
    if (rawHash.includes('?')) {
        const parts = rawHash.split('?');
        rawHash = parts[0];
        const hashParams = new URLSearchParams(parts[1]);
        hashParams.forEach((val, key) => queryParams.set(key, val));
    }

    let hash = rawHash;
    if (!hash) {
        if (window.location.hostname.includes('static-warlink') || window.location.pathname.includes('/static')) {
            hash = 'view-static';
        } else {
            hash = 'view-special-projects';
        }
    }

    // Handle legacy Grafana links
    if (hash.includes('support_tickets') || hash.includes('dialogs')) hash = 'view-support';
    if (hash.includes('accounts') || hash.includes('sponsors')) hash = 'view-monetization';
    if (hash.includes('nodes') || hash.includes('cluster')) hash = 'view-nodes';
    if (hash.includes('static') || hash.includes('cdn')) hash = 'view-static';

    const tabKey = hash.replace('view-', '');
    switchAdminTab(tabKey, false);

    const account = queryParams.get('account');
    if (account) {
        setTimeout(() => {
            openAccountCard(account);

            const grantAcc = document.getElementById('grant-account-number');
            if (grantAcc) grantAcc.value = account;

            const ticketsSearch = document.getElementById('tickets-search-input');
            if (ticketsSearch) ticketsSearch.value = account;

            const notifTarget = document.getElementById('notif-target-id');
            if (notifTarget) notifTarget.value = account;
        }, 150);
    }
}

function switchAdminTab(tabKey, updateHash = true) {
    const navItems = document.querySelectorAll('.nav-item');
    navItems.forEach(el => {
        el.classList.toggle('active', el.dataset.tab === tabKey);
    });

    const panes = document.querySelectorAll('.view-pane');
    panes.forEach(p => {
        p.classList.toggle('active', p.id === `view-${tabKey}`);
    });

    if (updateHash) {
        window.location.hash = `#view-${tabKey}`;
    }

    loadActiveTabData(tabKey);
}

function loadActiveTabData(tabKey) {
    if (!tabKey) {
        const activeNav = document.querySelector('.nav-item.active');
        tabKey = activeNav ? activeNav.dataset.tab : 'special-projects';
    }

    switch (tabKey) {
        case 'special-projects': loadSpecialProjects(); break;
        case 'nodes': loadClusterNodes(); break;
        case 'monetization': loadMonetizationSettings(); break;
        case 'notifications': loadNotifications(); break;
        case 'support': loadTickets(); break;
        case 'games-kanban': loadKanbanAndGold(); break;
        case 'static': loadStaticManager(); break;
        case 'progression': loadProgressionAdmin(); break;
        case 'system-ops': loadSystemOps(); break;
    }
}

// =============================================================================
// Setup Wizard (Первичный мастер настройки: Логин + Пароль + TOTP)
// =============================================================================

async function goToSetupStep2() {
    const user = document.getElementById('setup-username').value.trim();
    const pass = document.getElementById('setup-password').value;
    const confirm = document.getElementById('setup-password-confirm').value;

    if (!user) return showToast('Укажите логин администратора', 'error');
    if (!pass || pass.length < 8) return showToast('Пароль должен содержать не менее 8 символов', 'error');
    if (pass !== confirm) return showToast('Пароли не совпадают', 'error');

    try {
        const data = await fetchJSON('/api/v1/admin/auth/setup/init', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username: user })
        });
        if (!data.success) throw new Error(data.error || 'Ошибка инициализации 2FA');

        setupData = data;
        document.getElementById('setup-totp-secret').textContent = data.totp_secret;

        // Render QR SVG / PNG
        const qrContainer = document.getElementById('setup-qr-code');
        if (data.qr_svg) {
            qrContainer.innerHTML = data.qr_svg;
        } else if (data.qr_png) {
            qrContainer.innerHTML = `<img src="${data.qr_png}" style="width:100%;height:100%;display:block;" alt="QR Code">`;
        }

        // Render backup codes
        const bcContainer = document.getElementById('setup-backup-codes');
        bcContainer.innerHTML = (data.backup_codes || []).map(code => `
            <div class="backup-code-item">${code}</div>
        `).join('');

        document.getElementById('setup-step-1').style.display = 'none';
        document.getElementById('setup-step-2').style.display = 'block';
    } catch (err) {
        showToast(err.message, 'error');
    }
}

function backToSetupStep1() {
    document.getElementById('setup-step-2').style.display = 'none';
    document.getElementById('setup-step-1').style.display = 'block';
}

async function submitSetupWizard() {
    const user = document.getElementById('setup-username').value.trim();
    const pass = document.getElementById('setup-password').value;
    const code = document.getElementById('setup-otp-code').value.trim();

    if (!code || code.length !== 6) return showToast('Введите 6-значный проверочный код', 'error');

    try {
        const data = await fetchJSON('/api/v1/admin/auth/setup/verify', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                username: user,
                password: pass,
                totp_secret: setupData.totp_secret,
                backup_codes: setupData.backup_codes,
                code: code
            })
        });
        if (!data.success) throw new Error(data.error || 'Неверный код подтверждения');

        showToast('Аккаунт успешно создан и активирован', 'success');
        currentUser = user;
        document.getElementById('header-admin-user').textContent = currentUser;
        showScreen('screen-dashboard');
        loadActiveTabData();
    } catch (err) {
        showToast(err.message, 'error');
    }
}

// =============================================================================
// Login Screen (Двухфакторный вход)
// =============================================================================

function goToLoginOtp() {
    const user = document.getElementById('login-username').value.trim();
    const pass = document.getElementById('login-password').value;
    if (!user || !pass) return showToast('Укажите логин и пароль', 'error');

    document.getElementById('login-step-credentials').style.display = 'none';
    document.getElementById('login-step-otp').style.display = 'block';
    document.getElementById('login-otp-code').focus();
}

function backToLoginCredentials() {
    document.getElementById('login-step-otp').style.display = 'none';
    document.getElementById('login-step-credentials').style.display = 'block';
}

async function submitLogin() {
    const user = document.getElementById('login-username').value.trim();
    const pass = document.getElementById('login-password').value;
    const code = document.getElementById('login-otp-code').value.trim();

    if (!code) return showToast('Введите код подтверждения', 'error');

    try {
        const data = await fetchJSON('/api/v1/admin/auth/login', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username: user, password: pass, code: code })
        });
        if (!data.success) throw new Error(data.error || 'Ошибка авторизации');

        currentUser = user;
        document.getElementById('header-admin-user').textContent = currentUser;
        showScreen('screen-dashboard');
        loadActiveTabData();
        showToast('Авторизация успешна', 'success');
    } catch (err) {
        showToast(err.message, 'error');
    }
}

async function logoutAdmin() {
    try {
        await fetchJSON('/api/v1/admin/auth/logout', { method: 'POST' });
    } catch (e) {}
    currentUser = null;
    showScreen('screen-login');
    backToLoginCredentials();
    showToast('Вы вышли из системы', 'info');
}

// =============================================================================
// TAB 1: Спецпроекты и Сборы (special_projects)
// =============================================================================

async function loadSpecialProjects() {
    const grid = document.getElementById('special-projects-grid');
    if (!grid) return;
    grid.innerHTML = '<div class="text-secondary" style="font-size:12px;">Загрузка проектов...</div>';

    try {
        const data = await fetchJSON('/api/v1/admin/special-projects');
        const projects = (data && data.projects) ? data.projects : [];

        if (projects.length === 0) {
            grid.innerHTML = '<div class="text-secondary" style="font-size:12px; grid-column: 1 / -1;">Нет созданных спецпроектов. Создайте первый проект!</div>';
            return;
        }

        window.loadedProjects = projects;

        grid.innerHTML = projects.map(p => {
            const collected = (p.current_amount_rub != null) ? p.current_amount_rub : 0;
            const target = p.target_amount_rub || 1;
            const pct = Math.min(100, Math.round((collected / target) * 100));

            return `
                <div class="card">
                    <div style="display: flex; justify-content: space-between; align-items: start; margin-bottom: 8px;">
                        <div>
                            <div style="font-size: 14px; font-weight: 700; color: var(--text-primary);">${escapeHtml(p.title)}</div>
                            <div class="font-mono text-muted" style="font-size: 10px;">ID: ${escapeHtml(p.id)} • Бейдж: ${escapeHtml(p.badge || '—')} • Порядок: ${p.display_order || 1}</div>
                        </div>
                        <span class="badge ${p.is_active ? 'badge-success' : 'badge-neutral'}">
                            ${p.is_active ? 'Активен' : 'Отключен'}
                        </span>
                    </div>

                    ${p.description ? `<div style="font-size: 11px; color: var(--text-secondary); margin-bottom: 10px; line-height: 1.4;">${escapeHtml(p.description)}</div>` : ''}

                    <div style="display: flex; justify-content: space-between; font-size: 11px; margin-bottom: 4px;">
                        <span class="text-secondary">Собрано: <b style="color:var(--text-primary);">${collected.toLocaleString('ru-RU')} ₽</b></span>
                        <span class="text-secondary">Цель: <b style="color:var(--text-primary);">${target.toLocaleString('ru-RU')} ₽</b> (${pct}%)</span>
                    </div>

                    <div class="progress-bar-bg">
                        <div class="progress-bar-fill" style="width: ${pct}%;"></div>
                    </div>

                    <div style="display: flex; justify-content: space-between; font-size: 10px; color: var(--text-secondary); margin-top: 6px;">
                        <span>Базовая: ${p.base_price_rub || '—'} ₽</span>
                        <span>По скидке: ${p.discount_price_rub || '—'} ₽</span>
                        <span>Завершен: ${p.is_completed ? 'Да' : 'Нет'}</span>
                    </div>

                    <div style="display: flex; justify-content: space-between; align-items: center; margin-top: 14px; padding-top: 10px; border-top: 1px solid var(--border-subtle);">
                        <div style="display: flex; gap: 4px;">
                            <button class="btn btn-secondary btn-sm" onclick="incrementProjectAmount('${escapeHtml(p.id)}', 100)">+100 ₽</button>
                            <button class="btn btn-secondary btn-sm" onclick="incrementProjectAmount('${escapeHtml(p.id)}', 500)">+500 ₽</button>
                            <button class="btn btn-secondary btn-sm" onclick="incrementProjectAmount('${escapeHtml(p.id)}', 1000)">+1 000 ₽</button>
                        </div>
                        <div style="display: flex; gap: 6px;">
                            <button class="btn btn-secondary btn-sm" onclick="openEditProjectModal('${escapeHtml(p.id)}')">Редактировать</button>
                            <button class="btn btn-danger btn-sm" onclick="deleteProject('${escapeHtml(p.id)}')">Удалить</button>
                        </div>
                    </div>
                </div>
            `;
        }).join('');
    } catch (err) {
        grid.innerHTML = `<div style="color:var(--status-red); font-size:12px;">Ошибка загрузки спецпроектов: ${err.message}</div>`;
    }
}

async function incrementProjectAmount(id, delta) {
    try {
        const data = await fetchJSON('/api/v1/admin/special-projects/increment', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id: String(id), amount_rub: delta })
        });
        if (!data.success) throw new Error(data.error || 'Ошибка пополнения');
        showToast(`Успешно начислено +${delta} ₽`, 'success');
        loadSpecialProjects();
    } catch (err) {
        showToast(err.message, 'error');
    }
}

function openNewProjectModal() {
    document.getElementById('modal-project-title').textContent = 'Создать спецпроект';
    const idEl = document.getElementById('proj-id');
    idEl.value = '';
    idEl.disabled = false;
    document.getElementById('proj-badge').value = 'СБОР';
    document.getElementById('proj-title').value = '';
    document.getElementById('proj-description').value = '';
    document.getElementById('proj-base-price').value = '4100';
    document.getElementById('proj-discount-price').value = '0';
    document.getElementById('proj-target-amount').value = '4100';
    document.getElementById('proj-collected-amount').value = '0';
    document.getElementById('proj-boosty-url').value = '';
    document.getElementById('proj-boosty-label').value = 'Поддержать сбор';
    document.getElementById('proj-steam-url').value = '';
    document.getElementById('proj-steam-label').value = 'Подарить в Steam';
    document.getElementById('proj-display-order').value = '1';
    document.getElementById('proj-is-active').checked = true;
    document.getElementById('proj-is-completed').checked = false;
    document.getElementById('modal-project').style.display = 'flex';
}

function openEditProjectModal(id) {
    const p = (window.loadedProjects || []).find(x => x.id === id);
    if (!p) return;
    document.getElementById('modal-project-title').textContent = 'Редактировать спецпроект';
    const idEl = document.getElementById('proj-id');
    idEl.value = p.id || '';
    idEl.disabled = true;
    document.getElementById('proj-badge').value = p.badge || 'СБОР';
    document.getElementById('proj-title').value = p.title || '';
    document.getElementById('proj-description').value = p.description || '';
    document.getElementById('proj-base-price').value = p.base_price_rub || 0;
    document.getElementById('proj-discount-price').value = p.discount_price_rub || 0;
    document.getElementById('proj-target-amount').value = p.target_amount_rub || 0;
    document.getElementById('proj-collected-amount').value = (p.current_amount_rub != null) ? p.current_amount_rub : 0;
    document.getElementById('proj-boosty-url').value = p.boosty_url || '';
    document.getElementById('proj-boosty-label').value = p.boosty_label || 'Поддержать сбор';
    document.getElementById('proj-steam-url').value = p.steam_url || '';
    document.getElementById('proj-steam-label').value = p.steam_label || 'Подарить в Steam';
    document.getElementById('proj-display-order').value = p.display_order || 1;
    document.getElementById('proj-is-active').checked = Boolean(p.is_active);
    document.getElementById('proj-is-completed').checked = Boolean(p.is_completed);
    document.getElementById('modal-project').style.display = 'flex';
}

function editProject(p) {
    if (typeof p === 'string') return openEditProjectModal(p);
    if (p && p.id) return openEditProjectModal(p.id);
}

function closeProjectModal() {
    document.getElementById('modal-project').style.display = 'none';
}

async function saveProjectModal() {
    const id = document.getElementById('proj-id').value.trim();
    const title = document.getElementById('proj-title').value.trim();

    if (!id || !title) return showToast('Укажите ID и название спецпроекта', 'error');

    const payload = {
        id: id,
        title: title,
        badge: document.getElementById('proj-badge').value.trim() || 'СБОР',
        description: document.getElementById('proj-description').value.trim(),
        base_price_rub: parseInt(document.getElementById('proj-base-price').value, 10) || 0,
        discount_price_rub: parseInt(document.getElementById('proj-discount-price').value, 10) || 0,
        target_amount_rub: parseInt(document.getElementById('proj-target-amount').value, 10) || 0,
        current_amount_rub: parseInt(document.getElementById('proj-collected-amount').value, 10) || 0,
        boosty_url: document.getElementById('proj-boosty-url').value.trim(),
        boosty_label: document.getElementById('proj-boosty-label').value.trim() || 'Поддержать сбор',
        steam_url: document.getElementById('proj-steam-url').value.trim(),
        steam_label: document.getElementById('proj-steam-label').value.trim() || 'Подарить в Steam',
        display_order: parseInt(document.getElementById('proj-display-order').value, 10) || 1,
        is_active: document.getElementById('proj-is-active').checked,
        is_completed: document.getElementById('proj-is-completed').checked
    };

    try {
        const data = await fetchJSON('/api/v1/admin/special-projects', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });
        if (!data.success) throw new Error(data.error || 'Ошибка сохранения');
        showToast('Спецпроект сохранен', 'success');
        closeProjectModal();
        loadSpecialProjects();
    } catch (err) {
        showToast(err.message, 'error');
    }
}

async function deleteProject(id) {
    if (!confirm(`Вы уверены, что хотите удалить спецпроект "${id}"?`)) return;
    try {
        const data = await fetchJSON(`/api/v1/admin/special-projects?id=${encodeURIComponent(id)}`, { method: 'DELETE' });
        if (!data.success) throw new Error(data.error || 'Ошибка удаления');
        showToast('Спецпроект удален', 'success');
        loadSpecialProjects();
    } catch (err) {
        showToast(err.message, 'error');
    }
}

// =============================================================================
// TAB 2: Шлюзы и Ноды (cluster_nodes - Unified Pool & Hysteria 2 Gateways)
// =============================================================================

async function loadClusterNodes() {
    const grid = document.getElementById('cluster-nodes-grid');
    if (!grid) return;
    grid.innerHTML = '<div class="text-secondary" style="font-size:12px;">Загрузка шлюзов...</div>';

    try {
        const data = await fetchJSON('/api/v1/admin/nodes');

        if (data.cluster) {
            const c = data.cluster;
            const elOnline = document.getElementById('cluster-total-online');
            if (elOnline) elOnline.textContent = `${c.total_online || 0} / ${c.max_slots || 100}`;

            const elSponsor = document.getElementById('cluster-sponsor-slots');
            if (elSponsor) elSponsor.textContent = `${c.sponsor_reserved || 10} слотов`;

            const elFree = document.getElementById('cluster-free-slots');
            if (elFree) elFree.textContent = `${c.free_available != null ? c.free_available : 90} слотов`;

            const elDrain = document.getElementById('cluster-drain-switch');
            if (elDrain) elDrain.checked = Boolean(c.drain_mode);

            const elMaxInput = document.getElementById('cluster-max-slots-input');
            if (elMaxInput && document.activeElement !== elMaxInput) {
                elMaxInput.value = c.max_slots || 100;
            }

            const elSponsorInput = document.getElementById('cluster-sponsor-slots-input');
            if (elSponsorInput && document.activeElement !== elSponsorInput) {
                elSponsorInput.value = c.sponsor_reserved || 10;
            }
        }

        // 1. Master Control Plane Node
        const masterCard = document.getElementById('cluster-master-node-card');
        if (masterCard && data.master_node) {
            const m = data.master_node;
            masterCard.innerHTML = `
                <div class="card" style="border-left: 3px solid #60a5fa; background: #12151c;">
                    <div style="display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 6px; flex-wrap: wrap; gap: 8px;">
                        <div>
                            <div style="font-size: 13px; font-weight: 700; color: var(--text-primary);">${escapeHtml(m.name)}</div>
                            <div class="font-mono text-muted" style="font-size: 10px;">${m.node_id} • ${m.host}:${m.port} • IP: ${m.ip}</div>
                        </div>
                        <span class="badge badge-info">Control Plane • Master API</span>
                    </div>
                    <div style="font-size: 11px; color: var(--text-secondary); line-height: 1.4;">
                        ${escapeHtml(m.role)}
                    </div>
                </div>
            `;
        }

        // 2. Physical Gateways (2 dedicated nodes: Moscow and Frankfurt)
        const nodes = (data && data.nodes) ? data.nodes : [];
        if (nodes.length === 0) {
            grid.innerHTML = '<div class="text-secondary" style="font-size:12px; grid-column: 1 / -1;">Нет доступных шлюзов.</div>';
        } else {
            grid.innerHTML = nodes.map(n => `
                <div class="card">
                    <div style="display: flex; justify-content: space-between; align-items: start; margin-bottom: 8px;">
                        <div>
                            <div style="font-size: 13px; font-weight: 700; color: var(--text-primary);">${escapeHtml(n.name)}</div>
                            <div class="font-mono text-muted" style="font-size: 10px;">${n.node_id} • ${n.host}:${n.port} • IP: ${n.ip}</div>
                        </div>
                        <span class="badge ${n.is_healthy ? 'badge-success' : 'badge-urgent'}">
                            ${n.is_healthy ? 'В сети' : 'Сбой'}
                        </span>
                    </div>

                    <div style="font-size: 11px; color: var(--text-secondary); margin-bottom: 8px;">
                        ${escapeHtml(n.role || 'Игровой шлюз')}
                    </div>

                    <div class="grid-3" style="margin: 10px 0;">
                        <div class="stat-box">
                            <div class="stat-box-label">Всего игроков</div>
                            <div class="stat-box-val font-mono">${(n.active_players ?? 0)}</div>
                        </div>
                        <div class="stat-box">
                            <div class="stat-box-label">Прямой / Транзит</div>
                            <div class="stat-box-val font-mono" style="font-size:12px;">${n.direct_players ?? 0} / ${n.transit_players ?? 0}</div>
                        </div>
                        <div class="stat-box">
                            <div class="stat-box-label">Задержка (RTT)</div>
                            <div class="stat-box-val font-mono">${n.rtt_ms || '—'} мс</div>
                        </div>
                    </div>

                    <div style="display: flex; justify-content: space-between; align-items: center; margin-top: 10px; padding-top: 8px; border-top: 1px solid var(--border-subtle); font-size: 11px; color: var(--text-secondary);">
                        <span>Протокол: Hysteria 2 (UDP 443, BBR)</span>
                        <span>Пул слотов: Единый глобальный</span>
                    </div>
                </div>
            `).join('');
        }

        // 3. Client Route Modes (3 client routing modes)
        const routesGrid = document.getElementById('cluster-routes-grid');
        if (routesGrid && data.routes) {
            routesGrid.innerHTML = data.routes.map(r => `
                <div class="card">
                    <div style="display: flex; justify-content: space-between; align-items: start; margin-bottom: 6px;">
                        <div>
                            <div style="font-size: 12px; font-weight: 700; color: var(--text-primary);">${escapeHtml(r.name)}</div>
                            <div class="font-mono text-muted" style="font-size: 10px;">Режим: ${r.id}</div>
                        </div>
                        <span class="badge badge-update font-mono">${r.active_players ?? 0} игроков</span>
                    </div>
                    <div style="font-size: 11px; color: var(--text-secondary); margin-bottom: 8px; min-height: 32px;">
                        ${escapeHtml(r.description)}
                    </div>
                    <div style="display: flex; justify-content: space-between; align-items: center; padding-top: 8px; border-top: 1px solid var(--border-subtle); font-size: 10px; color: var(--text-secondary);">
                        <span class="font-mono" style="color: var(--accent);">${escapeHtml(r.gateway_path)}</span>
                        <span class="font-mono">${r.rtt_ms || '—'} мс</span>
                    </div>
                </div>
            `).join('');
        }

    } catch (err) {
        grid.innerHTML = `<div style="color:var(--status-red); font-size:12px;">Ошибка загрузки узлов: ${err.message}</div>`;
    }
}

async function saveClusterSlots() {
    const maxSlotsEl = document.getElementById('cluster-max-slots-input');
    const sponsorSlotsEl = document.getElementById('cluster-sponsor-slots-input');
    const maxVal = maxSlotsEl ? (parseInt(maxSlotsEl.value, 10) || 100) : 100;
    const sponsorVal = sponsorSlotsEl ? (parseInt(sponsorSlotsEl.value, 10) || 10) : 10;

    try {
        const data = await fetchJSON('/api/v1/admin/nodes/slots', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                max_sessions: maxVal,
                dedicated_sponsor_slots: sponsorVal
            })
        });
        if (!data.success) throw new Error(data.error || 'Ошибка сохранения пула слотов');
        showToast(`Емкость пула слотов обновлена: ${maxVal} (резерв спонсоров: ${sponsorVal})`, 'success');
        loadClusterNodes();
    } catch (err) {
        showToast(err.message, 'error');
    }
}

async function toggleClusterDrain(drain) {
    try {
        const data = await fetchJSON('/api/v1/admin/nodes/drain', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ drain_mode: drain })
        });
        if (!data.success) throw new Error(data.error || 'Ошибка смены режима');
        showToast(`Глобальный Drain Mode: ${drain ? 'ВКЛЮЧЕН (техобслуживание)' : 'ВЫКЛЮЧЕН (штатная работа)'}`, 'info');
        loadClusterNodes();
    } catch (err) {
        showToast(err.message, 'error');
        const el = document.getElementById('cluster-drain-switch');
        if (el) el.checked = !drain;
    }
}

// =============================================================================
// TAB 3: Финансы и Спонсорство (server_config / server_settings)
// =============================================================================

async function loadMonetizationSettings() {
    try {
        const data = await fetchJSON('/api/v1/admin/monetization');

        const elBoosty = document.getElementById('pay-boosty-enabled');
        if (elBoosty) elBoosty.checked = Boolean(data.boosty_enabled);

        const elSbp = document.getElementById('pay-sbp-enabled');
        if (elSbp) elSbp.checked = Boolean(data.sbp_enabled);

        const elUsdt = document.getElementById('pay-usdt-enabled');
        if (elUsdt) elUsdt.checked = Boolean(data.usdt_enabled);

        const elWallet = document.getElementById('pay-usdt-address');
        if (elWallet) elWallet.value = data.usdt_wallet || '';

        const elReason = document.getElementById('pay-pause-reason');
        if (elReason) elReason.value = data.pause_reason || '';
    } catch (err) {
        console.error('Failed to load monetization:', err);
    }
}

async function saveMonetizationSettings() {
    const payload = {
        boosty_enabled: document.getElementById('pay-boosty-enabled') ? document.getElementById('pay-boosty-enabled').checked : true,
        sbp_enabled: document.getElementById('pay-sbp-enabled') ? document.getElementById('pay-sbp-enabled').checked : true,
        usdt_enabled: document.getElementById('pay-usdt-enabled') ? document.getElementById('pay-usdt-enabled').checked : false,
        usdt_wallet: document.getElementById('pay-usdt-address') ? document.getElementById('pay-usdt-address').value.trim() : '',
        pause_reason: document.getElementById('pay-pause-reason') ? document.getElementById('pay-pause-reason').value.trim() : ''
    };

    try {
        const data = await fetchJSON('/api/v1/admin/monetization', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });
        if (!data.success) throw new Error(data.error || 'Ошибка сохранения');
        showToast('Настройки монетизации сохранены и синхронизированы с клиентом', 'success');
    } catch (err) {
        showToast(err.message, 'error');
    }
}

async function grantSponsorStatus() {
    const acc = document.getElementById('grant-account-number').value.trim();
    const months = parseInt(document.getElementById('grant-duration-months').value, 10);
    const amount = parseInt(document.getElementById('grant-amount-rub').value, 10);

    if (!acc) return showToast('Укажите номер аккаунта', 'error');

    try {
        const data = await fetchJSON('/api/v1/admin/sponsors/grant', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ account_number: acc, duration_months: months, amount_rub: amount })
        });
        if (!data.success) throw new Error(data.error || 'Ошибка начисления');
        showToast(`Статус спонсора успешно начислен аккаунту ${acc}`, 'success');
        document.getElementById('grant-account-number').value = '';
    } catch (err) {
        showToast(err.message, 'error');
    }
}

// =============================================================================
// TAB 4: Внутриигровые уведомления
// =============================================================================

function toggleNotifTargetField() {
    const type = document.getElementById('notif-target-type').value;
    document.getElementById('notif-target-id-wrap').style.display = (type === 'broadcast') ? 'none' : 'block';
}

function updateNotifPreview() {
    const title = document.getElementById('notif-title').value || 'Заголовок уведомления';
    const msg = document.getElementById('notif-message').value || 'Текст сообщения...';
    const btnLabel = document.getElementById('notif-action-label').value.trim();

    document.getElementById('prev-notif-title').textContent = title;
    document.getElementById('prev-notif-msg').textContent = msg;

    const btnEl = document.getElementById('prev-notif-btn');
    if (btnLabel) {
        btnEl.textContent = btnLabel;
        btnEl.style.display = 'inline-block';
    } else {
        btnEl.style.display = 'none';
    }
}

async function sendAdminNotification() {
    const payload = {
        title: document.getElementById('notif-title').value.trim(),
        message: document.getElementById('notif-message').value.trim(),
        severity: document.getElementById('notif-severity').value,
        target_type: document.getElementById('notif-target-type').value,
        target_id: document.getElementById('notif-target-id').value.trim(),
        action_label: document.getElementById('notif-action-label').value.trim(),
        action_url: document.getElementById('notif-action-url').value.trim()
    };

    if (!payload.title || !payload.message) {
        return showToast('Заполните заголовок и текст уведомления', 'error');
    }

    try {
        const data = await fetchJSON('/api/v1/admin/notifications', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });
        if (!data.success) throw new Error(data.error || 'Ошибка отправки');

        showToast('Уведомление успешно разослано игрокам', 'success');
        document.getElementById('notif-title').value = '';
        document.getElementById('notif-message').value = '';
        document.getElementById('notif-action-label').value = '';
        document.getElementById('notif-action-url').value = '';
        updateNotifPreview();
        loadNotifications();
    } catch (err) {
        showToast(err.message, 'error');
    }
}

async function loadNotifications() {
    const tbody = document.getElementById('tbody-notifications');
    if (!tbody) return;

    try {
        const data = await fetchJSON('/api/v1/admin/notifications');
        const list = (data && data.notifications) ? data.notifications : [];

        if (list.length === 0) {
            tbody.innerHTML = '<tr><td colspan="4" class="text-secondary" style="text-align:center; padding:16px;">Нет активных уведомлений</td></tr>';
            return;
        }

        tbody.innerHTML = list.map(n => {
            const dateStr = formatDateRu(n.created_at);
            return `
                <tr>
                    <td><b>${escapeHtml(n.title)}</b></td>
                    <td><span class="badge badge-${n.severity || 'info'}">${n.severity || 'info'}</span></td>
                    <td class="font-mono">${dateStr}</td>
                    <td>
                        <button class="btn btn-danger btn-sm" onclick="deleteNotification(${n.id})">Удалить</button>
                    </td>
                </tr>
            `;
        }).join('');
    } catch (err) {
        tbody.innerHTML = `<tr><td colspan="4" style="color:var(--status-red);">Ошибка: ${err.message}</td></tr>`;
    }
}

async function deleteNotification(id) {
    if (!confirm('Отозвать и удалить это уведомление?')) return;
    try {
        const data = await fetchJSON(`/api/v1/admin/notifications?id=${id}`, { method: 'DELETE' });
        if (!data.success) throw new Error(data.error || 'Ошибка удаления');
        showToast('Уведомление удалено', 'success');
        loadNotifications();
    } catch (err) {
        showToast(err.message, 'error');
    }
}

// =============================================================================
// TAB 5: Поддержка и Тикеты (support_tickets)
// =============================================================================

// =============================================================================
// TAB 5: Поддержка и Тикеты (support_tickets - Messenger Layout)
// =============================================================================

async function loadTickets(resetSelection = false) {
    const listEl = document.getElementById('tickets-list');
    if (!listEl) return;
    const status = document.getElementById('support-status-filter').value;
    listEl.innerHTML = '<div class="text-secondary" style="font-size:12px; padding:8px;">Загрузка обращений...</div>';

    try {
        const data = await fetchJSON(`/api/v1/admin/tickets?status=${encodeURIComponent(status)}`);
        const tickets = (data && data.tickets) ? data.tickets : [];
        window.loadedTickets = tickets;

        const countEl = document.getElementById('tickets-queue-count');
        if (countEl) countEl.textContent = `${tickets.length}`;

        if (tickets.length === 0) {
            listEl.innerHTML = '<div class="text-secondary" style="font-size:11px; padding: 12px; text-align:center;">Нет тикетов в данной категории.</div>';
            if (resetSelection || !currentTicketId) {
                document.getElementById('active-ticket-num').textContent = '#TK-0000';
                document.getElementById('active-ticket-meta').textContent = 'Нет активного обращения';
                document.getElementById('active-ticket-actions').style.display = 'none';
                document.getElementById('ticket-canned-bar').style.display = 'none';
                document.getElementById('ticket-chat-footer').style.display = 'none';
                document.getElementById('ticket-chat-messages').innerHTML = '<div style="color: var(--text-muted); text-align: center; margin: auto; font-size: 12px;">Выберите обращение из списка</div>';
                currentTicketId = null;
            }
            return;
        }

        listEl.innerHTML = tickets.map(t => {
            const isActive = (currentTicketId === t.id);
            const statusBadgeClass = (t.status === 'resolved') ? 'badge-success' : ((t.status === 'new') ? 'badge-update' : ((t.status === 'in_progress') ? 'badge-warning' : 'badge-neutral'));
            const statusLabel = (t.status === 'new') ? 'Новый' : ((t.status === 'in_progress') ? 'В работе' : ((t.status === 'resolved') ? 'Решен' : 'Архив'));

            return `
                <div class="ticket-item-card ${isActive ? 'active' : ''} ${t.status === 'new' ? 'unread' : ''}" onclick="openTicketDetail(${t.id})">
                    <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom: 4px;">
                        <span class="font-mono text-muted" style="font-size:10px;">#TK-${String(t.id).padStart(4, '0')}</span>
                        <span class="badge ${statusBadgeClass}">${statusLabel}</span>
                    </div>
                    <div style="font-size:12px; font-weight:600; color:var(--text-primary); margin-bottom:4px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">
                        ${escapeHtml(t.user_comment || t.category || 'Обращение')}
                    </div>
                    <div style="display:flex; justify-content:space-between; font-size:10px; color:var(--text-secondary);">
                        <span class="font-mono">${escapeHtml(t.account_number || t.device_id || 'Игрок')}</span>
                        <span class="font-mono">${formatDateRu(t.created_at)}</span>
                    </div>
                </div>
            `;
        }).join('');

        filterTicketsList();

        // If no ticket selected yet, open the first one
        if (!currentTicketId && tickets.length > 0) {
            openTicketDetail(tickets[0].id);
        }
    } catch (err) {
        listEl.innerHTML = `<div style="color:var(--status-red); font-size:12px; padding:8px;">Ошибка: ${err.message}</div>`;
    }
}

function filterTicketsList() {
    const input = document.getElementById('tickets-search-input');
    const query = input ? input.value.toLowerCase().trim() : '';
    const cards = document.querySelectorAll('.ticket-item-card');
    cards.forEach(card => {
        const text = card.textContent.toLowerCase();
        card.style.display = (!query || text.includes(query)) ? 'block' : 'none';
    });
}

async function openTicketDetail(id) {
    currentTicketId = id;

    // Highlight active card in left list
    document.querySelectorAll('.ticket-item-card').forEach(c => c.classList.remove('active'));
    const allCards = document.querySelectorAll('.ticket-item-card');
    allCards.forEach(c => {
        if (c.textContent.includes(`#TK-${String(id).padStart(4, '0')}`)) {
            c.classList.add('active');
        }
    });

    try {
        const data = await fetchJSON(`/api/v1/admin/tickets/${id}`);
        const t = data.ticket;
        const msgs = data.messages || [];

        // Header Meta
        document.getElementById('active-ticket-num').textContent = `#TK-${String(t.id).padStart(4, '0')}`;
        document.getElementById('active-ticket-meta').innerHTML = `
            <div>
                <b style="color:var(--text-primary);">${escapeHtml(t.category || 'Общее обращение')}</b> • 
                <span class="font-mono">Аккаунт: ${t.account_number ? `<a href="javascript:void(0)" onclick="openAccountCard('${escapeHtml(t.account_number)}')" style="color:var(--accent); text-decoration:underline;">${escapeHtml(t.account_number)}</a>` : '—'}</span> • 
                <span class="font-mono">v${escapeHtml(t.app_version || '2.1.12')}</span>
            </div>
            <div class="text-muted font-mono" style="font-size:10px;">Создан: ${formatDateRu(t.created_at)}</div>
        `;

        // Logs download button
        const logsWrap = document.getElementById('active-ticket-logs-wrap');
        if (logsWrap) {
            logsWrap.innerHTML = t.has_logs 
                ? `<a href="/api/v1/tickets/logs?ticket_id=${t.id}" target="_blank" class="btn btn-secondary btn-sm" style="font-size:11px; padding:3px 8px;">
                     <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
                     Логи (tar.gz)
                   </a>`
                : '';
        }

        // Status select
        const statusSelect = document.getElementById('ticket-status-select');
        if (statusSelect) {
            statusSelect.value = t.status || 'new';
        }

        // Show messenger controls
        document.getElementById('active-ticket-actions').style.display = 'flex';
        document.getElementById('ticket-canned-bar').style.display = 'flex';
        document.getElementById('ticket-chat-footer').style.display = 'flex';

        // Messages timeline
        const chatEl = document.getElementById('ticket-chat-messages');
        if (msgs.length === 0) {
            chatEl.innerHTML = '<div style="color:var(--text-muted); font-size:12px; text-align:center; margin:auto;">В этом обращении пока нет сообщений.</div>';
        } else {
            chatEl.innerHTML = msgs.map(m => {
                const senderType = String(m.sender_type || m.sender || '').toLowerCase().trim();
                const isAdminMsg = senderType === 'admin' || senderType.includes('admin') || senderType.includes('разработчик');
                const isSystemMsg = senderType === 'system';
                const senderTitle = isSystemMsg ? 'СИСТЕМА' : (isAdminMsg ? 'АДМИНИСТРАТОР' : 'ИГРОК');
                const bubbleClass = isSystemMsg ? 'chat-bubble-system' : (isAdminMsg ? 'chat-bubble-admin' : 'chat-bubble-user');
                return `
                    <div class="chat-bubble ${bubbleClass}">
                        <div class="chat-bubble-meta font-mono">
                            <span>${senderTitle}</span>
                            <span>${formatDateRu(m.created_at)}</span>
                        </div>
                        <div class="chat-bubble-text">${escapeHtml(m.message)}</div>
                    </div>
                `;
            }).join('');
        }

        // Scroll timeline to bottom
        chatEl.scrollTop = chatEl.scrollHeight;

        // Ensure Enter-to-send is bound
        const replyTextarea = document.getElementById('ticket-reply-text');
        if (replyTextarea && !replyTextarea.dataset.enterBound) {
            replyTextarea.dataset.enterBound = 'true';
            replyTextarea.addEventListener('keydown', (e) => {
                if (e.key === 'Enter' && !e.shiftKey) {
                    e.preventDefault();
                    sendTicketReply();
                }
            });
        }
    } catch (err) {
        showToast(err.message, 'error');
    }
}

function insertCannedReply(text) {
    const el = document.getElementById('ticket-reply-text');
    if (!el) return;
    el.value = text;
    el.focus();
}

async function changeActiveTicketStatus(status) {
    if (!currentTicketId) return;
    try {
        const data = await fetchJSON(`/api/v1/admin/tickets/${currentTicketId}/status`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ status })
        });
        if (!data.success) throw new Error(data.error || 'Ошибка смены статуса');
        showToast(`Статус тикета изменен на: ${status}`, 'success');
        loadTickets(false);
    } catch (err) {
        showToast(err.message, 'error');
    }
}

async function sendTicketReply() {
    if (!currentTicketId) return;
    const replyEl = document.getElementById('ticket-reply-text');
    const text = replyEl ? replyEl.value.trim() : '';
    if (!text) return showToast('Введите текст ответа', 'error');

    try {
        const data = await fetchJSON(`/api/v1/admin/tickets/${currentTicketId}/reply`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ message: text })
        });
        if (!data.success) throw new Error(data.error || 'Ошибка отправки');
        replyEl.value = '';
        showToast('Ответ отправлен игроку', 'success');
        openTicketDetail(currentTicketId);
        loadTickets(false);
    } catch (err) {
        showToast(err.message, 'error');
    }
}

async function setTicketStatus(status) {
    await changeActiveTicketStatus(status);
}

// =============================================================================
// TAB 6: Игры, Канбан & Золотообмен (game_suggestions + MetaForge)
// =============================================================================

async function loadKanbanAndGold() {
    await Promise.all([loadKanbanBoard(), loadMetaForgeTelemetry()]);
}

async function loadKanbanBoard() {
    try {
        const data = await fetchJSON('/api/v1/admin/games/suggestions');
        const games = (data && data.suggestions) ? data.suggestions : [];

        const columns = {
            'voting': [],
            'queue_integration': [],
            'supported': [],
            'forbidden': []
        };

        games.forEach(g => {
            const st = g.status || 'voting';
            if (columns[st]) columns[st].push(g);
            else columns['voting'].push(g);
        });

        // Update counts
        const cntVoting = document.getElementById('kcount-voting');
        if (cntVoting) cntVoting.textContent = columns['voting'].length;

        const cntQueue = document.getElementById('kcount-queue');
        if (cntQueue) cntQueue.textContent = columns['queue_integration'].length;

        const cntSupported = document.getElementById('kcount-supported');
        if (cntSupported) cntSupported.textContent = columns['supported'].length;

        const cntForbidden = document.getElementById('kcount-forbidden');
        if (cntForbidden) cntForbidden.textContent = columns['forbidden'].length;

        // Render cards
        renderKanbanColumn('kcol-voting', columns['voting'], 'voting');
        renderKanbanColumn('kcol-queue_integration', columns['queue_integration'], 'queue_integration');
        renderKanbanColumn('kcol-supported', columns['supported'], 'supported');
        renderKanbanColumn('kcol-forbidden', columns['forbidden'], 'forbidden');
    } catch (err) {
        console.error('Failed to load kanban:', err);
    }
}

function renderKanbanColumn(containerId, items, columnStatus) {
    const el = document.getElementById(containerId);
    if (!el) return;

    if (items.length === 0) {
        el.innerHTML = '<div style="font-size:11px; color:var(--text-muted); text-align:center; padding:24px 8px;">Нет карточек</div>';
        return;
    }

    el.innerHTML = items.map(g => `
        <div class="kanban-card" draggable="true" ondragstart="handleKanbanDragStart(event, ${g.steam_app_id})">
            <div class="kanban-card-top">
                <div class="kanban-icon-wrap" style="position:relative; width:26px; height:26px; flex-shrink:0;">
                    ${g.icon_url ? `<img src="${escapeHtml(g.icon_url)}" class="kanban-game-icon" alt="" onerror="this.style.display='none'; if(this.nextElementSibling) this.nextElementSibling.style.display='flex';">` : ''}
                    <div class="kanban-game-icon kanban-game-icon-fallback" style="${g.icon_url ? 'display:none;' : 'display:flex;'} align-items:center; justify-content:center; background:#1c1c1c; border:1px solid #282828;">
                        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="#666" stroke-width="2"><rect x="2" y="6" width="20" height="12" rx="2"/><path d="M6 12h4m-2-2v4m8-3h.01m2 2h.01"/></svg>
                    </div>
                </div>
                <div class="kanban-card-title">${escapeHtml(g.title)}</div>
            </div>
            <div class="kanban-card-meta">
                <span class="font-mono">AppID: ${g.steam_app_id}</span>
                <span class="font-mono" style="color:var(--accent);">Голосов: ${g.votes_count || 0}</span>
            </div>
        </div>
    `).join('');
}

function handleKanbanDragStart(e, steamAppId) {
    draggedCardAppId = steamAppId;
    e.dataTransfer.setData('text/plain', String(steamAppId));
    e.target.classList.add('dragging');
}

function allowKanbanDrop(e) {
    e.preventDefault();
}

async function handleKanbanDrop(e, targetStatus) {
    e.preventDefault();
    const appEl = document.querySelector('.kanban-card.dragging');
    if (appEl) appEl.classList.remove('dragging');

    if (!draggedCardAppId) return;
    await moveKanbanCard(draggedCardAppId, targetStatus);
    draggedCardAppId = null;
}

async function moveKanbanCard(steamAppId, targetStatus) {
    try {
        const data = await fetchJSON('/api/v1/admin/games/kanban/move', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ steam_app_id: steamAppId, status: targetStatus })
        });
        if (!data.success) throw new Error(data.error || 'Ошибка перемещения');

        showToast(`Игра переведена в статус: ${targetStatus}`, 'success');
        loadKanbanBoard();
    } catch (err) {
        showToast(err.message, 'error');
    }
}

// MetaForge Gold Market Sync Telemetry
async function loadMetaForgeTelemetry() {
    try {
        const data = await fetchJSON('/api/v1/admin/gold/sync-telemetry');

        if (data.current) {
            const elTime = document.getElementById('metaforge-last-time');
            if (elTime) elTime.textContent = data.current.detected_time_msk || '03:00 МСК';

            const elDate = document.getElementById('metaforge-last-date');
            if (elDate) elDate.textContent = data.current.target_date ? formatDateRu(data.current.target_date) : 'Сегодня';

            const elDelay = document.getElementById('metaforge-delay-val');
            if (elDelay) elDelay.textContent = formatDelayText(data.current.delay_seconds);

            const elPrice = document.getElementById('metaforge-current-price');
            if (elPrice) elPrice.textContent = `$ ${(data.current.price_usd || 0).toLocaleString('ru-RU')}`;
        }

        const tbody = document.getElementById('tbody-gold-sync-history');
        if (tbody) {
            const list = data.history || [];
            if (list.length === 0) {
                tbody.innerHTML = '<tr><td colspan="5" class="text-secondary" style="text-align:center; padding:12px;">Сбор истории...</td></tr>';
                return;
            }

            tbody.innerHTML = list.map(item => `
                <tr>
                    <td class="font-mono"><b>${formatDateRu(item.target_date || item.created_at)}</b></td>
                    <td class="font-mono text-muted">03:00:00 МСК</td>
                    <td class="font-mono">${escapeHtml(item.detected_time_msk || '—')}</td>
                    <td class="font-mono" style="color:var(--accent);">+${formatDelayText(item.delay_seconds)}</td>
                    <td class="font-mono">$ ${(item.price_usd || 0).toLocaleString('ru-RU')}</td>
                </tr>
            `).join('');
        }
    } catch (err) {
        console.error('Failed to load gold telemetry:', err);
    }
}

function formatDelayText(sec) {
    if (sec === undefined || sec === null) return '0 сек';
    if (sec < 60) return `${sec} сек`;
    const mins = Math.floor(sec / 60);
    const remSec = sec % 60;
    return `${mins} мин ${remSec} сек`;
}

// =============================================================================
// TAB 7: Системные флаги и Эксплуатация
// =============================================================================

async function loadSystemOps() {
    try {
        const data = await fetchJSON('/api/v1/admin/settings');

        const elMaint = document.getElementById('sys-maint-enabled');
        if (elMaint) elMaint.checked = Boolean(data.maintenance_mode);

        const elMsg = document.getElementById('sys-maint-msg');
        if (elMsg) elMsg.value = data.maintenance_message || '';
    } catch (err) {
        console.error('Failed to load system ops:', err);
    }
}

async function saveSystemOpsSettings() {
    const payload = {
        maintenance_mode: document.getElementById('sys-maint-enabled') ? document.getElementById('sys-maint-enabled').checked : false,
        maintenance_message: document.getElementById('sys-maint-msg') ? document.getElementById('sys-maint-msg').value.trim() : ''
    };

    try {
        const data = await fetchJSON('/api/v1/admin/settings', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });
        if (!data.success) throw new Error(data.error || 'Ошибка сохранения');
        showToast('Системные параметры сохранены', 'success');
    } catch (err) {
        showToast(err.message, 'error');
    }
}

async function reloadServerFilters() {
    try {
        const data = await fetchJSON('/api/v1/admin/reload-filters', { method: 'POST' });
        showToast(data.message || 'Фильтры перезагружены', 'info');
    } catch (err) {
        showToast(err.message, 'error');
    }
}

async function clearGoldCache() {
    try {
        await fetchJSON('/api/v1/market/gold?t=' + Date.now());
        showToast('Курс золота успешно обновлен с MetaForge', 'success');
        loadKanbanAndGold();
    } catch (err) {
        showToast(err.message, 'error');
    }
}

// =============================================================================
// Utilities (Strictly DD.MM.YYYY Date Formatting, Toast, Escaping)
// =============================================================================

function formatDateRu(val) {
    if (!val) return '—';
    try {
        if (typeof val === 'string' && /^\d{2}\.\d{2}\.\d{4}/.test(val)) return val;
        const d = new Date(val);
        if (isNaN(d.getTime())) {
            if (typeof val === 'string' && val.includes('-')) {
                return val.slice(0, 10).split('-').reverse().join('.');
            }
            return val;
        }
        const day = String(d.getDate()).padStart(2, '0');
        const mo = String(d.getMonth() + 1).padStart(2, '0');
        const yr = d.getFullYear();
        const hh = String(d.getHours()).padStart(2, '0');
        const mm = String(d.getMinutes()).padStart(2, '0');
        return `${day}.${mo}.${yr} ${hh}:${mm}`;
    } catch (e) {
        return val;
    }
}

function showToast(message, type = 'info') {
    const container = document.getElementById('toast-container');
    if (!container) return;

    const el = document.createElement('div');
    el.className = 'toast';
    if (type === 'error') el.style.borderLeftColor = 'var(--status-red)';
    if (type === 'success') el.style.borderLeftColor = 'var(--status-green)';

    el.textContent = message;
    container.appendChild(el);

    setTimeout(() => {
        el.style.opacity = '0';
        el.style.transition = 'opacity 0.3s';
        setTimeout(() => el.remove(), 300);
    }, 3500);
}

function escapeHtml(str) {
    if (!str) return '';
    return String(str)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#039;');
}
const escapeHTML = escapeHtml;

// =============================================================================
// Account Card Modal (Карточка аккаунта со всеми данными из БД и RAM)
// =============================================================================

let currentModalAccount = null;
let currentModalAccountData = null;

function searchAccountFromNav() {
    const input = document.getElementById('nav-account-search');
    const val = input ? input.value.trim() : '';
    if (!val) return;
    openAccountCard(val);
}

async function openAccountCard(accountIdentifier) {
    if (!accountIdentifier) return;
    currentModalAccount = accountIdentifier;

    const modal = document.getElementById('modal-account');
    if (modal) modal.style.display = 'flex';

    // Set initial loading skeleton
    document.getElementById('acc-modal-number').textContent = accountIdentifier;
    document.getElementById('acc-modal-online-badge').className = 'badge badge-neutral';
    document.getElementById('acc-modal-online-badge').textContent = 'Загрузка...';
    document.getElementById('acc-modal-nickname').textContent = '...';
    document.getElementById('acc-modal-motto').textContent = '...';
    document.getElementById('acc-modal-donations').textContent = '... ₽';
    document.getElementById('acc-modal-sponsor-until').textContent = '...';
    document.getElementById('acc-modal-steam').textContent = '...';
    document.getElementById('acc-modal-registered').textContent = '...';
    document.getElementById('acc-modal-live-session').style.display = 'none';

    try {
        const data = await fetchJSON(`/api/v1/admin/accounts/${encodeURIComponent(accountIdentifier)}`);
        if (!data || !data.success || !data.account) {
            throw new Error(data.error || 'Аккаунт не найден');
        }

        currentModalAccountData = data;
        const acc = data.account;
        const devices = data.devices || [];
        const connections = data.connections || [];
        const tickets = data.tickets || [];
        const bans = data.bans || [];
        const isOnline = Boolean(data.is_online);
        const sess = data.active_session;

        currentModalAccount = acc.account_number;
        document.getElementById('acc-modal-number').textContent = acc.account_number;

        // Online status
        const onlineBadge = document.getElementById('acc-modal-online-badge');
        if (isOnline && sess) {
            onlineBadge.className = 'badge badge-success';
            onlineBadge.textContent = 'В СЕТИ';
            document.getElementById('acc-modal-live-session').style.display = 'block';
            document.getElementById('acc-live-game').textContent = sess.game || 'Онлайн';
            document.getElementById('acc-live-route').textContent = sess.route_mode || sess.connected_node || 'Прямой';
            document.getElementById('acc-live-ip').textContent = sess.client_ip || '—';
            document.getElementById('acc-live-version').textContent = sess.client_version || 'v2.1.12';
        } else {
            onlineBadge.className = 'badge badge-neutral';
            onlineBadge.textContent = 'ОФФЛАЙН';
            document.getElementById('acc-modal-live-session').style.display = 'none';
        }

        // Tier badge
        const tierBadge = document.getElementById('acc-modal-tier-badge');
        if (acc.is_sponsor) {
            tierBadge.className = 'badge badge-update';
            tierBadge.textContent = 'SPONSOR';
        } else {
            tierBadge.className = 'badge badge-info';
            tierBadge.textContent = 'FREE';
        }

        // Ban badge & toggle button
        const banBadge = document.getElementById('acc-modal-ban-badge');
        const banBtn = document.getElementById('acc-btn-ban');
        const banSubTabBtn = document.getElementById('acc-subtab-btn-bans');
        if (data.is_banned) {
            banBadge.style.display = 'inline-flex';
            banBtn.className = 'btn btn-secondary btn-sm';
            banBtn.textContent = 'Разблокировать доступ';
            banBtn.onclick = unbanModalAccount;
            banSubTabBtn.style.display = 'inline-block';
        } else {
            banBadge.style.display = 'none';
            banBtn.className = 'btn btn-danger btn-sm';
            banBtn.textContent = 'Заблокировать доступ';
            banBtn.onclick = banModalAccount;
            banSubTabBtn.style.display = bans.length > 0 ? 'inline-block' : 'none';
        }

        // Profile fields
        document.getElementById('acc-modal-nickname').textContent = acc.nickname || 'Безымянный игрок';
        document.getElementById('acc-modal-motto').textContent = acc.motto || 'Без статуса';
        document.getElementById('acc-modal-donations').textContent = `${acc.total_donated_rub || 0} ₽`;
        
        if (acc.sponsor_until) {
            const expDate = new Date(acc.sponsor_until);
            document.getElementById('acc-modal-sponsor-until').textContent = `До ${expDate.toLocaleDateString('ru-RU')}`;
        } else {
            document.getElementById('acc-modal-sponsor-until').textContent = 'Спонсорство не активно';
        }

        if (acc.steam_id) {
            document.getElementById('acc-modal-steam').textContent = acc.steam_id;
            document.getElementById('acc-modal-steam-link').innerHTML = `<a href="https://steamcommunity.com/profiles/${escapeHtml(acc.steam_id)}" target="_blank" style="color:var(--accent); text-decoration:underline;">Профиль Steam</a>`;
        } else {
            document.getElementById('acc-modal-steam').textContent = 'Не привязан';
            document.getElementById('acc-modal-steam-link').textContent = '—';
        }

        document.getElementById('acc-modal-registered').textContent = formatDateRu(acc.created_at);
        document.getElementById('acc-modal-devices-count').textContent = `${devices.length} привязанных устройств`;

        // Update counts in sub-tab headers
        document.getElementById('acc-tab-cnt-dev').textContent = devices.length;
        document.getElementById('acc-tab-cnt-conn').textContent = connections.length;
        document.getElementById('acc-tab-cnt-tick').textContent = tickets.length;
        document.getElementById('acc-tab-cnt-bans').textContent = bans.length;

        // Sub-Tab 1: Devices
        const devTbody = document.getElementById('acc-tbody-devices');
        if (devices.length === 0) {
            devTbody.innerHTML = '<tr><td colspan="2" class="text-secondary" style="text-align: center;">Нет привязанных устройств</td></tr>';
        } else {
            devTbody.innerHTML = devices.map(d => `
                <tr>
                    <td class="font-mono" style="font-size:11px;">${escapeHtml(d.device_id)}</td>
                    <td class="font-mono text-muted" style="font-size:11px;">${formatDateRu(d.created_at)}</td>
                </tr>
            `).join('');
        }

        // Sub-Tab 2: Connections History
        const connTbody = document.getElementById('acc-tbody-connections');
        if (connections.length === 0) {
            connTbody.innerHTML = '<tr><td colspan="5" class="text-secondary" style="text-align: center;">История подключений пуста</td></tr>';
        } else {
            connTbody.innerHTML = connections.map(c => `
                <tr>
                    <td class="font-mono text-muted" style="font-size:11px;">${formatDateRu(c.connected_at)}</td>
                    <td class="font-mono" style="font-size:11px;">${escapeHtml(c.client_ip)}</td>
                    <td style="font-size:11px;">${escapeHtml(c.city || '—')}, ${escapeHtml(c.country || '—')}</td>
                    <td><span class="badge badge-neutral font-mono" style="font-size:9px;">${escapeHtml(c.route_mode || 'direct')}</span></td>
                    <td class="font-mono text-muted" style="font-size:11px;">${escapeHtml(c.app_version || '—')}</td>
                </tr>
            `).join('');
        }

        // Sub-Tab 3: Support Tickets
        const tickTbody = document.getElementById('acc-tbody-tickets');
        if (tickets.length === 0) {
            tickTbody.innerHTML = '<tr><td colspan="6" class="text-secondary" style="text-align: center;">Нет обращений в поддержку</td></tr>';
        } else {
            tickTbody.innerHTML = tickets.map(t => {
                const statusBadge = (t.status === 'resolved') ? 'badge-success' : ((t.status === 'new') ? 'badge-update' : ((t.status === 'in_progress') ? 'badge-warning' : 'badge-neutral'));
                const statusText = (t.status === 'new') ? 'Новый' : ((t.status === 'in_progress') ? 'В работе' : ((t.status === 'resolved') ? 'Решен' : 'Архив'));
                return `
                    <tr>
                        <td class="font-mono text-muted" style="font-size:11px;">#TK-${String(t.id).padStart(4, '0')}</td>
                        <td style="font-size:11px; font-weight:600;">${escapeHtml(t.category || '—')}</td>
                        <td><span class="badge ${statusBadge}">${statusText}</span></td>
                        <td style="font-size:11px; max-width:240px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">${escapeHtml(t.user_comment || '—')}</td>
                        <td class="font-mono text-muted" style="font-size:11px;">${formatDateRu(t.created_at)}</td>
                        <td>
                            <button class="btn btn-secondary btn-sm" onclick="openTicketFromAccountModal(${t.id})">Открыть</button>
                        </td>
                    </tr>
                `;
            }).join('');
        }

        // Sub-Tab 4: Bans
        const bansTbody = document.getElementById('acc-tbody-bans');
        if (bans.length === 0) {
            bansTbody.innerHTML = '<tr><td colspan="5" class="text-secondary" style="text-align: center;">Нет активных блокировок</td></tr>';
        } else {
            bansTbody.innerHTML = bans.map(b => `
                <tr>
                    <td class="font-mono" style="font-size:11px;">${escapeHtml(b.target_type)}: ${escapeHtml(b.target_value)}</td>
                    <td style="font-size:11px; color:#f87171;">${escapeHtml(b.reason)}</td>
                    <td class="font-mono text-muted" style="font-size:11px;">${escapeHtml(b.banned_by)}</td>
                    <td class="font-mono text-muted" style="font-size:11px;">${b.expires_at ? formatDateRu(b.expires_at) : 'Бессрочно'}</td>
                    <td class="font-mono text-muted" style="font-size:11px;">${formatDateRu(b.created_at)}</td>
                </tr>
            `).join('');
        }

    } catch (err) {
        showToast(`Ошибка открытия карточки: ${err.message}`, 'error');
        closeAccountModal();
    }
}

function closeAccountModal() {
    const modal = document.getElementById('modal-account');
    if (modal) modal.style.display = 'none';
}

function copyAccountModalNumber() {
    if (!currentModalAccount) return;
    navigator.clipboard.writeText(currentModalAccount);
    showToast(`Номер скопирован: ${currentModalAccount}`, 'success');
}

function switchAccountSubTab(tabName) {
    document.querySelectorAll('.account-tab-btn').forEach(b => b.classList.remove('active'));
    document.querySelectorAll('.account-tab-pane').forEach(p => p.classList.remove('active'));

    const btn = document.getElementById(`acc-subtab-btn-${tabName}`);
    if (btn) btn.classList.add('active');

    const pane = document.getElementById(`acc-subtab-${tabName}`);
    if (pane) pane.classList.add('active');
}

function openGrantModalForAccount() {
    if (!currentModalAccount) return;
    closeAccountModal();
    switchAdminTab('monetization');
    const input = document.getElementById('grant-account-number');
    if (input) input.value = currentModalAccount;
}

function openNotificationForAccount() {
    if (!currentModalAccount) return;
    closeAccountModal();
    switchAdminTab('notifications');
    const typeSelect = document.getElementById('notif-target-type');
    if (typeSelect) {
        typeSelect.value = 'account';
        typeSelect.dispatchEvent(new Event('change'));
    }
    const targetInput = document.getElementById('notif-target-id');
    if (targetInput) targetInput.value = currentModalAccount;
}

function openTicketFromAccountModal(ticketId) {
    closeAccountModal();
    switchAdminTab('support');
    setTimeout(() => {
        openTicketDetail(ticketId);
    }, 200);
}

async function kickModalAccountSession() {
    if (!currentModalAccount) return;
    try {
        const data = await fetchJSON('/api/v1/admin/accounts/kick', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ account_number: currentModalAccount })
        });
        if (!data.success) throw new Error(data.error || 'Ошибка сброса сессии');
        showToast('Сессия игрока успешно сброшена', 'success');
        openAccountCard(currentModalAccount);
    } catch (err) {
        showToast(err.message, 'error');
    }
}

async function banModalAccount() {
    if (!currentModalAccount) return;
    const reason = prompt('Укажите причину блокировки:', 'Нарушение правил сервиса WarLink');
    if (reason === null) return;
    const duration = prompt('Срок блокировки (1h / 24h / 7d / perm):', '24h');
    if (duration === null) return;

    try {
        const data = await fetchJSON('/api/v1/admin/accounts/ban', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                account_number: currentModalAccount,
                reason: reason.trim() || 'Нарушение правил сервиса WarLink',
                duration: duration.trim() || '24h'
            })
        });
        if (!data.success) throw new Error(data.error || 'Ошибка блокировки');
        showToast('Аккаунт заблокирован', 'success');
        openAccountCard(currentModalAccount);
    } catch (err) {
        showToast(err.message, 'error');
    }
}

async function unbanModalAccount() {
    if (!currentModalAccount) return;
    try {
        const data = await fetchJSON('/api/v1/admin/accounts/unban', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ account_number: currentModalAccount })
        });
        if (!data.success) throw new Error(data.error || 'Ошибка разблокировки');
        showToast('Аккаунт успешно разблокирован', 'success');
        openAccountCard(currentModalAccount);
    } catch (err) {
        showToast(err.message, 'error');
    }
}

// =============================================================================
// Static Asset & CDN Manager
// =============================================================================

let currentStaticPath = '';
let staticFilesData = [];
let staticFoldersData = [];
let staticCdnBaseUrl = 'https://static-warlink.max-alekseyev.com/static';
let currentStaticViewMode = 'grid'; // 'grid' | 'list'
let selectedStaticPaths = new Set();
let activePreviewItem = null;
let staticDropzoneInitialized = false;

function formatBytes(bytes, decimals = 1) {
    if (!bytes || bytes === 0) return '0 B';
    const k = 1024;
    const dm = decimals < 0 ? 0 : decimals;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
}

async function loadStaticManager(path = currentStaticPath) {
    currentStaticPath = path || '';
    selectedStaticPaths.clear();
    updateStaticBulkActions();

    const sortBy = document.getElementById('static-sort-by') ? document.getElementById('static-sort-by').value : 'name_asc';
    const summaryEl = document.getElementById('static-summary-text');
    if (summaryEl) summaryEl.textContent = 'Загрузка ассетов...';

    initStaticDropzone();

    try {
        const data = await fetchJSON(`/api/v1/admin/static/files?path=${encodeURIComponent(currentStaticPath)}&sort=${encodeURIComponent(sortBy)}`);
        if (!data.success) throw new Error(data.error || 'Ошибка загрузки файлов');

        staticFilesData = data.files || [];
        staticFoldersData = data.folders || [];
        staticCdnBaseUrl = data.cdn_base_url || 'https://static-warlink.max-alekseyev.com/static';

        renderStaticBreadcrumbs(data.breadcrumbs || []);
        renderStaticFolders(data.folders || []);
        renderStaticGallery();

        if (summaryEl) {
            const count = staticFilesData.length;
            const size = formatBytes(data.total_size || 0);
            summaryEl.textContent = `Папка: /${currentStaticPath} — ${count} файлов (${size})`;
        }
    } catch (err) {
        showToast(err.message, 'error');
        if (summaryEl) summaryEl.textContent = 'Ошибка загрузки каталога';
    }
}

function renderStaticBreadcrumbs(crumbs) {
    const container = document.getElementById('static-breadcrumbs');
    if (!container) return;
    container.innerHTML = '';

    crumbs.forEach((crumb, idx) => {
        if (idx > 0) {
            const sep = document.createElement('span');
            sep.className = 'breadcrumb-sep';
            sep.textContent = '/';
            container.appendChild(sep);
        }

        const isLast = idx === crumbs.length - 1;
        const item = document.createElement('span');
        item.className = `breadcrumb-item ${isLast ? 'active' : ''}`;
        item.textContent = crumb.name || 'Корень';
        if (!isLast) {
            item.onclick = () => navigateToStaticPath(crumb.path);
        }
        container.appendChild(item);
    });
}

function navigateToStaticPath(path) {
    currentStaticPath = path || '';
    loadStaticManager(currentStaticPath);
}

function refreshStaticList() {
    loadStaticManager(currentStaticPath);
}

function renderStaticFolders(folders) {
    const container = document.getElementById('static-folders-container');
    if (!container) return;

    if (!folders || folders.length === 0) {
        container.style.display = 'none';
        container.innerHTML = '';
        return;
    }

    container.style.display = 'grid';
    container.innerHTML = '';

    folders.forEach(f => {
        const card = document.createElement('div');
        card.className = 'folder-card';
        card.onclick = () => navigateToStaticPath(f.path);

        const countText = f.item_count !== undefined ? `${f.item_count} файлов` : '';

        card.innerHTML = `
            <div class="folder-card-left">
                <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="stroke: var(--accent); flex-shrink: 0;"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
                <span class="folder-card-name" title="${escapeHtml(f.name)}">${escapeHtml(f.name)}</span>
            </div>
            <span class="folder-card-count">${countText}</span>
        `;
        container.appendChild(card);
    });
}

function renderStaticGallery() {
    const searchVal = (document.getElementById('static-search-input') ? document.getElementById('static-search-input').value : '').toLowerCase().trim();
    const filterExt = document.getElementById('static-filter-ext') ? document.getElementById('static-filter-ext').value : 'all';

    const filtered = staticFilesData.filter(f => {
        if (searchVal && !f.name.toLowerCase().includes(searchVal)) return false;
        if (filterExt === 'images' && !f.is_image) return false;
        if (filterExt.startsWith('.') && f.ext !== filterExt) return false;
        return true;
    });

    const gridEl = document.getElementById('static-gallery-grid');
    const listEl = document.getElementById('static-gallery-list');
    const tbody = document.getElementById('static-tbody-files');

    if (currentStaticViewMode === 'grid') {
        if (gridEl) gridEl.style.display = 'grid';
        if (listEl) listEl.style.display = 'none';

        if (filtered.length === 0) {
            gridEl.innerHTML = `
                <div style="grid-column: 1 / -1; padding: 40px 16px; text-align: center; color: var(--text-muted); background: var(--bg-card); border: 1px dashed var(--border-card); border-radius: var(--radius-sm);">
                    <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" style="stroke: var(--text-muted); margin-bottom: 8px;"><rect x="3" y="3" width="18" height="18" rx="2" ry="2"/><circle cx="8.5" cy="8.5" r="1.5"/><polyline points="21 15 16 10 5 21"/></svg>
                    <div>В текущей директории нет файлов, соответствующих фильтру</div>
                </div>
            `;
            return;
        }

        gridEl.innerHTML = '';
        filtered.forEach(f => {
            const card = document.createElement('div');
            card.className = 'static-card';

            const extUpper = (f.ext || '').replace('.', '').toUpperCase() || 'FILE';
            const dimText = (f.width && f.height) ? `${f.width}×${f.height}` : (f.ext === '.svg' ? 'SVG' : 'Файл');
            const sizeText = formatBytes(f.size);

            let thumbContent = '';
            if (f.is_image) {
                thumbContent = `<img class="static-thumb-img" src="${escapeHtml(f.url)}" alt="${escapeHtml(f.name)}" loading="lazy" onerror="this.src=''; this.alt='Ошибка загрузки';">`;
            } else {
                thumbContent = `<svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" style="stroke: var(--text-muted);"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>`;
            }

            card.innerHTML = `
                <div class="static-thumb-wrap">
                    <span class="static-card-ext">${extUpper}</span>
                    ${thumbContent}
                    <div class="static-card-overlay">
                        <button class="btn btn-primary btn-sm" onclick="event.stopPropagation(); copyStaticURL('${escapeHtml(f.url)}', this)">
                            <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                            Копировать URL
                        </button>
                        <div style="display: flex; gap: 4px;">
                            <button class="btn btn-secondary btn-sm" onclick="event.stopPropagation(); openStaticPreviewModalByPath('${escapeHtml(f.path)}')">
                                Просмотр
                            </button>
                            <button class="btn btn-danger btn-sm" onclick="event.stopPropagation(); deleteStaticFile('${escapeHtml(f.path)}')">
                                <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
                            </button>
                        </div>
                    </div>
                </div>
                <div class="static-card-info" onclick="openStaticPreviewModalByPath('${escapeHtml(f.path)}')">
                    <span class="static-card-title" title="${escapeHtml(f.name)}">${escapeHtml(f.name)}</span>
                    <div class="static-card-meta">
                        <span>${dimText}</span>
                        <span>${sizeText}</span>
                    </div>
                </div>
            `;
            gridEl.appendChild(card);
        });
    } else {
        if (gridEl) gridEl.style.display = 'none';
        if (listEl) listEl.style.display = 'block';

        if (!tbody) return;
        tbody.innerHTML = '';

        if (filtered.length === 0) {
            tbody.innerHTML = `<tr><td colspan="7" class="text-secondary" style="text-align: center; padding: 24px;">Нет файлов в текущей папке</td></tr>`;
            return;
        }

        filtered.forEach(f => {
            const tr = document.createElement('tr');
            const dimText = (f.width && f.height) ? `${f.width}×${f.height}` : '—';
            const sizeText = formatBytes(f.size);
            const dateText = f.mod_time ? new Date(f.mod_time).toLocaleString('ru-RU', { day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit' }) : '—';

            let thumbIcon = '';
            if (f.is_image) {
                thumbIcon = `<div style="width: 28px; height: 28px; background: repeating-conic-gradient(#181818 0% 25%, #121212 0% 50%) 50% / 8px 8px; border: 1px solid var(--border-card); display: flex; align-items: center; justify-content: center; border-radius: 2px; overflow: hidden;"><img src="${escapeHtml(f.url)}" style="max-width: 100%; max-height: 100%; object-fit: contain;"></div>`;
            } else {
                thumbIcon = `<div style="width: 28px; height: 28px; background: var(--bg-card); border: 1px solid var(--border-card); display: flex; align-items: center; justify-content: center; border-radius: 2px;"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" style="stroke: var(--text-muted);"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg></div>`;
            }

            const isChecked = selectedStaticPaths.has(f.path) ? 'checked' : '';

            tr.innerHTML = `
                <td><input type="checkbox" ${isChecked} onchange="toggleSelectStaticFile('${escapeHtml(f.path)}', this.checked)"></td>
                <td>${thumbIcon}</td>
                <td>
                    <span class="font-mono" style="font-size: 12px; font-weight: 500; cursor: pointer; color: var(--text-link);" onclick="openStaticPreviewModalByPath('${escapeHtml(f.path)}')">${escapeHtml(f.name)}</span>
                </td>
                <td class="font-mono text-secondary" style="font-size: 11px;">${dimText}</td>
                <td class="font-mono text-secondary" style="font-size: 11px;">${sizeText}</td>
                <td class="font-mono text-secondary" style="font-size: 11px;">${dateText}</td>
                <td style="text-align: right;">
                    <div style="display: flex; gap: 4px; justify-content: flex-end;">
                        <button class="btn btn-secondary btn-sm" onclick="copyStaticURL('${escapeHtml(f.url)}', this)" title="Скопировать CDN URL">
                            <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                        </button>
                        <button class="btn btn-secondary btn-sm" onclick="openStaticPreviewModalByPath('${escapeHtml(f.path)}')" title="Просмотр">
                            <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
                        </button>
                        <button class="btn btn-secondary btn-sm" onclick="openRenameStaticModal('${escapeHtml(f.path)}', '${escapeHtml(f.name)}')" title="Переименовать">
                            <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
                        </button>
                        <button class="btn btn-danger btn-sm" onclick="deleteStaticFile('${escapeHtml(f.path)}')" title="Удалить">
                            <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
                        </button>
                    </div>
                </td>
            `;
            tbody.appendChild(tr);
        });
    }
}

function setStaticViewMode(mode) {
    currentStaticViewMode = mode;
    const btnGrid = document.getElementById('btn-view-grid');
    const btnList = document.getElementById('btn-view-list');
    if (btnGrid) btnGrid.classList.toggle('active', mode === 'grid');
    if (btnList) btnList.classList.toggle('active', mode === 'list');
    renderStaticGallery();
}

function filterStaticGallery() {
    renderStaticGallery();
}

function changeStaticSort() {
    loadStaticManager(currentStaticPath);
}

function copyStaticURL(url, btn) {
    if (!url) return;
    navigator.clipboard.writeText(url).then(() => {
        showToast('CDN URL скопирован в буфер', 'success');
        if (btn) {
            const originalHTML = btn.innerHTML;
            btn.innerHTML = `<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="stroke: #10b981;"><polyline points="20 6 9 17 4 12"/></svg> Скопировано!`;
            setTimeout(() => {
                btn.innerHTML = originalHTML;
            }, 1500);
        }
    }).catch(err => {
        showToast('Не удалось скопировать URL: ' + err.message, 'error');
    });
}

function copyStaticInputVal(inputId, btn) {
    const input = document.getElementById(inputId);
    if (!input || !input.value) return;
    navigator.clipboard.writeText(input.value).then(() => {
        showToast('Скопировано в буфер обмена', 'success');
        if (btn) {
            const originalHTML = btn.innerHTML;
            btn.innerHTML = `<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="stroke: #10b981;"><polyline points="20 6 9 17 4 12"/></svg> Скопировано!`;
            setTimeout(() => {
                btn.innerHTML = originalHTML;
            }, 1500);
        }
    }).catch(err => {
        showToast('Ошибка копирования: ' + err.message, 'error');
    });
}

function toggleSelectStaticFile(path, isSelected) {
    if (isSelected) {
        selectedStaticPaths.add(path);
    } else {
        selectedStaticPaths.delete(path);
    }
    updateStaticBulkActions();
}

function toggleSelectAllStaticFiles(masterCheckbox) {
    const isChecked = masterCheckbox.checked;
    staticFilesData.forEach(f => {
        if (isChecked) {
            selectedStaticPaths.add(f.path);
        } else {
            selectedStaticPaths.delete(f.path);
        }
    });
    renderStaticGallery();
    updateStaticBulkActions();
}

function updateStaticBulkActions() {
    const bulkWrap = document.getElementById('static-bulk-actions');
    const countEl = document.getElementById('static-selected-count');
    if (!bulkWrap || !countEl) return;

    if (selectedStaticPaths.size > 0) {
        bulkWrap.style.display = 'flex';
        countEl.textContent = `Выбрано: ${selectedStaticPaths.size}`;
    } else {
        bulkWrap.style.display = 'none';
    }
}

async function deleteSelectedStaticFiles() {
    if (selectedStaticPaths.size === 0) return;
    if (!confirm(`Удалить выбранные файлы (${selectedStaticPaths.size} шт.)? Это действие необратимо.`)) return;

    try {
        const data = await fetchJSON('/api/v1/admin/static/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ paths: Array.from(selectedStaticPaths) })
        });
        if (!data.success) throw new Error(data.error || 'Ошибка удаления');
        showToast(`Удалено файлов: ${data.deleted_count}`, 'success');
        selectedStaticPaths.clear();
        loadStaticManager(currentStaticPath);
    } catch (err) {
        showToast(err.message, 'error');
    }
}

// -----------------------------------------------------------------------------
// Drag & Drop and Upload Pipeline
// -----------------------------------------------------------------------------

function initStaticDropzone() {
    if (staticDropzoneInitialized) return;
    const dropzone = document.getElementById('static-dropzone');
    if (!dropzone) return;

    ['dragenter', 'dragover'].forEach(eventName => {
        dropzone.addEventListener(eventName, (e) => {
            e.preventDefault();
            e.stopPropagation();
            dropzone.classList.add('dragover');
        }, false);
    });

    ['dragleave', 'drop'].forEach(eventName => {
        dropzone.addEventListener(eventName, (e) => {
            e.preventDefault();
            e.stopPropagation();
            dropzone.classList.remove('dragover');
        }, false);
    });

    dropzone.addEventListener('drop', (e) => {
        const files = e.dataTransfer.files;
        if (files && files.length > 0) {
            handleStaticFilesUpload(files);
        }
    });

    dropzone.addEventListener('click', (e) => {
        if (e.target.closest('.dropzone-options')) return;
        const fileInput = document.getElementById('static-file-input');
        if (fileInput) fileInput.click();
    });

    staticDropzoneInitialized = true;
}

function triggerStaticUpload() {
    const fileInput = document.getElementById('static-file-input');
    if (fileInput) fileInput.click();
}

function handleStaticFileSelect(event) {
    const files = event.target.files;
    if (files && files.length > 0) {
        handleStaticFilesUpload(files);
    }
    event.target.value = '';
}

async function convertImageFileToWebP(file) {
    return new Promise((resolve) => {
        if (!file.type.startsWith('image/') || file.type === 'image/webp' || file.type === 'image/svg+xml') {
            return resolve(file);
        }
        const img = new Image();
        const objectUrl = URL.createObjectURL(file);
        img.onload = () => {
            URL.revokeObjectURL(objectUrl);
            const canvas = document.createElement('canvas');
            canvas.width = img.naturalWidth;
            canvas.height = img.naturalHeight;
            const ctx = canvas.getContext('2d');
            ctx.drawImage(img, 0, 0);
            canvas.toBlob((blob) => {
                if (!blob) return resolve(file);
                const newName = file.name.replace(/\.[^/.]+$/, "") + ".webp";
                const webpFile = new File([blob], newName, { type: 'image/webp' });
                resolve(webpFile);
            }, 'image/webp', 0.92);
        };
        img.onerror = () => {
            URL.revokeObjectURL(objectUrl);
            resolve(file);
        };
        img.src = objectUrl;
    });
}

async function handleStaticFilesUpload(rawFiles) {
    if (!rawFiles || rawFiles.length === 0) return;

    const progressWrap = document.getElementById('static-upload-progress-wrap');
    const progressBar = document.getElementById('static-upload-progress-fill');
    const percentText = document.getElementById('static-upload-percent-text');
    const statusText = document.getElementById('static-upload-status-text');

    const shouldConvert = document.getElementById('static-opt-convert-webp') ? document.getElementById('static-opt-convert-webp').checked : false;
    const shouldOverwrite = document.getElementById('static-opt-overwrite') ? document.getElementById('static-opt-overwrite').checked : false;

    if (progressWrap) progressWrap.style.display = 'block';
    if (progressBar) progressBar.style.width = '0%';
    if (percentText) percentText.textContent = '0%';
    if (statusText) statusText.textContent = `Подготовка ${rawFiles.length} файлов к отправке...`;

    const processedFiles = [];
    for (let i = 0; i < rawFiles.length; i++) {
        const f = rawFiles[i];
        if (shouldConvert) {
            const converted = await convertImageFileToWebP(f);
            processedFiles.push(converted);
        } else {
            processedFiles.push(f);
        }
    }

    if (statusText) statusText.textContent = `Загрузка ${processedFiles.length} файлов в /${currentStaticPath}...`;

    const formData = new FormData();
    formData.append('path', currentStaticPath);
    formData.append('overwrite', shouldOverwrite ? 'true' : 'false');

    for (let i = 0; i < processedFiles.length; i++) {
        formData.append('files', processedFiles[i]);
    }

    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/api/v1/admin/static/upload', true);

    xhr.upload.onprogress = (e) => {
        if (e.lengthComputable) {
            const percent = Math.round((e.loaded / e.total) * 100);
            if (progressBar) progressBar.style.width = percent + '%';
            if (percentText) percentText.textContent = percent + '%';
        }
    };

    xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) {
            try {
                const res = JSON.parse(xhr.responseText);
                if (res.success) {
                    showToast(`Успешно загружено файлов: ${res.count || processedFiles.length}`, 'success');
                    setTimeout(() => {
                        if (progressWrap) progressWrap.style.display = 'none';
                        loadStaticManager(currentStaticPath);
                    }, 500);
                } else {
                    throw new Error(res.error || 'Ошибка загрузки');
                }
            } catch (err) {
                showToast(err.message, 'error');
                if (progressWrap) progressWrap.style.display = 'none';
            }
        } else {
            showToast(`Ошибка сервера (${xhr.status})`, 'error');
            if (progressWrap) progressWrap.style.display = 'none';
        }
    };

    xhr.onerror = () => {
        showToast('Сетевая ошибка при загрузке файлов', 'error');
        if (progressWrap) progressWrap.style.display = 'none';
    };

    xhr.send(formData);
}

// -----------------------------------------------------------------------------
// Modals and Asset Inspector
// -----------------------------------------------------------------------------

function openStaticPreviewModalByPath(path) {
    const item = staticFilesData.find(f => f.path === path);
    if (!item) return;
    openStaticPreviewModal(item);
}

function openStaticPreviewModal(item) {
    activePreviewItem = item;
    const modal = document.getElementById('modal-static-preview');
    if (!modal) return;

    const titleEl = document.getElementById('static-preview-title');
    const badgeEl = document.getElementById('static-preview-ext-badge');
    const imgEl = document.getElementById('static-preview-img');
    const dimEl = document.getElementById('static-preview-dimensions');
    const sizeEl = document.getElementById('static-preview-size');
    const formatEl = document.getElementById('static-preview-format');
    const dateEl = document.getElementById('static-preview-date');
    const urlInput = document.getElementById('static-preview-url-input');
    const pathInput = document.getElementById('static-preview-path-input');
    const downloadBtn = document.getElementById('static-preview-download-btn');

    if (titleEl) titleEl.textContent = item.name;
    if (badgeEl) badgeEl.textContent = (item.ext || '').replace('.', '').toUpperCase() || 'FILE';

    if (imgEl) {
        if (item.is_image) {
            imgEl.style.display = 'block';
            imgEl.src = item.url;
        } else {
            imgEl.style.display = 'none';
            imgEl.src = '';
        }
    }

    if (dimEl) dimEl.textContent = (item.width && item.height) ? `${item.width} × ${item.height} px` : (item.ext === '.svg' ? 'Векторный SVG' : '—');
    if (sizeEl) sizeEl.textContent = formatBytes(item.size);
    if (formatEl) formatEl.textContent = (item.ext || '').toUpperCase();
    if (dateEl) dateEl.textContent = item.mod_time ? new Date(item.mod_time).toLocaleString('ru-RU') : '—';

    if (urlInput) urlInput.value = item.url;
    if (pathInput) pathInput.value = item.path;
    if (downloadBtn) {
        downloadBtn.href = item.url;
        downloadBtn.download = item.name;
    }

    modal.style.display = 'flex';
}

function closeStaticPreviewModal() {
    const modal = document.getElementById('modal-static-preview');
    if (modal) modal.style.display = 'none';
    activePreviewItem = null;
}

function openNewFolderModal() {
    const modal = document.getElementById('modal-static-mkdir');
    if (!modal) return;

    const parentInput = document.getElementById('static-mkdir-parent');
    const nameInput = document.getElementById('static-mkdir-name');

    if (parentInput) parentInput.value = currentStaticPath ? `/${currentStaticPath}` : '/ (Корень)';
    if (nameInput) {
        nameInput.value = '';
        setTimeout(() => nameInput.focus(), 50);
    }

    modal.style.display = 'flex';
}

function closeNewFolderModal() {
    const modal = document.getElementById('modal-static-mkdir');
    if (modal) modal.style.display = 'none';
}

async function submitNewFolder() {
    const nameInput = document.getElementById('static-mkdir-name');
    if (!nameInput) return;
    const name = nameInput.value.trim();
    if (!name) return showToast('Укажите название папки', 'error');

    try {
        const data = await fetchJSON('/api/v1/admin/static/mkdir', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ path: currentStaticPath, name })
        });
        if (!data.success) throw new Error(data.error || 'Ошибка создания папки');
        showToast(`Папка ${data.name} успешно создана`, 'success');
        closeNewFolderModal();
        loadStaticManager(currentStaticPath);
    } catch (err) {
        showToast(err.message, 'error');
    }
}

let staticRenameTargetPath = '';

function openRenameStaticModal(path, currentName) {
    staticRenameTargetPath = path;
    const modal = document.getElementById('modal-static-rename');
    if (!modal) return;

    const oldInput = document.getElementById('static-rename-old');
    const newInput = document.getElementById('static-rename-new');

    if (oldInput) oldInput.value = currentName;
    if (newInput) {
        newInput.value = currentName;
        setTimeout(() => newInput.focus(), 50);
    }

    modal.style.display = 'flex';
}

function closeRenameStaticModal() {
    const modal = document.getElementById('modal-static-rename');
    if (modal) modal.style.display = 'none';
    staticRenameTargetPath = '';
}

async function submitRenameStatic() {
    const newInput = document.getElementById('static-rename-new');
    if (!newInput || !staticRenameTargetPath) return;
    const newName = newInput.value.trim();
    if (!newName) return showToast('Укажите новое имя', 'error');

    try {
        const data = await fetchJSON('/api/v1/admin/static/rename', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ old_path: staticRenameTargetPath, new_name: newName })
        });
        if (!data.success) throw new Error(data.error || 'Ошибка переименования');
        showToast('Файл успешно переименован', 'success');
        closeRenameStaticModal();
        loadStaticManager(currentStaticPath);
    } catch (err) {
        showToast(err.message, 'error');
    }
}

async function deleteStaticFile(path) {
    if (!confirm(`Удалить файл "${path}"? Это действие необратимо.`)) return;
    try {
        const data = await fetchJSON('/api/v1/admin/static/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ paths: [path] })
        });
        if (!data.success) throw new Error(data.error || 'Ошибка удаления');
        showToast('Файл успешно удален', 'success');
        loadStaticManager(currentStaticPath);
    } catch (err) {
        showToast(err.message, 'error');
    }
}

function deleteCurrentStaticPreviewFile() {
    if (!activePreviewItem) return;
    const path = activePreviewItem.path;
    closeStaticPreviewModal();
    deleteStaticFile(path);
}

function renameCurrentStaticPreviewFile() {
    if (!activePreviewItem) return;
    const path = activePreviewItem.path;
    const name = activePreviewItem.name;
    closeStaticPreviewModal();
    openRenameStaticModal(path, name);
}

function replaceCurrentStaticPreviewFile() {
    if (!activePreviewItem) return;
    const input = document.getElementById('static-replace-file-input');
    if (input) input.click();
}

async function handleStaticReplaceSelected(event) {
    const file = event.target.files[0];
    event.target.value = '';
    if (!file || !activePreviewItem) return;

    const targetPath = activePreviewItem.path;
    const parentDir = targetPath.includes('/') ? targetPath.substring(0, targetPath.lastIndexOf('/')) : '';

    const formData = new FormData();
    formData.append('path', parentDir);
    formData.append('overwrite', 'true');

    const replacementFile = new File([file], activePreviewItem.name, { type: file.type });
    formData.append('files', replacementFile);

    try {
        const res = await fetch('/api/v1/admin/static/upload', {
            method: 'POST',
            body: formData
        });
        const data = await res.json();
        if (!data.success) throw new Error(data.error || 'Ошибка замены файла');
        showToast('Файл успешно заменен', 'success');
        closeStaticPreviewModal();
        loadStaticManager(currentStaticPath);
    } catch (err) {
        showToast(err.message, 'error');
    }
}

// =============================================================================
// TAB 9: Progression Database Editor (Dark Cloudflare Utility)
// =============================================================================

let currentProgressionDB = {
    version: "2.0.0",
    source: "MetaForge.app/wardogs",
    total_unlocks: 0,
    roles: ["career", "assault", "medic", "recon", "support", "driver", "pilot"],
    unlocks: [],
    catalog: []
};

let progActiveRole = "all";
let progActiveType = "";
let progSearchTerm = "";
let progSort = "lvl_asc";
let progEditingIndex = -1;
let progRawJSONVisible = false;
let progIconPickerCache = [];

const PROG_ROLES = [
    { id: "all", label: "Все ветки" },
    { id: "career", label: "Карьера" },
    { id: "assault", label: "Штурмовик" },
    { id: "medic", label: "Медик" },
    { id: "recon", label: "Разведчик" },
    { id: "support", label: "Поддержка" },
    { id: "driver", label: "Водитель" },
    { id: "pilot", label: "Пилот" }
];

async function loadProgressionAdmin(force = false) {
    const statusText = document.getElementById('prog-status-text');
    if (statusText) statusText.textContent = 'Загрузка базы данных прокачки...';

    try {
        const url = '/api/v1/admin/progression/database' + (force ? '?t=' + Date.now() : '');
        const data = await fetchJSON(url);
        if (data && Array.isArray(data.unlocks)) {
            currentProgressionDB = data;
            if (!Array.isArray(currentProgressionDB.roles) || currentProgressionDB.roles.length === 0) {
                currentProgressionDB.roles = ["career", "assault", "medic", "recon", "support", "driver", "pilot"];
            }

            const verBadge = document.getElementById('prog-version-badge');
            if (verBadge) verBadge.textContent = currentProgressionDB.version || 'v2.0.0';

            const totalBadge = document.getElementById('prog-total-badge');
            if (totalBadge) totalBadge.textContent = `${currentProgressionDB.unlocks.length} наград`;

            renderProgRoleTabs();
            renderProgTable();
        } else {
            throw new Error('Некорректный формат базы данных прогрессии');
        }
    } catch (err) {
        console.error('Failed to load progression database:', err);
        showToast(err.message || 'Ошибка загрузки базы прокачки', 'error');
        if (statusText) statusText.textContent = 'Ошибка загрузки базы прокачки';
    }
}

function renderProgRoleTabs() {
    const container = document.getElementById('prog-role-tabs-container');
    if (!container) return;

    container.innerHTML = PROG_ROLES.map(r => {
        const isActive = r.id === progActiveRole;
        let count = 0;
        if (currentProgressionDB && Array.isArray(currentProgressionDB.unlocks)) {
            if (r.id === 'all') {
                count = currentProgressionDB.unlocks.length;
            } else {
                count = currentProgressionDB.unlocks.filter(it => (it.role || '').toLowerCase() === r.id).length;
            }
        }
        return `
            <button type="button" class="prog-role-btn ${isActive ? 'active' : ''}" onclick="setProgRole('${r.id}')">
                <span>${r.label}</span>
                <span style="opacity: 0.7; font-size: 10px; margin-left: 2px;">(${count})</span>
            </button>
        `;
    }).join('');
}

function setProgRole(role) {
    progActiveRole = role;
    renderProgRoleTabs();
    renderProgTable();
}

function onProgSearchChange() {
    const input = document.getElementById('prog-search-input');
    progSearchTerm = input ? input.value.trim().toLowerCase() : '';
    renderProgTable();
}

function onProgFilterChange() {
    const select = document.getElementById('prog-type-select');
    progActiveType = select ? select.value : '';
    renderProgTable();
}

function onProgSortChange() {
    const select = document.getElementById('prog-sort-select');
    progSort = select ? select.value : 'lvl_asc';
    renderProgTable();
}

function normalizeProgIconURL(icon) {
    if (!icon) return 'data:image/svg+xml,%3Csvg xmlns="http://www.w3.org/2000/svg" width="36" height="36"%3E%3Crect width="36" height="36" fill="%231a1a1a"/%3E%3C/svg%3E';
    if (icon.startsWith('http://') || icon.startsWith('https://')) return icon;
    if (icon.startsWith('data:')) return icon;

    let clean = icon.trim();
    if (clean.startsWith('/')) clean = clean.substring(1);

    if (clean.startsWith('guides/icons/')) {
        clean = 'static/wardogs/items/' + clean.slice(13);
    } else if (clean.startsWith('guides/images/')) {
        clean = 'static/wardogs/renders/' + clean.slice(14);
    } else if (clean.startsWith('wardogs/')) {
        clean = 'static/' + clean;
    } else if (!clean.startsWith('static/')) {
        clean = 'static/wardogs/items/' + clean;
    }

    return 'https://static-warlink.max-alekseyev.com/' + clean;
}

function renderProgTable() {
    const tbody = document.getElementById('prog-items-tbody');
    const statusText = document.getElementById('prog-status-text');
    if (!tbody) return;

    if (!currentProgressionDB || !Array.isArray(currentProgressionDB.unlocks)) {
        tbody.innerHTML = `<tr><td colspan="8" style="text-align: center; padding: 30px; color: var(--text-muted);">База данных пуста</td></tr>`;
        return;
    }

    let items = currentProgressionDB.unlocks.map((item, originalIndex) => ({
        ...item,
        _originalIndex: originalIndex
    }));

    // Filter by role
    if (progActiveRole !== 'all') {
        items = items.filter(it => (it.role || '').toLowerCase() === progActiveRole);
    }

    // Filter by type
    if (progActiveType) {
        items = items.filter(it => {
            const tab = (it.tab || '').toLowerCase();
            const sub = (it.subcategory || '').toLowerCase();
            return tab.includes(progActiveType) || sub.includes(progActiveType);
        });
    }

    // Search query
    if (progSearchTerm) {
        items = items.filter(it => {
            const name = (it.name || '').toLowerCase();
            const nameRu = (it.name_ru || '').toLowerCase();
            const uid = (it.unlock_id || '').toLowerCase();
            const slug = (it.slug || '').toLowerCase();
            const catRu = (it.category_ru || '').toLowerCase();
            const subcat = (it.subcategory || '').toLowerCase();
            return name.includes(progSearchTerm) ||
                   nameRu.includes(progSearchTerm) ||
                   uid.includes(progSearchTerm) ||
                   slug.includes(progSearchTerm) ||
                   catRu.includes(progSearchTerm) ||
                   subcat.includes(progSearchTerm);
        });
    }

    // Sort
    items.sort((a, b) => {
        switch (progSort) {
            case 'lvl_asc':
                return (a.level || 0) - (b.level || 0);
            case 'lvl_desc':
                return (b.level || 0) - (a.level || 0);
            case 'price_asc':
                return (a.price || 0) - (b.price || 0);
            case 'price_desc':
                return (b.price || 0) - (a.price || 0);
            case 'name_asc':
                return (a.name_ru || a.name || '').localeCompare(b.name_ru || b.name || '');
            default:
                return (a.level || 0) - (b.level || 0);
        }
    });

    if (statusText) {
        statusText.textContent = `Наград в выборке: ${items.length} из ${currentProgressionDB.unlocks.length}`;
    }

    if (items.length === 0) {
        tbody.innerHTML = `<tr><td colspan="8" style="text-align: center; padding: 40px; color: var(--text-muted); font-family: var(--font-mono); font-size: 11px;">Наград по заданным критериям фильтра не найдено</td></tr>`;
        return;
    }

    const rowsHtml = items.map(item => {
        const role = (item.role || 'career').toLowerCase();
        const roleLabel = (PROG_ROLES.find(r => r.id === role) || {}).label || role;
        const iconUrl = normalizeProgIconURL(item.icon);
        const nameRu = escapeHtml(item.name_ru || item.name || '—');
        const nameEn = escapeHtml(item.name || '');
        const unlockId = escapeHtml(item.unlock_id || '—');
        const slug = escapeHtml(item.slug || '—');
        const cat = escapeHtml(item.category_ru || item.subcategory || item.tab || 'Разблокировка');
        const price = item.price != null ? Number(item.price).toLocaleString('ru-RU') : '—';
        const unlockCost = item.unlock_cost != null ? Number(item.unlock_cost).toLocaleString('ru-RU') : '—';

        return `
            <tr>
                <td>
                    <span class="prog-level-badge">${item.level != null ? item.level : 0}</span>
                </td>
                <td>
                    <span class="prog-role-tag ${role}">${roleLabel}</span>
                </td>
                <td>
                    <div style="display: flex; align-items: center; gap: 8px;">
                        <img src="${iconUrl}" class="prog-icon-thumb" alt="${nameRu}" loading="lazy" onerror="this.onerror=null; this.src='data:image/svg+xml,%3Csvg xmlns=\\'http://www.w3.org/2000/svg\\' width=\\'36\\' height=\\'36\\'%3E%3Crect width=\\'36\\' height=\\'36\\' fill=\\'%231a1a1a\\'/ %3E%3C/svg%3E'">
                        <div>
                            <div style="font-weight: 600; color: var(--text-primary); font-size: 12px;">${nameRu}</div>
                            ${nameEn && nameEn !== nameRu ? `<div style="font-size: 10px; color: var(--text-muted); font-family: var(--font-mono);">${nameEn}</div>` : ''}
                        </div>
                    </div>
                </td>
                <td>
                    <div class="font-mono" style="font-size: 11px; color: var(--text-primary);">${unlockId}</div>
                    <div class="font-mono" style="font-size: 10px; color: var(--text-muted);">${slug}</div>
                </td>
                <td>
                    <span class="badge badge-outline font-mono" style="font-size: 10px;">${cat}</span>
                </td>
                <td style="text-align: right;">
                    <span class="font-mono" style="font-weight: 600; color: #10b981;">${price} $</span>
                </td>
                <td style="text-align: right;">
                    <span class="font-mono" style="color: var(--accent);">${unlockCost}</span>
                </td>
                <td style="text-align: right;">
                    <div style="display: flex; justify-content: flex-end; gap: 4px;">
                        <button class="btn btn-secondary btn-sm" onclick="openProgEditModal(${item._originalIndex})" title="Редактировать награду" style="padding: 3px 6px;">
                            <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
                        </button>
                        <button class="btn btn-secondary btn-sm" onclick="duplicateProgItem(${item._originalIndex})" title="Дублировать награду" style="padding: 3px 6px;">
                            <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                        </button>
                        <button class="btn btn-danger btn-sm" onclick="deleteProgItem(${item._originalIndex})" title="Удалить награду" style="padding: 3px 6px;">
                            <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
                        </button>
                    </div>
                </td>
            </tr>
        `;
    }).join('');

    tbody.innerHTML = rowsHtml;
}

function openProgAddItemModal() {
    progEditingIndex = -1;
    const title = document.getElementById('modal-prog-title');
    if (title) title.textContent = 'Добавление новой награды';

    const delBtn = document.getElementById('btn-prog-modal-delete');
    if (delBtn) delBtn.style.display = 'none';

    document.getElementById('prog-field-name-ru').value = '';
    document.getElementById('prog-field-name-en').value = '';
    document.getElementById('prog-field-role').value = progActiveRole !== 'all' ? progActiveRole : 'career';
    document.getElementById('prog-field-level').value = '1';
    document.getElementById('prog-field-price').value = '1000';
    document.getElementById('prog-field-unlock-cost').value = '1000';
    document.getElementById('prog-field-unlock-id').value = '';
    document.getElementById('prog-field-slug').value = '';
    document.getElementById('prog-field-tab').value = 'weapons';
    document.getElementById('prog-field-subcategory').value = '';
    document.getElementById('prog-field-category-ru').value = '';
    document.getElementById('prog-field-icon').value = 'static/wardogs/items/';
    document.getElementById('prog-field-description').value = '';

    updateProgIconPreview('static/wardogs/items/');

    const modal = document.getElementById('modal-prog-item');
    if (modal) modal.style.display = 'flex';
}

function openProgEditModal(index) {
    if (!currentProgressionDB || !currentProgressionDB.unlocks || !currentProgressionDB.unlocks[index]) return;
    progEditingIndex = index;
    const item = currentProgressionDB.unlocks[index];

    const title = document.getElementById('modal-prog-title');
    if (title) title.textContent = `Редактирование: ${item.name_ru || item.name || 'Награда'}`;

    const delBtn = document.getElementById('btn-prog-modal-delete');
    if (delBtn) delBtn.style.display = 'inline-flex';

    document.getElementById('prog-field-name-ru').value = item.name_ru || '';
    document.getElementById('prog-field-name-en').value = item.name || '';
    document.getElementById('prog-field-role').value = (item.role || 'career').toLowerCase();
    document.getElementById('prog-field-level').value = item.level != null ? item.level : 0;
    document.getElementById('prog-field-price').value = item.price != null ? item.price : 0;
    document.getElementById('prog-field-unlock-cost').value = item.unlock_cost != null ? item.unlock_cost : 0;
    document.getElementById('prog-field-unlock-id').value = item.unlock_id || '';
    document.getElementById('prog-field-slug').value = item.slug || '';
    document.getElementById('prog-field-tab').value = item.tab || '';
    document.getElementById('prog-field-subcategory').value = item.subcategory || '';
    document.getElementById('prog-field-category-ru').value = item.category_ru || '';
    document.getElementById('prog-field-icon').value = item.icon || '';
    document.getElementById('prog-field-description').value = item.description || '';

    updateProgIconPreview(item.icon || '');

    const modal = document.getElementById('modal-prog-item');
    if (modal) modal.style.display = 'flex';
}

function closeProgItemModal() {
    const modal = document.getElementById('modal-prog-item');
    if (modal) modal.style.display = 'none';
}

function updateProgIconPreview(path) {
    const img = document.getElementById('prog-field-icon-preview');
    if (img) {
        img.src = normalizeProgIconURL(path);
    }
}

function saveProgModalItem() {
    const nameRu = document.getElementById('prog-field-name-ru').value.trim();
    const nameEn = document.getElementById('prog-field-name-en').value.trim();
    const role = document.getElementById('prog-field-role').value.trim().toLowerCase();
    const level = parseInt(document.getElementById('prog-field-level').value, 10) || 0;
    const price = parseInt(document.getElementById('prog-field-price').value, 10) || 0;
    const unlockCost = parseInt(document.getElementById('prog-field-unlock-cost').value, 10) || 0;
    const unlockId = document.getElementById('prog-field-unlock-id').value.trim();
    const slug = document.getElementById('prog-field-slug').value.trim();
    const tab = document.getElementById('prog-field-tab').value.trim();
    const subcategory = document.getElementById('prog-field-subcategory').value.trim();
    const categoryRu = document.getElementById('prog-field-category-ru').value.trim();
    const icon = document.getElementById('prog-field-icon').value.trim();
    const description = document.getElementById('prog-field-description').value;

    if (!nameRu && !nameEn) {
        showToast('Укажите название предмета', 'error');
        return;
    }
    if (!unlockId) {
        showToast('Укажите уникальный Unlock ID (например: Id.Item.TCX_SPR)', 'error');
        return;
    }

    let cleanIcon = icon;
    if (cleanIcon.startsWith('/')) cleanIcon = cleanIcon.substring(1);
    if (cleanIcon.startsWith('wardogs/')) {
        cleanIcon = 'static/' + cleanIcon;
    } else if (cleanIcon.startsWith('items/')) {
        cleanIcon = 'static/wardogs/' + cleanIcon;
    }

    const itemObj = {
        unlock_id: unlockId,
        slug: slug || nameEn.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, ''),
        name: nameEn || nameRu,
        name_ru: nameRu || nameEn,
        role: role,
        level: level,
        total_xp: null,
        tab: tab || 'weapons',
        subcategory: subcategory,
        category_ru: categoryRu || subcategory || 'Разблокировка',
        price: price,
        unlock_cost: unlockCost,
        icon: cleanIcon,
        image: '',
        guide: '',
        description: description,
        specs: {}
    };

    if (progEditingIndex >= 0 && progEditingIndex < currentProgressionDB.unlocks.length) {
        // Keep existing specs or other fields if present
        const oldItem = currentProgressionDB.unlocks[progEditingIndex];
        currentProgressionDB.unlocks[progEditingIndex] = {
            ...oldItem,
            ...itemObj
        };
        showToast(`Награда "${itemObj.name_ru}" обновлена в таблице`, 'success');
    } else {
        currentProgressionDB.unlocks.push(itemObj);
        showToast(`Награда "${itemObj.name_ru}" добавлена в таблицу`, 'success');
    }

    currentProgressionDB.total_unlocks = currentProgressionDB.unlocks.length;
    const totalBadge = document.getElementById('prog-total-badge');
    if (totalBadge) totalBadge.textContent = `${currentProgressionDB.unlocks.length} наград`;

    closeProgItemModal();
    renderProgRoleTabs();
    renderProgTable();
}

function duplicateProgItem(index) {
    if (!currentProgressionDB || !currentProgressionDB.unlocks || !currentProgressionDB.unlocks[index]) return;
    const orig = currentProgressionDB.unlocks[index];
    const copy = JSON.parse(JSON.stringify(orig));

    copy.unlock_id = copy.unlock_id + '_Copy';
    copy.slug = copy.slug + '-copy';
    copy.name_ru = copy.name_ru + ' (Копия)';
    copy.name = copy.name + ' (Copy)';

    currentProgressionDB.unlocks.splice(index + 1, 0, copy);
    currentProgressionDB.total_unlocks = currentProgressionDB.unlocks.length;

    const totalBadge = document.getElementById('prog-total-badge');
    if (totalBadge) totalBadge.textContent = `${currentProgressionDB.unlocks.length} наград`;

    showToast(`Награда скопирована как "${copy.name_ru}"`, 'success');
    renderProgRoleTabs();
    renderProgTable();
}

function deleteProgItem(index) {
    if (!currentProgressionDB || !currentProgressionDB.unlocks || !currentProgressionDB.unlocks[index]) return;
    const item = currentProgressionDB.unlocks[index];
    if (!confirm(`Удалить награду "${item.name_ru || item.name}" (ID: ${item.unlock_id})?`)) return;

    currentProgressionDB.unlocks.splice(index, 1);
    currentProgressionDB.total_unlocks = currentProgressionDB.unlocks.length;

    const totalBadge = document.getElementById('prog-total-badge');
    if (totalBadge) totalBadge.textContent = `${currentProgressionDB.unlocks.length} наград`;

    showToast(`Награда "${item.name_ru || item.name}" удалена из таблицы`, 'success');
    renderProgRoleTabs();
    renderProgTable();
}

function deleteCurrentProgModalItem() {
    if (progEditingIndex >= 0) {
        closeProgItemModal();
        deleteProgItem(progEditingIndex);
    }
}

async function saveProgressionDatabase() {
    const btn = document.getElementById('btn-prog-save');
    const originalHTML = btn ? btn.innerHTML : '';
    if (btn) {
        btn.disabled = true;
        btn.innerHTML = `<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="spin"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg> Сохранение и деплой...`;
    }

    try {
        currentProgressionDB.total_unlocks = currentProgressionDB.unlocks.length;
        if (Array.isArray(currentProgressionDB.catalog)) {
            currentProgressionDB.total_catalog_items = currentProgressionDB.catalog.length;
        }

        const res = await fetchJSON('/api/v1/admin/progression/database', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(currentProgressionDB)
        });

        if (!res.success) {
            throw new Error(res.error || 'Ошибка сохранения на сервере');
        }

        showToast(`База данных прокачки успешно сохранена (${res.total_unlocks} наград) и применена во всех клиентах!`, 'success');
        const saveStatus = document.getElementById('prog-save-status');
        if (saveStatus) {
            saveStatus.textContent = `Синхронизировано: ${new Date().toLocaleTimeString('ru-RU')}`;
        }
    } catch (err) {
        console.error('Failed to save progression database:', err);
        showToast(err.message || 'Ошибка сохранения на сервере', 'error');
    } finally {
        if (btn) {
            btn.disabled = false;
            btn.innerHTML = originalHTML;
        }
    }
}

function toggleProgJSONView() {
    const tableWrap = document.getElementById('prog-table-wrap');
    const jsonWrap = document.getElementById('prog-json-wrap');
    const toggleText = document.getElementById('prog-json-toggle-text');
    const textarea = document.getElementById('prog-raw-json-textarea');

    progRawJSONVisible = !progRawJSONVisible;

    if (progRawJSONVisible) {
        if (tableWrap) tableWrap.style.display = 'none';
        if (jsonWrap) jsonWrap.style.display = 'block';
        if (toggleText) toggleText.textContent = 'Табличный режим';
        if (textarea) textarea.value = JSON.stringify(currentProgressionDB, null, 2);
    } else {
        if (tableWrap) tableWrap.style.display = 'block';
        if (jsonWrap) jsonWrap.style.display = 'none';
        if (toggleText) toggleText.textContent = 'JSON режим';
    }
}

function applyProgRawJSON() {
    const textarea = document.getElementById('prog-raw-json-textarea');
    if (!textarea) return;

    try {
        const parsed = JSON.parse(textarea.value);
        if (!parsed || !Array.isArray(parsed.unlocks)) {
            throw new Error('JSON должен содержать массив "unlocks"');
        }
        currentProgressionDB = parsed;
        currentProgressionDB.total_unlocks = parsed.unlocks.length;

        const totalBadge = document.getElementById('prog-total-badge');
        if (totalBadge) totalBadge.textContent = `${currentProgressionDB.unlocks.length} наград`;

        showToast(`JSON успешно применен в таблицу (${parsed.unlocks.length} наград)`, 'success');
        toggleProgJSONView();
        renderProgRoleTabs();
        renderProgTable();
    } catch (err) {
        showToast('Ошибка синтаксиса JSON: ' + err.message, 'error');
    }
}

function formatProgRawJSON() {
    const textarea = document.getElementById('prog-raw-json-textarea');
    if (!textarea) return;
    try {
        const parsed = JSON.parse(textarea.value);
        textarea.value = JSON.stringify(parsed, null, 2);
        showToast('JSON отформатирован', 'success');
    } catch (err) {
        showToast('Невозможно отформатировать: ошибка в JSON', 'error');
    }
}

function exportProgressionJSON() {
    try {
        const str = JSON.stringify(currentProgressionDB, null, 2);
        const blob = new Blob([str], { type: 'application/json' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `progression_db_${new Date().toISOString().slice(0,10)}.json`;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);
        showToast('Экспорт progression_db.json завершен', 'success');
    } catch (err) {
        showToast('Ошибка экспорта: ' + err.message, 'error');
    }
}

function importProgressionJSON(event) {
    const file = event.target.files && event.target.files[0];
    if (!file) return;

    const reader = new FileReader();
    reader.onload = function(e) {
        try {
            const data = JSON.parse(e.target.result);
            if (!data || !Array.isArray(data.unlocks)) {
                throw new Error('Файл не содержит массив "unlocks"');
            }
            currentProgressionDB = data;
            currentProgressionDB.total_unlocks = data.unlocks.length;

            const totalBadge = document.getElementById('prog-total-badge');
            if (totalBadge) totalBadge.textContent = `${currentProgressionDB.unlocks.length} наград`;

            renderProgRoleTabs();
            renderProgTable();
            showToast(`Успешно импортировано ${data.unlocks.length} наград из файла`, 'success');
        } catch (err) {
            showToast('Ошибка импорта: ' + err.message, 'error');
        }
        event.target.value = '';
    };
    reader.readAsText(file);
}

// -----------------------------------------------------------------------------
// Icon Picker modal integrating with Static CDN
// -----------------------------------------------------------------------------
async function openProgIconPicker() {
    const modal = document.getElementById('modal-prog-icon-picker');
    if (modal) modal.style.display = 'flex';
    await loadProgIconPickerFolder('wardogs/items');
}

function closeProgIconPicker() {
    const modal = document.getElementById('modal-prog-icon-picker');
    if (modal) modal.style.display = 'none';
}

async function loadProgIconPickerFolder(folder) {
    const grid = document.getElementById('prog-icon-picker-grid');
    if (grid) grid.innerHTML = '<div style="grid-column: 1/-1; text-align: center; padding: 20px; color: var(--text-muted); font-family: var(--font-mono); font-size: 11px;">Загрузка списка иконок...</div>';

    try {
        const data = await fetchJSON(`/api/v1/admin/static/files?path=${encodeURIComponent(folder)}`);
        if (data && Array.isArray(data.files)) {
            progIconPickerCache = data.files.filter(f => !f.is_dir && f.is_image);
            renderProgIconPickerGrid(progIconPickerCache);
        } else {
            throw new Error('Не удалось получить список ассетов');
        }
    } catch (err) {
        console.error('Failed to load icon picker files:', err);
        if (grid) grid.innerHTML = `<div style="grid-column: 1/-1; text-align: center; padding: 20px; color: var(--text-muted); font-family: var(--font-mono); font-size: 11px;">Ошибка: ${err.message}</div>`;
    }
}

function filterProgIconPicker() {
    const search = document.getElementById('prog-icon-picker-search').value.trim().toLowerCase();
    const filtered = progIconPickerCache.filter(f => f.name.toLowerCase().includes(search));
    renderProgIconPickerGrid(filtered);
}

function renderProgIconPickerGrid(files) {
    const grid = document.getElementById('prog-icon-picker-grid');
    if (!grid) return;

    if (!files || files.length === 0) {
        grid.innerHTML = '<div style="grid-column: 1/-1; text-align: center; padding: 20px; color: var(--text-muted); font-family: var(--font-mono); font-size: 11px;">Иконки не найдены</div>';
        return;
    }

    grid.innerHTML = files.map(f => {
        const fullRelPath = 'static/' + f.path;
        const imgUrl = f.url || f.cdn_url || ('https://static-warlink.max-alekseyev.com/static/' + f.path);
        return `
            <div class="prog-icon-picker-card" onclick="selectProgIcon('${fullRelPath}')" title="${f.name}">
                <img src="${imgUrl}" alt="${f.name}" loading="lazy" onerror="this.onerror=null; this.src='data:image/svg+xml,%3Csvg xmlns=\\'http://www.w3.org/2000/svg\\' width=\\'48\\' height=\\'48\\'%3E%3Crect width=\\'48\\' height=\\'48\\' fill=\\'%231a1a1a\\'/ %3E%3C/svg%3E'">
                <span class="name">${f.name}</span>
            </div>
        `;
    }).join('');
}

function selectProgIcon(path) {
    const input = document.getElementById('prog-field-icon');
    if (input) {
        input.value = path;
        updateProgIconPreview(path);
    }
    closeProgIconPicker();
    showToast(`Выбрана иконка: ${path.split('/').pop()}`, 'success');
}


