// Progression & Unlock Roadmap Controller (Dark Cloudflare Utility)
(function() {
    'use strict';

    let currentProgression = {
        career_level: 0,
        roles: {
            assault: 0,
            medic: 0,
            recon: 0,
            support: 0,
            driver: 0,
            pilot: 0
        },
        xp_progress: {
            assault: 0,
            medic: 0,
            recon: 0,
            support: 0,
            driver: 0,
            pilot: 0
        },
        wishlist_id: '',
        unlocked_items: []
    };

    let nextUnlocks = {};
    let allUnlocks = [];
    let currentFilterRole = 'career';
    let currentSearchTerm = '';
    let hideUnlocked = false;
    let isInitialized = false;

    const ROLES = ['assault', 'medic', 'recon', 'support', 'driver', 'pilot'];

    const ROLE_LABELS = {
        career: 'КАРЬЕРА',
        assault: 'ШТУРМОВИК',
        medic: 'МЕДИК',
        recon: 'РАЗВЕДЧИК',
        support: 'ПОДДЕРЖКА',
        driver: 'ВОДИТЕЛЬ',
        pilot: 'ПИЛОТ'
    };

    const ROLE_ICONS = {
        assault: 'assets/roles/assault.webp',
        medic: 'assets/roles/medic.webp',
        recon: 'assets/roles/recon.webp',
        support: 'assets/roles/support.webp',
        driver: 'assets/roles/driver.webp',
        pilot: 'assets/roles/pilot.webp',
        career: 'assets/career/1.webp'
    };

    function escapeHtml(text) {
        if (!text) return '';
        return String(text)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;');
    }

    function getCareerBadgePath(level) {
        if (!level || level <= 0) return 'assets/career/1.webp';
        if (level <= 100) return 'assets/career/' + level + '.webp';
        if (level < 125) return 'assets/career/100.webp';
        if (level < 150) return 'assets/career/125.webp';
        if (level < 175) return 'assets/career/150.webp';
        if (level < 200) return 'assets/career/175.webp';
        if (level < 225) return 'assets/career/200.webp';
        if (level < 250) return 'assets/career/225.webp';
        if (level < 275) return 'assets/career/250.webp';
        if (level < 300) return 'assets/career/275.webp';
        return 'assets/career/300.webp';
    }

    function normalizeItemIcon(icon) {
        if (!icon) return 'wardogs_icon.png';
        if (icon.startsWith('http://') || icon.startsWith('https://')) return icon;
        let clean = icon;
        if (clean.startsWith('guides/icons/')) {
            clean = 'static/wardogs/items/' + clean.slice(13);
        } else if (clean.startsWith('/guides/icons/')) {
            clean = 'static/wardogs/items/' + clean.slice(14);
        } else if (clean.startsWith('guides/images/')) {
            clean = 'static/wardogs/renders/' + clean.slice(14);
        } else if (clean.startsWith('/guides/images/')) {
            clean = 'static/wardogs/renders/' + clean.slice(15);
        }
        if (!clean.startsWith('/')) {
            clean = '/' + clean;
        }
        return clean;
    }

    function checkFirstTimeGuide() {
        if (currentProgression && currentProgression.guide_dismissed) {
            return;
        }
        const seen = localStorage.getItem('wl_prog_guide_seen');
        if (seen) return;

        const modal = document.getElementById('prog-guide-modal');
        if (modal) {
            modal.style.display = 'flex';
        }
    }

    function dismissGuide() {
        if (currentProgression) {
            currentProgression.guide_dismissed = true;
        }
        try {
            localStorage.setItem('wl_prog_guide_seen', '1');
        } catch(e) {}
        const modal = document.getElementById('prog-guide-modal');
        if (modal) {
            modal.style.display = 'none';
        }

        // Persist to backend config.json so it never shows again across restarts
        fetch('/api/progression', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ guide_dismissed: true })
        }).catch(function(err) {
            console.error('Failed to persist guide_dismissed', err);
        });
    }

    function toggleGuide() {
        const modal = document.getElementById('prog-guide-modal');
        if (!modal) return;
        if (modal.style.display === 'none' || !modal.style.display) {
            modal.style.display = 'flex';
        } else {
            modal.style.display = 'none';
        }
    }

    async function init() {
        if (isInitialized) return;
        isInitialized = true;

        // Setup global paste listener for Ctrl+V screenshot import
        window.addEventListener('paste', handleGlobalPaste);

        // Fetch initial data
        await loadProgressionData();
        await loadCatalogDatabase();
    }

    async function loadProgressionData() {
        const fetcher = async () => {
            const resp = await fetch('/api/progression');
            if (!resp.ok) throw new Error('Failed to load progression: ' + resp.status);
            return await resp.json();
        };

        const render = (data) => {
            if (!data) return;
            if (data.progression) {
                currentProgression = data.progression;
                if (!currentProgression.roles) {
                    currentProgression.roles = {};
                }
                if (!currentProgression.xp_progress) {
                    currentProgression.xp_progress = {};
                }
                if (!Array.isArray(currentProgression.unlocked_items)) {
                    currentProgression.unlocked_items = [];
                }
            }
            if (data.next_unlocks) {
                nextUnlocks = data.next_unlocks;
            }
            renderAll();
        };

        if (window.UIStore && typeof UIStore.requestSWR === 'function') {
            await UIStore.requestSWR('/api/progression', fetcher, render, () => {});
        } else {
            try {
                const data = await fetcher();
                render(data);
            } catch (e) {
                console.error('Failed loading progression data:', e);
            }
        }
    }

    async function loadCatalogDatabase() {
        const fetcher = async () => {
            const resp = await fetch('/api/progression/database');
            if (!resp.ok) throw new Error('Failed to load catalog: ' + resp.status);
            return await resp.json();
        };

        const render = (data) => {
            if (data && data.unlocks && Array.isArray(data.unlocks)) {
                allUnlocks = data.unlocks;
                updateCareerNextReward();
                updateTargetBanner();
                renderCatalog();
            }
        };

        if (window.UIStore && typeof UIStore.requestSWR === 'function') {
            await UIStore.requestSWR('/api/progression/database', fetcher, render, () => {});
        } else {
            try {
                const data = await fetcher();
                render(data);
            } catch (e) {
                console.error('Failed loading catalog database:', e);
            }
        }
    }

    function renderAll() {
        renderCareerAndRoles();
        updateTargetBanner();
        renderCatalog();
    }

    function renderCareerAndRoles() {
        // Career level is strictly the arithmetic sum of all 6 roles
        let sum = 0;
        for (const r of ROLES) {
            const lvl = (currentProgression.roles && currentProgression.roles[r]) || 0;
            sum += lvl;

            // Update level number in dial
            const lvlEl = document.getElementById('prog-lvl-' + r);
            if (lvlEl) lvlEl.textContent = String(lvl);

            // Update XP progress arc and dot
            const xp = (currentProgression.xp_progress && currentProgression.xp_progress[r]) || 0;
            const clampedXp = Math.min(100, Math.max(0, xp));

            // Arc stroke fill: 280deg arc on radius 37 has length 180.8
            const arcEl = document.getElementById('prog-dial-arc-' + r);
            if (arcEl) {
                const filled = 180.8 * (clampedXp / 100);
                arcEl.style.strokeDasharray = filled.toFixed(1) + ' 232.5';
            }

            // Dot on perimeter: rotated around center with radius 23.8px (58px dial wrap)
            const dotEl = document.getElementById('prog-dial-dot-' + r);
            if (dotEl) {
                const angle = 130 + (clampedXp / 100) * 280;
                dotEl.style.transform = 'rotate(' + angle.toFixed(1) + 'deg) translate(23.8px) rotate(-' + angle.toFixed(1) + 'deg)';
            }

            // Update role next unlock item name, level requirement and XP/price
            const roleInfo = nextUnlocks && nextUnlocks[r];
            const nameEl = document.getElementById('prog-dial-name-' + r);
            const lvlReqEl = document.getElementById('prog-dial-lvl-' + r);
            const xpReqEl = document.getElementById('prog-dial-xp-' + r);

            if (roleInfo && roleInfo.next_item) {
                const it = roleInfo.next_item;
                const displayName = it.name_ru || it.name;
                if (nameEl) {
                    nameEl.textContent = displayName;
                    nameEl.title = displayName;
                }
                if (lvlReqEl) lvlReqEl.textContent = 'Ур. ' + it.level;
                if (xpReqEl) {
                    if (it.total_xp) {
                        xpReqEl.textContent = it.total_xp.toLocaleString('en-US') + ' XP';
                    } else if (it.price) {
                        xpReqEl.textContent = '$' + it.price.toLocaleString('en-US');
                    } else {
                        xpReqEl.textContent = '';
                    }
                }
            } else {
                if (nameEl) {
                    nameEl.textContent = 'Все открыто';
                    nameEl.title = 'Все открыто';
                }
                if (lvlReqEl) lvlReqEl.textContent = 'MAX';
                if (xpReqEl) xpReqEl.textContent = '-';
            }
        }

        currentProgression.career_level = sum;

        // Update Career Level in main header row
        const careerValEl = document.getElementById('prog-career-val');
        if (careerValEl) careerValEl.textContent = String(sum);

        // Update Career Badge Icon
        const badgeImg = document.getElementById('prog-career-badge-img');
        if (badgeImg) {
            badgeImg.src = getCareerBadgePath(sum);
        }

        updateCareerNextReward();
    }

    function updateCareerNextReward() {
        const nextNameEl = document.getElementById('prog-career-next-name');
        const nextLvlEl = document.getElementById('prog-career-next-lvl');
        const nextPriceEl = document.getElementById('prog-career-next-price');

        const careerInfo = nextUnlocks && nextUnlocks['career'];
        if (careerInfo && careerInfo.next_item) {
            const item = careerInfo.next_item;
            const displayName = item.name_ru || item.name;
            if (nextNameEl) nextNameEl.textContent = displayName;
            if (nextLvlEl) nextLvlEl.textContent = 'Ур. ' + item.level;
            if (nextPriceEl) nextPriceEl.textContent = item.price ? '$' + item.price.toLocaleString('en-US') : '$0';
        } else {
            // Find closest career reward from allUnlocks if nextUnlocks['career'] not ready
            const careerItems = allUnlocks.filter(it => it.role === 'career' && it.level > currentProgression.career_level);
            if (careerItems.length > 0) {
                const item = careerItems[0];
                const displayName = item.name_ru || item.name;
                if (nextNameEl) nextNameEl.textContent = displayName;
                if (nextLvlEl) nextLvlEl.textContent = 'Ур. ' + item.level;
                if (nextPriceEl) nextPriceEl.textContent = item.price ? '$' + item.price.toLocaleString('en-US') : '$0';
            } else {
                if (nextNameEl) nextNameEl.textContent = 'Все награды получены';
                if (nextLvlEl) nextLvlEl.textContent = 'MAX';
                if (nextPriceEl) nextPriceEl.textContent = '-';
            }
        }
    }

    function updateTargetBanner() {
        const banner = document.getElementById('prog-target-banner');
        if (!banner) return;

        const wishlistId = currentProgression.wishlist_id;
        if (!wishlistId) {
            banner.style.display = 'none';
            return;
        }

        const targetItem = allUnlocks.find(it => it.unlock_id === wishlistId);
        if (!targetItem) {
            banner.style.display = 'none';
            return;
        }

        const targetRole = targetItem.role;
        let curLvl = 0;
        let roleNameRu = 'КАРЬЕРА';
        if (targetRole === 'career') {
            curLvl = currentProgression.career_level || 0;
            roleNameRu = 'КАРЬЕРА';
        } else {
            curLvl = (currentProgression.roles && currentProgression.roles[targetRole]) || 0;
            const roleLabels = {
                assault: 'ШТУРМОВИК',
                medic: 'МЕДИК',
                recon: 'РАЗВЕДЧИК',
                support: 'ПОДДЕРЖКА',
                driver: 'ВОДИТЕЛЬ',
                pilot: 'ПИЛОТ'
            };
            roleNameRu = roleLabels[targetRole] || (targetRole ? targetRole.toUpperCase() : 'ОПЕРАТОР');
        }

        const reqLvl = targetItem.level || 1;
        const remLvl = Math.max(0, reqLvl - curLvl);
        const pct = Math.min(100, Math.round((curLvl / reqLvl) * 100));

        // Calculate financial goals: target price & total path equipment budget
        const targetPrice = targetItem.price || 0;
        let pathBudget = 0;
        for (const it of allUnlocks) {
            if (it.role === targetRole && it.level > curLvl && it.level <= reqLvl) {
                pathBudget += (it.price || 0);
            }
        }
        if (pathBudget === 0) pathBudget = targetPrice;

        const imgEl = document.getElementById('prog-target-img');
        const nameEl = document.getElementById('prog-target-name');
        const catEl = document.getElementById('prog-target-cat');
        const roleLblEl = document.getElementById('prog-target-role-lbl');
        const reqLvlEl = document.getElementById('prog-target-req-lvl');
        const remEl = document.getElementById('prog-target-remaining');
        const priceEl = document.getElementById('prog-target-price');
        const budgetEl = document.getElementById('prog-target-path-budget');
        const statusEl = document.getElementById('prog-target-cur-status');
        const pctEl = document.getElementById('prog-target-pct');
        const fillEl = document.getElementById('prog-target-bar-fill');

        if (imgEl) imgEl.src = normalizeItemIcon(targetItem.icon);
        if (nameEl) nameEl.textContent = targetItem.name_ru || targetItem.name;
        if (catEl) catEl.textContent = targetItem.category_ru || SUBCAT_RU[targetItem.subcategory] || targetItem.subcategory || targetItem.tab || 'Предмет';
        if (roleLblEl) roleLblEl.textContent = roleNameRu;
        if (reqLvlEl) reqLvlEl.textContent = 'Ур. ' + reqLvl;
        if (remEl) {
            remEl.textContent = remLvl === 0 ? 'Уровень достигнут' : ('Осталось: ' + remLvl + ' ур.');
        }
        if (priceEl) priceEl.textContent = targetPrice ? '$' + targetPrice.toLocaleString('en-US') : '$0';
        if (budgetEl) budgetEl.textContent = pathBudget ? '$' + pathBudget.toLocaleString('en-US') : '$0';
        if (statusEl) statusEl.textContent = curLvl + ' / ' + reqLvl;
        if (pctEl) pctEl.textContent = pct + '%';
        if (fillEl) fillEl.style.width = pct + '%';

        banner.style.display = 'flex';
    }

    async function stepRole(role, delta) {
        if (!currentProgression.roles) currentProgression.roles = {};
        const cur = currentProgression.roles[role] || 0;
        const next = Math.max(0, cur + delta);
        currentProgression.roles[role] = next;

        // Strictly re-calculate sum of all 6 roles
        let sum = 0;
        for (const r of ROLES) {
            sum += currentProgression.roles[r] || 0;
        }
        currentProgression.career_level = sum;

        renderAll();

        try {
            const resp = await fetch('/api/progression', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    roles: currentProgression.roles,
                    career_level: sum,
                    wishlist_id: currentProgression.wishlist_id || '',
                    unlocked_items: currentProgression.unlocked_items || []
                })
            });
            if (resp.ok) {
                const data = await resp.json();
                if (data.next_unlocks) {
                    nextUnlocks = data.next_unlocks;
                    renderCareerAndRoles();
                    updateTargetBanner();
                    renderCatalog();
                }
            }
        } catch (e) {
            console.error('Failed saving role level:', e);
        }
    }

    async function resetAll() {
        // Reset all 6 roles and career to 0, clear unlocked items
        currentProgression.roles = {
            assault: 0,
            medic: 0,
            recon: 0,
            support: 0,
            driver: 0,
            pilot: 0
        };
        currentProgression.career_level = 0;
        currentProgression.xp_progress = {
            assault: 0,
            medic: 0,
            recon: 0,
            support: 0,
            driver: 0,
            pilot: 0
        };
        currentProgression.unlocked_items = [];
        currentProgression.wishlist_id = '';

        renderAll();

        if (window.showToast) {
            showToast('Весь прогресс сброшен до 0');
        }

        try {
            const resp = await fetch('/api/progression', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    roles: currentProgression.roles,
                    career_level: 0,
                    xp_progress: currentProgression.xp_progress,
                    wishlist_id: '',
                    unlocked_items: []
                })
            });
            if (resp.ok) {
                const data = await resp.json();
                if (data.next_unlocks) {
                    nextUnlocks = data.next_unlocks;
                    renderCareerAndRoles();
                    updateTargetBanner();
                    renderCatalog();
                }
            }
        } catch (e) {
            console.error('Failed resetting progression:', e);
        }
    }

    const SUBCAT_RU = {
        'Pistol': 'Пистолет',
        'Magazines': 'Магазин',
        'Body Armour': 'Броня',
        'Helmets': 'Шлем',
        'Optics': 'Прицел',
        'Muzzles': 'ДТК / Глушитель',
        'Assault Rifle': 'Штурм. винтовка',
        'SMG': 'Пистолет-пулемет',
        'Submachine Gun': 'Пистолет-пулемет',
        'Shotgun': 'Дробовик',
        'Sniper Rifle': 'Снайп. винтовка',
        'DMR': 'Марксман. винтовка',
        'Marksman Rifle': 'Марксман. винтовка',
        'LMG': 'Пулемет',
        'Light Machine Gun': 'Ручной пулемет',
        'Machine Gun': 'Пулемет',
        'Backpack': 'Рюкзак',
        'Vest': 'Разгрузка',
        'Grip': 'Рукоятка',
        'Foregrips': 'Рукоятка',
        'Ammunition': 'Боеприпасы',
        'Vehicle': 'Техника',
        'Ground': 'Наземная техника',
        'Air': 'Авиация',
        'Patch': 'Патч',
        'Medical': 'Медицина',
        'Supplies': 'Припасы',
        'Traversal': 'Снаряжение',
        'Building': 'Строительство',
        'Tactical': 'Тактическое',
        'Charge': 'Взрывчатка',
        'Launcher': 'Гранатомет',
        'Bow': 'Лук',
        'Arrows': 'Стрелы',
        'Crate': 'Ящик',
        'Recon': 'Разведка',
        'Other': 'Прочее'
    };

    function toggleHideUnlocked(checked) {
        if (typeof checked === 'boolean') {
            hideUnlocked = checked;
        } else {
            hideUnlocked = !hideUnlocked;
        }
        const checkInput = document.getElementById('check-prog-hide-unlocked');
        if (checkInput && checkInput.checked !== hideUnlocked) {
            checkInput.checked = hideUnlocked;
        }
        renderCatalog();
    }

    async function toggleItemUnlocked(unlockId, e) {
        if (e) e.stopPropagation();
        if (!unlockId) return;

        if (!Array.isArray(currentProgression.unlocked_items)) {
            currentProgression.unlocked_items = [];
        }

        const idx = currentProgression.unlocked_items.indexOf(unlockId);
        if (idx >= 0) {
            currentProgression.unlocked_items.splice(idx, 1);
        } else {
            currentProgression.unlocked_items.push(unlockId);
        }

        renderCatalog();

        try {
            await fetch('/api/progression', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    roles: currentProgression.roles,
                    career_level: currentProgression.career_level,
                    xp_progress: currentProgression.xp_progress,
                    wishlist_id: currentProgression.wishlist_id,
                    unlocked_items: currentProgression.unlocked_items
                })
            });
        } catch (err) {
            console.error('Failed saving unlocked item state:', err);
        }
    }

    function renderCatalog() {
        const tbody = document.getElementById('prog-catalog-tbody');
        if (!tbody) return;

        const countEl = document.getElementById('prog-catalog-count');
        const unlockedSet = new Set(Array.isArray(currentProgression.unlocked_items) ? currentProgression.unlocked_items : []);

        let filtered = allUnlocks.filter(item => {
            if (item.role !== currentFilterRole) {
                return false;
            }
            const isUnlocked = unlockedSet.has(item.unlock_id);
            if (hideUnlocked && isUnlocked) {
                return false;
            }
            if (currentSearchTerm) {
                const q = currentSearchTerm.toLowerCase();
                const matchName = (item.name && item.name.toLowerCase().includes(q)) || (item.name_ru && item.name_ru.toLowerCase().includes(q));
                const matchCat = (item.category_ru && item.category_ru.toLowerCase().includes(q)) || (item.subcategory && item.subcategory.toLowerCase().includes(q));
                const matchDesc = item.description && item.description.toLowerCase().includes(q);
                if (!matchName && !matchCat && !matchDesc) return false;
            }
            return true;
        });

        // Strictly sort by level ascending
        filtered.sort((a, b) => (a.level || 0) - (b.level || 0));

        if (countEl) {
            countEl.textContent = '(' + filtered.length + ' наград)';
        }

        if (filtered.length === 0) {
            tbody.innerHTML = '<tr class="prog-empty-table-row font-mono"><td colspan="6">Ничего не найдено по заданному фильтру</td></tr>';
            return;
        }

        let html = '';
        for (const item of filtered) {
            const isUnlocked = unlockedSet.has(item.unlock_id);
            const isWishlist = currentProgression.wishlist_id === item.unlock_id;

            let rowClass = '';
            if (isUnlocked) {
                rowClass = 'is-unlocked-row';
            } else if (isWishlist) {
                rowClass = 'is-wishlist-row';
            }

            const iconUrl = normalizeItemIcon(item.icon);
            const priceFormatted = item.price ? '$' + item.price.toLocaleString('en-US') : '—';
            const catName = item.category_ru || SUBCAT_RU[item.subcategory] || item.subcategory || item.tab || 'Предмет';
            const displayName = item.name_ru || item.name;

            const lockBtnHtml = isUnlocked
                ? `<button class="btn-prog-lock is-unlocked" onclick="ProgressionController.toggleItemUnlocked('${escapeHtml(item.unlock_id)}', event)" title="Разблокировано (нажмите, чтобы отметить заблокированным)"><svg class="prog-lock-svg unlocked" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#00e676" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 9.9-1"/></svg></button>`
                : `<button class="btn-prog-lock is-locked" onclick="ProgressionController.toggleItemUnlocked('${escapeHtml(item.unlock_id)}', event)" title="Заблокировано (нажмите, чтобы отметить открытым)"><svg class="prog-lock-svg locked" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#666666" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg></button>`;

            const targetBtnHtml = isWishlist
                ? `<button class="btn-prog-target is-active font-mono" onclick="ProgressionController.toggleWishlist('${escapeHtml(item.unlock_id)}', event)" title="Цель активна (нажмите для отмены)"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="6"/><circle cx="12" cy="12" r="2"/></svg></button>`
                : `<button class="btn-prog-target font-mono" onclick="ProgressionController.toggleWishlist('${escapeHtml(item.unlock_id)}', event)" title="Сделать целью"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="6"/><circle cx="12" cy="12" r="2"/></svg></button>`;

            html += `
                <tr class="${rowClass}">
                    <td class="td-lock-cell">${lockBtnHtml}</td>
                    <td class="td-lvl-cell">
                        <span class="td-lvl-badge font-mono">${item.level}</span>
                    </td>
                    <td>
                        <div class="td-item-cell">
                            <img class="td-item-icon" src="${escapeHtml(iconUrl)}" alt="${escapeHtml(displayName)}" onerror="this.src='wardogs_icon.png'">
                            <span class="td-item-name font-mono" title="${escapeHtml(displayName)}">${escapeHtml(displayName)}</span>
                        </div>
                    </td>
                    <td>
                        <span class="td-cat-text font-mono" title="${escapeHtml(catName)}">${escapeHtml(catName)}</span>
                    </td>
                    <td class="td-price-cell font-mono">${priceFormatted}</td>
                    <td style="text-align: center;">${targetBtnHtml}</td>
                </tr>
            `;
        }

        tbody.innerHTML = html;
    }

    async function toggleWishlist(unlockId, e) {
        if (e) e.stopPropagation();
        const nextId = (currentProgression.wishlist_id === unlockId) ? '' : (unlockId || '');
        currentProgression.wishlist_id = nextId;
        updateTargetBanner();
        renderCatalog();

        try {
            await fetch('/api/progression', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    roles: currentProgression.roles,
                    career_level: currentProgression.career_level,
                    xp_progress: currentProgression.xp_progress,
                    wishlist_id: nextId,
                    unlocked_items: currentProgression.unlocked_items || []
                })
            });
        } catch (e) {
            console.error('Failed updating wishlist:', e);
        }
    }

    async function setWishlist(unlockId) {
        return toggleWishlist(unlockId);
    }

    function setFilter(role) {
        currentFilterRole = role;
        document.querySelectorAll('.prog-tab-btn').forEach(btn => {
            btn.classList.toggle('active', btn.dataset.filter === role);
        });
        renderCatalog();
    }

    function onSearchInput(e) {
        currentSearchTerm = e.target.value.trim();
        renderCatalog();
    }

    // --- Screenshot Parsing via Clipboard (Ctrl+V) & File Upload ---
    async function handleGlobalPaste(e) {
        const items = (e.clipboardData || e.originalEvent.clipboardData).items;
        if (!items) return;

        for (let i = 0; i < items.length; i++) {
            if (items[i].type.indexOf('image') !== -1) {
                const blob = items[i].getAsFile();
                if (blob) {
                    e.preventDefault();
                    await uploadScreenshotBlob(blob);
                    return;
                }
            }
        }
    }

    function triggerImport() {
        const fileInput = document.getElementById('prog-file-input');
        if (fileInput) fileInput.click();
    }

    function onFileSelected(e) {
        const file = e.target.files && e.target.files[0];
        if (file) {
            uploadScreenshotBlob(file);
        }
    }

    async function uploadScreenshotBlob(blob) {
        const progEl = document.getElementById('view-progression');
        if (!progEl || !progEl.classList.contains('active')) {
            if (window.switchView) {
                window.switchView('view-progression');
            }
        }

        if (window.showToast) {
            showToast('Распознавание прогресса WARDOGS...');
        }

        try {
            const formData = new FormData();
            formData.append('image', blob, 'screenshot.png');

            const resp = await fetch('/api/progression/parse', {
                method: 'POST',
                body: formData
            });

            if (!resp.ok) {
                const errData = await resp.json();
                throw new Error(errData.error || 'Ошибка распознавания');
            }

            const data = await resp.json();
            if (data.success && data.progression) {
                currentProgression = data.progression;
                if (data.next_unlocks) {
                    nextUnlocks = data.next_unlocks;
                }
                renderAll();
                if (window.showToast) {
                    showToast('Прогресс распознан: Карьера ' + currentProgression.career_level);
                }
            }
        } catch (err) {
            console.error('Screenshot parse error:', err);
            if (window.showToast) {
                showToast('Ошибка распознавания: ' + err.message);
            }
        }
    }

    // --- Drag and Drop Handlers ---
    function onDragOver(e) {
        e.preventDefault();
        e.stopPropagation();
        const dropzone = document.getElementById('prog-dropzone');
        if (dropzone) dropzone.style.display = 'flex';
    }

    function onDragLeave(e) {
        e.preventDefault();
        e.stopPropagation();
        const dropzone = document.getElementById('prog-dropzone');
        if (dropzone && e.relatedTarget && !dropzone.contains(e.relatedTarget)) {
            dropzone.style.display = 'none';
        }
    }

    function onDrop(e) {
        e.preventDefault();
        e.stopPropagation();
        const dropzone = document.getElementById('prog-dropzone');
        if (dropzone) dropzone.style.display = 'none';

        const files = e.dataTransfer && e.dataTransfer.files;
        if (files && files.length > 0) {
            uploadScreenshotBlob(files[0]);
        }
    }

    function onOpen() {
        renderAll();
        checkFirstTimeGuide();
    }

    function getProgressionSummary() {
        if (!currentProgression) return null;
        let sum = currentProgression.career_level || 0;
        const roles = currentProgression.roles || {};
        let roleSum = 0;
        for (const r of ROLES) {
            roleSum += (roles[r] || 0);
        }
        if (sum === 0 && roleSum > 0) sum = roleSum;

        if (sum === 0 && roleSum === 0) return null;

        let targetItemInfo = null;
        if (currentProgression.wishlist_id && allUnlocks && allUnlocks.length > 0) {
            const it = allUnlocks.find(x => x.unlock_id === currentProgression.wishlist_id);
            if (it) {
                const targetRole = it.role || 'assault';
                const curLvl = targetRole === 'career' ? sum : (roles[targetRole] || 0);
                const reqLvl = it.level || 0;
                const remLvl = Math.max(0, reqLvl - curLvl);
                targetItemInfo = {
                    name: it.name_ru || it.name,
                    level: reqLvl,
                    role: targetRole,
                    curLvl: curLvl,
                    remLvl: remLvl,
                    isReached: curLvl >= reqLvl
                };
            }
        }

        return {
            hasProgression: true,
            careerLevel: sum,
            roles: {
                assault: roles.assault || 0,
                medic: roles.medic || 0,
                recon: roles.recon || 0,
                support: roles.support || 0,
                driver: roles.driver || 0,
                pilot: roles.pilot || 0
            },
            targetItem: targetItemInfo
        };
    }

    function onClose() {
        const modal = document.getElementById('prog-guide-modal');
        if (modal) modal.style.display = 'none';
    }

    // Export ProgressionController to global window
    window.ProgressionController = {
        init: init,
        onOpen: onOpen,
        onClose: onClose,
        checkFirstTimeGuide: checkFirstTimeGuide,
        resetAll: resetAll,
        stepRole: stepRole,
        setFilter: setFilter,
        onSearchInput: onSearchInput,
        setWishlist: setWishlist,
        toggleWishlist: toggleWishlist,
        toggleHideUnlocked: toggleHideUnlocked,
        toggleItemUnlocked: toggleItemUnlocked,
        triggerImport: triggerImport,
        onFileSelected: onFileSelected,
        onDragOver: onDragOver,
        onDragLeave: onDragLeave,
        onDrop: onDrop,
        getProgressionSummary: getProgressionSummary,
        getAllUnlocks: function() { return allUnlocks; },
        dismissGuide: dismissGuide,
        toggleGuide: toggleGuide
    };

    // Auto-init on DOMContentLoaded
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();
