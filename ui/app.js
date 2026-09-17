let isConnected = false;
let isBusy = false;
let isDownloadingDeps = false;

// --- Window Dragging and Controls ---
function handleTitlebarMouseDown(e) {
    if (e.target.closest('.win-btn')) return;
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

// --- API / State Sync ---
async function fetchStatus() {
    try {
        const res = await fetch('/api/status');
        const data = await res.json();
        updateUI(data);
    } catch (e) {
        // Server initializing
    }
}

function updateUI(data) {
    isConnected = data.is_connected;
    isBusy = data.is_busy;
    isDownloadingDeps = !!data.is_downloading_deps;

    const btnToggle = document.getElementById('btn-toggle');
    const badgeStatus = document.getElementById('badge-status');
    const badgeText = document.getElementById('badge-text');
    const statusDesc = document.getElementById('status-desc');
    const selectProfile = document.getElementById('select-profile');
    const chkAuto = document.getElementById('chk-autolaunch');
    const lastLogMsg = document.getElementById('last-log-msg');

    if (chkAuto && chkAuto.checked !== data.autolaunch_game) {
        chkAuto.checked = data.autolaunch_game;
    }

    // Populate and sync profile select dropdown
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
        selectProfile.disabled = (isConnected || isBusy || isDownloadingDeps || data.is_testing);
    }

    let targetText = 'ПОДКЛЮЧИТЬ';
    let targetClass = 'btn btn-primary';
    let targetBadgeText = 'ГОТОВ';
    let targetBadgeClass = 'status-line status-standby';
    let targetDesc = 'Система готова к оптимизации маршрута';
    let targetDisabled = false;

    if (isDownloadingDeps) {
        targetDisabled = true;
        targetText = 'ЗАГРУЗКА КОМПОНЕНТОВ…';
        targetClass = 'btn btn-primary btn-busy';
        targetBadgeClass = 'status-line status-busy';
        targetBadgeText = 'ЗАГРУЗКА…';
        targetDesc = data.deps_msg || 'Первичное скачивание сетевых компонентов с GitHub…';
    } else if (data.is_testing) {
        targetDisabled = true;
        targetText = 'ТЕСТИРОВАНИЕ…';
        targetClass = 'btn btn-primary btn-busy';
        targetBadgeClass = 'status-line status-busy';
        targetBadgeText = 'ТЕСТ ZAPRET…';
        targetDesc = 'Проверка 22 профилей обхода DPI (service 12)';
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
        targetDesc = 'Сетевая оптимизация активна (Cloudflare + Zapret)';
    }

    if (btnToggle) {
        if (btnToggle.disabled !== targetDisabled) btnToggle.disabled = targetDisabled;
        const btnSpan = document.getElementById('btn-text');
        if (btnSpan) {
            if (btnSpan.textContent !== targetText) btnSpan.textContent = targetText;
        } else {
            if (btnToggle.textContent !== targetText) btnToggle.textContent = targetText;
        }
        if (btnToggle.className !== targetClass) btnToggle.className = targetClass;
    }

    const btnSpinner = document.getElementById('btn-spinner');
    if (btnSpinner) {
        btnSpinner.style.display = (isBusy || data.is_testing || isDownloadingDeps) ? 'inline-block' : 'none';
    }

    if (badgeStatus && badgeStatus.className !== targetBadgeClass) {
        badgeStatus.className = targetBadgeClass;
    }
    if (badgeText && badgeText.textContent !== targetBadgeText) {
        badgeText.textContent = targetBadgeText;
    }
    if (statusDesc && statusDesc.textContent !== targetDesc) {
        statusDesc.textContent = targetDesc;
    }

    // Minimal Linear Progress Bar & Animations
    const b = data.benchmark || data.progress;
    const mainBox = document.getElementById('main-progress-box');
    const mainMsg = document.getElementById('main-progress-msg');
    const mainPct = document.getElementById('main-progress-pct');
    const mainFill = document.getElementById('main-progress-fill');

    const isRunning = isBusy || data.is_testing || (b && b.is_running);

    if (isRunning && !isConnected) {
        if (mainBox) mainBox.style.display = 'flex';
        const rawPct = (b && b.percent) || 0;
        // Strictly clamp percent to 99% while connecting, never 100% or 101%
        const clampedPct = Math.min(Math.max(rawPct, 0), 99);
        const pctStr = clampedPct + '%';
        const msgStr = (b && b.message) || (isBusy ? 'Оптимизация сетевого маршрута...' : 'Идет тестирование...');

        if (mainMsg && mainMsg.textContent !== msgStr) mainMsg.textContent = msgStr;
        if (mainPct && mainPct.textContent !== pctStr) mainPct.textContent = pctStr;
        if (mainFill) {
            if (mainFill.style.width !== pctStr) mainFill.style.width = pctStr;
            if (!mainFill.classList.contains('active-running')) {
                mainFill.classList.add('active-running');
            }
        }
        updatePipelineSteps(clampedPct);
    } else if (isConnected) {
        if (mainFill) {
            mainFill.classList.remove('active-running');
            mainFill.style.width = '100%';
        }
        updatePipelineSteps(100);
        if (mainBox) mainBox.style.display = 'none';
    } else {
        if (mainFill) {
            mainFill.classList.remove('active-running');
            mainFill.style.width = '0%';
        }
        if (mainBox) mainBox.style.display = 'none';
    }

    // Latest log message in footer
    if (lastLogMsg && data.logs && data.logs.length > 0) {
        const last = data.logs[data.logs.length - 1];
        if (lastLogMsg.textContent !== last) {
            lastLogMsg.textContent = last;
        }
    }

    // Zapret Test button state
    const btnRunTest = document.getElementById('btn-run-test');
    if (btnRunTest) {
        if (data.is_testing) {
            btnRunTest.textContent = 'Идет тестирование…';
            btnRunTest.disabled = true;
        } else {
            btnRunTest.textContent = 'Тест Zapret (service 12) ▷';
            btnRunTest.disabled = isConnected || isBusy || isDownloadingDeps;
        }
    }
}

function updatePipelineSteps(pct) {
    const sDiag = document.getElementById('step-diag');
    const sZapret = document.getElementById('step-zapret');
    const sWarp = document.getElementById('step-warp');
    const sReady = document.getElementById('step-ready');
    if (!sDiag || !sZapret || !sWarp || !sReady) return;

    sDiag.className = 'step-item';
    sZapret.className = 'step-item';
    sWarp.className = 'step-item';
    sReady.className = 'step-item';

    if (pct < 18) {
        sDiag.className = 'step-item step-active';
    } else if (pct < 75) {
        sDiag.className = 'step-item step-done';
        sZapret.className = 'step-item step-active';
    } else if (pct < 92) {
        sDiag.className = 'step-item step-done';
        sZapret.className = 'step-item step-done';
        sWarp.className = 'step-item step-active';
    } else if (pct < 100) {
        sDiag.className = 'step-item step-done';
        sZapret.className = 'step-item step-done';
        sWarp.className = 'step-item step-done';
        sReady.className = 'step-item step-active';
    } else {
        sDiag.className = 'step-item step-done';
        sZapret.className = 'step-item step-done';
        sWarp.className = 'step-item step-done';
        sReady.className = 'step-item step-done';
    }
}

async function runZapretTest() {
    if (isDownloadingDeps) return;
    const btn = document.getElementById('btn-run-test');
    if (btn) btn.textContent = 'Запуск тестирования…';
    try {
        await fetch('/api/run-test', { method: 'POST' });
    } catch (e) {
        console.error(e);
    }
}

async function onProfileChange(val) {
    if (!val || isBusy || isConnected || isDownloadingDeps) return;
    try {
        await fetch('/api/profile', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ profile: val })
        });
    } catch (e) {
        console.error(e);
    }
}

async function toggleConnect() {
    if (isBusy || isDownloadingDeps) return;

    const btnToggle = document.getElementById('btn-toggle');
    const btnSpan = document.getElementById('btn-text');
    const btnSpinner = document.getElementById('btn-spinner');
    const badgeStatus = document.getElementById('badge-status');
    const badgeText = document.getElementById('badge-text');
    const statusDesc = document.getElementById('status-desc');
    const mainBox = document.getElementById('main-progress-box');
    const mainMsg = document.getElementById('main-progress-msg');
    const mainPct = document.getElementById('main-progress-pct');
    const mainFill = document.getElementById('main-progress-fill');

    isBusy = true;

    // Instant UI feedback on click (zero perceived lag)
    if (!isConnected) {
        if (btnToggle) {
            btnToggle.disabled = true;
            btnToggle.className = 'btn btn-primary btn-busy';
        }
        if (btnSpan) btnSpan.textContent = 'ПОДКЛЮЧЕНИЕ…';
        if (btnSpinner) btnSpinner.style.display = 'inline-block';
        if (badgeStatus) badgeStatus.className = 'status-line status-busy';
        if (badgeText) badgeText.textContent = 'ПОДКЛЮЧЕНИЕ…';
        if (statusDesc) statusDesc.textContent = 'Идет запуск и настройка туннеля, подождите…';

        if (mainBox) mainBox.style.display = 'flex';
        if (mainMsg) mainMsg.textContent = 'Инициализация компонентов...';
        if (mainPct) mainPct.textContent = '3%';
        if (mainFill) {
            mainFill.style.width = '3%';
            mainFill.classList.add('active-running');
        }
        updatePipelineSteps(3);
    } else {
        if (btnToggle) {
            btnToggle.disabled = true;
            btnToggle.className = 'btn btn-primary btn-busy';
        }
        if (btnSpan) btnSpan.textContent = 'ОТКЛЮЧЕНИЕ…';
        if (btnSpinner) btnSpinner.style.display = 'inline-block';
    }

    const endpoint = isConnected ? '/api/disconnect' : '/api/connect';
    try {
        await fetch(endpoint, { method: 'POST' });
    } catch (e) {
        isBusy = false;
    }
}

async function onAutoLaunchChange() {
    const chk = document.getElementById('chk-autolaunch');
    try {
        await fetch('/api/settings', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ autolaunch_game: chk.checked })
        });
    } catch (e) {
        console.error(e);
    }
}

async function openLogFile() {
    try {
        if (window.openLogFileNative) {
            await window.openLogFileNative();
            return;
        }
        await fetch('/api/open-log', { method: 'POST' });
    } catch (e) {
        console.error(e);
        try {
            await fetch('/api/open-log', { method: 'POST' });
        } catch (_) {}
    }
}

// Poll loop (750ms)
setInterval(fetchStatus, 750);

fetchStatus();

// Notify native host that UI is rendered and ready to be revealed without flashing
function notifyReadyToReveal() {
    requestAnimationFrame(() => {
        setTimeout(() => {
            if (window.revealWindow) {
                window.revealWindow();
            }
        }, 30);
    });
}

if (document.readyState === 'complete' || document.readyState === 'interactive') {
    notifyReadyToReveal();
} else {
    window.addEventListener('DOMContentLoaded', notifyReadyToReveal);
}
