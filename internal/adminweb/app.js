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
    await checkAuthStatus();
});

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

    let hash = rawHash || 'view-special-projects';

    // Handle legacy Grafana links
    if (hash.includes('support_tickets') || hash.includes('dialogs')) hash = 'view-support';
    if (hash.includes('accounts') || hash.includes('sponsors')) hash = 'view-monetization';
    if (hash.includes('nodes') || hash.includes('cluster')) hash = 'view-nodes';

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

