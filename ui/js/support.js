// ui/js/support.js - Conversational Support & Ticket Thread Controller (Dark Cloudflare Utility)

(function () {
    let currentTicket = null;
    let currentMessages = [];
    let pollInterval = null;
    let isSending = false;
    let isNewTopicMode = false;
    let selectedPresetCategory = 'other';
    let currentViewMode = 'chat'; // 'chat' | 'history'
    let lastRenderedSignature = '';
    let isFirstLoad = true;

    function escapeHtml(str) {
        if (!str) return '';
        return String(str)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#39;');
    }

    function formatFullDate(isoStr) {
        if (!isoStr) return '';
        const d = new Date(isoStr);
        if (isNaN(d.getTime())) return '';
        const dd = String(d.getDate()).padStart(2, '0');
        const mo = String(d.getMonth() + 1).padStart(2, '0');
        const hh = String(d.getHours()).padStart(2, '0');
        const mm = String(d.getMinutes()).padStart(2, '0');
        return `${dd}.${mo} ${hh}:${mm}`;
    }

    function getStatusBadge(status) {
        switch (status) {
            case 'new':
                return { text: 'Новый', cls: 'badge-status-new' };
            case 'in_progress':
                return { text: 'В работе', cls: 'badge-status-active' };
            case 'resolved':
                return { text: 'Решено', cls: 'badge-status-resolved' };
            case 'closed':
                return { text: 'В архиве', cls: 'badge-status-neutral' };
            default:
                return { text: 'В работе', cls: 'badge-status-active' };
        }
    }

    function getCategoryName(cat) {
        const map = {
            'game_crash': 'Вылет игры WARDOGS',
            'gateway_conn': 'Ошибка шлюза',
            'high_ping': 'Высокий пинг / потери',
            'discord_dpi': 'Сбой Discord / DPI',
            'other': 'Общий вопрос'
        };
        return map[cat] || cat || 'Обращение';
    }

    async function fetchSupportChat(silent = false, ticketId = null) {
        if (isNewTopicMode && !ticketId) {
            // Keep user on the new topic creation screen without overriding it
            return;
        }

        const streamEl = document.getElementById('support-chat-stream');
        if (!silent && streamEl && (!currentMessages || currentMessages.length === 0)) {
            streamEl.innerHTML = `
                <div class="support-chat-empty">
                    <span class="font-mono text-muted" style="font-size: 11px;">Синхронизация истории диалога...</span>
                </div>
            `;
        }

        try {
            const url = ticketId ? `/api/support/active?ticket_id=${ticketId}` : '/api/support/active';
            const resp = await fetch(url);
            if (!resp.ok) {
                if (!silent) {
                    currentTicket = null;
                    currentMessages = [];
                    renderEmptySupportState();
                }
                return;
            }
            const data = await resp.json();

            if (data && data.success && data.ticket) {
                currentTicket = data.ticket;
                currentMessages = data.messages || [];
                isNewTopicMode = false;
                renderSupportHeader();
                renderSupportChatMessages();
            } else {
                if (!silent || !currentTicket) {
                    currentTicket = null;
                    currentMessages = [];
                    renderEmptySupportState();
                }
            }
        } catch (e) {
            console.error('Failed to load support chat:', e);
            if (!silent) {
                currentTicket = null;
                currentMessages = [];
                renderEmptySupportState();
            }
        }
    }

    function renderSupportHeader() {
        const idEl = document.getElementById('support-ticket-id');
        const badgeEl = document.getElementById('support-status-badge');
        const titleEl = document.getElementById('support-main-title');
        const archivedBanner = document.getElementById('support-archived-banner');
        const quickResolveBtn = document.getElementById('btn-quick-resolve');
        const historyBtn = document.getElementById('btn-support-history-toggle');
        const backBtn = document.getElementById('btn-support-back');

        if (currentViewMode === 'history') {
            if (titleEl) titleEl.textContent = 'ИСТОРИЯ';
            if (idEl) idEl.style.display = 'none';
            if (badgeEl) badgeEl.style.display = 'none';
            if (archivedBanner) archivedBanner.style.display = 'none';
            if (historyBtn) historyBtn.textContent = 'К диалогу';
            if (backBtn) backBtn.style.display = 'none';
            return;
        }

        if (titleEl) titleEl.textContent = 'ПОДДЕРЖКА';
        if (historyBtn) historyBtn.textContent = 'История';
        if (backBtn) backBtn.style.display = 'none';

        if (isNewTopicMode || !currentTicket || !currentTicket.id) {
            if (idEl) {
                idEl.textContent = 'Новая тема';
                idEl.style.display = 'inline-block';
            }
            if (badgeEl) badgeEl.style.display = 'none';
            if (archivedBanner) archivedBanner.style.display = 'none';
            if (quickResolveBtn) quickResolveBtn.style.display = 'none';
            return;
        }

        if (idEl) {
            idEl.textContent = `#TK-${String(currentTicket.id).padStart(4, '0')}`;
            idEl.style.display = 'inline-block';
        }
        if (badgeEl) {
            const bInfo = getStatusBadge(currentTicket.status);
            badgeEl.className = `support-status-pill ${bInfo.cls}`;
            badgeEl.textContent = bInfo.text;
            badgeEl.style.display = 'inline-block';
        }

        const isClosed = currentTicket.status === 'resolved' || currentTicket.status === 'closed';
        if (archivedBanner) {
            archivedBanner.style.display = isClosed ? 'flex' : 'none';
        }
        if (quickResolveBtn) {
            quickResolveBtn.style.display = isClosed ? 'none' : 'inline-flex';
        }
    }

    function renderEmptySupportState() {
        renderSupportHeader();
        const streamEl = document.getElementById('support-chat-stream');
        if (!streamEl) return;

        lastRenderedSignature = 'empty_state';
        streamEl.innerHTML = `
            <div class="support-welcome-card">
                <div class="support-welcome-title">Связь с разработчиком WarLink</div>
                <div class="support-welcome-desc">
                    Опишите проблему, вылет игры, задержку или вопрос по сервису.
                    Для вашего вопроса будет открыта отдельная тема диалога.
                </div>
                <div class="support-preset-chips">
                    <button class="support-chip" onclick="setPresetCategory('game_crash', 'Вылет из матча WARDOGS')">Вылет игры WARDOGS</button>
                    <button class="support-chip" onclick="setPresetCategory('gateway_conn', 'Ошибка подключения к шлюзу')">Ошибка шлюза</button>
                    <button class="support-chip" onclick="setPresetCategory('high_ping', 'Высокий пинг / потеря пакетов')">Высокий пинг</button>
                    <button class="support-chip" onclick="setPresetCategory('discord_dpi', 'Сбой работы Discord')">Проблема с Discord</button>
                </div>
            </div>
        `;
    }

    function renderSupportChatMessages() {
        const streamEl = document.getElementById('support-chat-stream');
        if (!streamEl) return;

        if (!currentMessages || currentMessages.length === 0) {
            renderEmptySupportState();
            return;
        }

        // Check if user is scrolled near bottom (within 60px)
        const isNearBottom = (streamEl.scrollHeight - streamEl.scrollTop - streamEl.clientHeight) < 60;

        // Check if messages list actually changed to prevent DOM thrashing and scroll jumps
        const newSignature = `${currentTicket ? currentTicket.id : 0}_` + currentMessages.map(m => `${m.id}_${m.status || ''}_${m.message ? m.message.length : 0}`).join('|');
        if (newSignature === lastRenderedSignature) {
            // No changes: do NOT touch innerHTML or scroll position!
            return;
        }
        lastRenderedSignature = newSignature;

        let html = '';
        for (const m of currentMessages) {
            const isUser = m.sender_type === 'user';
            const isAdmin = m.sender_type === 'admin';
            const isSys = m.sender_type === 'system';
            const timeStr = formatFullDate(m.created_at);

            if (isSys) {
                const isLogs = m.attachment_type === 'logs_archive';
                html += `
                    <div class="support-msg-system">
                        <div class="support-sys-icon">
                            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                                <circle cx="12" cy="12" r="10"/>
                                <line x1="12" y1="16" x2="12" y2="12"/>
                                <line x1="12" y1="8" x2="12.01" y2="8"/>
                            </svg>
                        </div>
                        <div class="support-sys-text">
                            <span class="support-sys-tag">${isLogs ? 'ДИАГНОСТИКА' : 'СИСТЕМА'}:</span>
                            ${escapeHtml(m.message)}
                        </div>
                        <span class="support-msg-time font-mono">${escapeHtml(timeStr)}</span>
                    </div>
                `;
            } else if (isAdmin) {
                html += `
                    <div class="support-msg-row support-msg-admin">
                        <div class="support-bubble-admin">
                            <div class="support-bubble-header">
                                <span class="support-author-name support-author-admin">Max (Разработчик)</span>
                                <span class="support-msg-time font-mono">${escapeHtml(timeStr)}</span>
                            </div>
                            <div class="support-bubble-body">${escapeHtml(m.message)}</div>
                        </div>
                    </div>
                `;
            } else {
                html += `
                    <div class="support-msg-row support-msg-user">
                        <div class="support-bubble-user">
                            <div class="support-bubble-header">
                                <span class="support-author-name">${escapeHtml(m.sender_name || 'Вы')}</span>
                                <span class="support-msg-time font-mono">${escapeHtml(timeStr)}</span>
                            </div>
                            <div class="support-bubble-body">${escapeHtml(m.message)}</div>
                        </div>
                    </div>
                `;
            }
        }

        streamEl.innerHTML = html;

        // ONLY auto-scroll down if user was already near bottom or on initial load
        if (isFirstLoad || isNearBottom) {
            streamEl.scrollTop = streamEl.scrollHeight;
            isFirstLoad = false;
        }
    }

    async function fetchSupportHistory() {
        const listEl = document.getElementById('support-history-list');
        const countEl = document.getElementById('support-history-count');
        if (listEl) {
            listEl.innerHTML = `
                <div class="support-chat-empty">
                    <span class="font-mono text-muted" style="font-size: 11px;">Загрузка списка обращений...</span>
                </div>
            `;
        }

        try {
            const resp = await fetch('/api/support/history');
            if (!resp.ok) {
                if (listEl) listEl.innerHTML = `<div class="support-chat-empty"><span class="text-muted" style="font-size: 11px;">Не удалось загрузить историю</span></div>`;
                return;
            }
            const data = await resp.json();
            const tickets = (data && data.tickets) ? data.tickets : [];

            if (countEl) {
                countEl.textContent = `${tickets.length} обращений`;
            }

            if (tickets.length === 0) {
                if (listEl) {
                    listEl.innerHTML = `
                        <div class="support-welcome-card" style="margin: 30px 10px;">
                            <div class="support-welcome-title">История обращений пуста</div>
                            <div class="support-welcome-desc">
                                У вас пока нет созданных тикетов. Нажмите «+ Новое обращение», чтобы отправить вопрос разработчику.
                            </div>
                            <button class="btn btn-primary" onclick="startNewSupportTopic()" style="font-size: 11px; padding: 6px 14px;">Создать обращение</button>
                        </div>
                    `;
                }
                return;
            }

            let html = '';
            for (const t of tickets) {
                const bInfo = getStatusBadge(t.status);
                const isCurrent = currentTicket && currentTicket.id === t.id;
                const catName = getCategoryName(t.category);
                const dateStr = formatFullDate(t.created_at);
                const snippet = t.admin_reply ? `Ответ разработчика: ${t.admin_reply}` : (t.user_comment || 'Без комментария');
                const msgsCount = t.messages_count || 1;

                html += `
                    <div class="support-history-card ${isCurrent ? 'is-active-ticket' : ''}" onclick="openTicketById(${t.id})">
                        <div class="history-card-header">
                            <div class="history-card-left">
                                <span class="history-card-id font-mono">#TK-${String(t.id).padStart(4, '0')}</span>
                                <span class="history-card-category">${escapeHtml(catName)}</span>
                            </div>
                            <div style="display: flex; align-items: center; gap: 6px;">
                                <span class="history-card-time font-mono">${escapeHtml(dateStr)}</span>
                                <span class="${bInfo.cls}" style="font-size: 9px; padding: 2px 6px;">${bInfo.text}</span>
                            </div>
                        </div>
                        <div class="history-card-snippet font-sans">${escapeHtml(snippet)}</div>
                        <div class="history-card-footer font-mono">
                            <span>Сообщений: ${msgsCount}</span>
                            <span style="color: var(--action);">Открыть диалог →</span>
                        </div>
                    </div>
                `;
            }

            if (listEl) listEl.innerHTML = html;
        } catch (e) {
            console.error('Failed to load support history:', e);
            if (listEl) listEl.innerHTML = `<div class="support-chat-empty"><span class="text-muted" style="font-size: 11px;">Ошибка соединения</span></div>`;
        }
    }

    function switchSupportPane(paneName) {
        currentViewMode = paneName;
        const chatPane = document.getElementById('support-chat-pane');
        const histPane = document.getElementById('support-history-pane');
        const histBtn = document.getElementById('btn-support-history-toggle');

        if (paneName === 'history') {
            if (chatPane) chatPane.style.display = 'none';
            if (histPane) histPane.style.display = 'flex';
            if (histBtn) histBtn.textContent = 'К диалогу';
            renderSupportHeader();
            fetchSupportHistory();
        } else {
            if (chatPane) chatPane.style.display = 'flex';
            if (histPane) histPane.style.display = 'none';
            if (histBtn) histBtn.textContent = 'История обращений';
            renderSupportHeader();
        }
    }

    window.toggleSupportHistoryView = function () {
        if (currentViewMode === 'history') {
            switchSupportPane('chat');
        } else {
            switchSupportPane('history');
        }
    };

    window.openTicketById = function (ticketId) {
        isNewTopicMode = false;
        switchSupportPane('chat');
        isFirstLoad = true;
        lastRenderedSignature = '';
        fetchSupportChat(false, ticketId);
    };

    window.onSupportBackClick = function (event) {
        if (event) event.stopPropagation();
        if (currentViewMode === 'history') {
            switchSupportPane('chat');
            return;
        }
        if (typeof switchView === 'function') {
            switchView(null);
        }
    };

    window.setPresetCategory = function (catKey, label) {
        selectedPresetCategory = catKey;
        const input = document.getElementById('support-input-message');
        if (input) {
            input.value = `[${label}]: `;
            input.focus();
        }
    };

    window.onSupportInputKeyDown = function (event) {
        if (event.key === 'Enter' && !event.shiftKey) {
            event.preventDefault();
            sendSupportMessage();
        }
    };

    window.sendSupportMessage = async function () {
        if (isSending) return;
        const input = document.getElementById('support-input-message');
        if (!input) return;
        const text = input.value.trim();
        if (!text) return;

        isSending = true;
        const sendBtn = document.getElementById('btn-support-send');
        if (sendBtn) sendBtn.disabled = true;

        const ticketId = isNewTopicMode ? 0 : (currentTicket ? currentTicket.id : 0);
        const payload = {
            ticket_id: ticketId,
            is_new_topic: isNewTopicMode || ticketId === 0,
            message: text,
            category: selectedPresetCategory
        };

        try {
            const resp = await fetch('/api/support/send-message', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload)
            });

            if (resp.ok) {
                const data = await resp.json();
                input.value = '';
                isNewTopicMode = false;
                isFirstLoad = true; // force scroll to bottom on new message
                lastRenderedSignature = '';

                if (data && data.ticket_id) {
                    await fetchSupportChat(false, data.ticket_id);
                } else {
                    await fetchSupportChat(false);
                }

                if (typeof showToast === 'function') {
                    showToast('Сообщение отправлено');
                }
            } else {
                if (typeof showToast === 'function') {
                    showToast('Ошибка отправки сообщения');
                }
            }
        } catch (e) {
            console.error('Failed to send support message:', e);
            if (typeof showToast === 'function') {
                showToast('Сетевой сбой при отправке');
            }
        } finally {
            isSending = false;
            if (sendBtn) sendBtn.disabled = false;
        }
    };

    window.sendFreshSupportLogs = async function () {
        const btn = document.getElementById('btn-quick-send-logs');
        const feedback = document.getElementById('support-action-feedback');
        if (btn) btn.disabled = true;
        if (feedback) {
            feedback.textContent = 'Упаковка логов...';
            feedback.style.display = 'inline-block';
        }

        const ticketId = currentTicket ? currentTicket.id : 0;

        try {
            const resp = await fetch('/api/support/send-fresh-logs', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ticket_id: ticketId })
            });

            if (resp.ok) {
                const data = await resp.json();
                if (feedback) {
                    feedback.textContent = `Логи загружены (${Math.round(data.archive_size / 1024)} КБ)`;
                    setTimeout(() => { if (feedback) feedback.style.display = 'none'; }, 3500);
                }
                isFirstLoad = true;
                lastRenderedSignature = '';
                await fetchSupportChat(true, data.ticket_id || ticketId);
            } else {
                if (feedback) feedback.textContent = 'Сбой отправки';
            }
        } catch (e) {
            console.error('Send logs failed:', e);
            if (feedback) feedback.textContent = 'Сетевой сбой';
        } finally {
            if (btn) btn.disabled = false;
        }
    };

    window.sendSupportPingTest = async function () {
        const btn = document.getElementById('btn-quick-ping');
        const feedback = document.getElementById('support-action-feedback');
        if (btn) btn.disabled = true;
        if (feedback) {
            feedback.textContent = 'Замер задержки...';
            feedback.style.display = 'inline-block';
        }

        const ticketId = currentTicket ? currentTicket.id : 0;

        try {
            const resp = await fetch('/api/support/ping-test', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ticket_id: ticketId })
            });

            if (resp.ok) {
                const data = await resp.json();
                if (feedback) {
                    feedback.textContent = `RTT: ${data.ping_ms} мс`;
                    setTimeout(() => { if (feedback) feedback.style.display = 'none'; }, 4000);
                }
                isFirstLoad = true;
                lastRenderedSignature = '';
                await fetchSupportChat(true, ticketId);
            } else {
                if (feedback) feedback.textContent = 'Таймаут шлюза';
            }
        } catch (e) {
            console.error('Ping test failed:', e);
            if (feedback) feedback.textContent = 'Ошибка теста';
        } finally {
            if (btn) btn.disabled = false;
        }
    };

    window.resolveSupportTicket = async function () {
        if (!currentTicket || !currentTicket.id) return;
        const btn = document.getElementById('btn-quick-resolve');
        if (btn) btn.disabled = true;

        try {
            const resp = await fetch('/api/support/resolve', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ticket_id: currentTicket.id })
            });

            if (resp.ok) {
                if (typeof showToast === 'function') {
                    showToast('Обращение успешно отмечено как решенное и перенесено в архив');
                }
                lastRenderedSignature = '';
                isFirstLoad = true;
                await fetchSupportChat(false, currentTicket.id);
            }
        } catch (e) {
            console.error('Failed to resolve ticket:', e);
        } finally {
            if (btn) btn.disabled = false;
        }
    };

    window.startNewSupportTopic = function () {
        isNewTopicMode = true;
        currentTicket = null;
        currentMessages = [];
        selectedPresetCategory = 'other';
        switchSupportPane('chat');
        renderEmptySupportState();
        const input = document.getElementById('support-input-message');
        if (input) {
            input.value = '';
            input.focus();
        }
    };

    window.openSupportChat = function () {
        switchSupportPane('chat');
        isFirstLoad = true;
        lastRenderedSignature = '';
        fetchSupportChat();
        if (pollInterval) clearInterval(pollInterval);
        pollInterval = setInterval(() => {
            const viewEl = document.getElementById('view-support');
            if (viewEl && viewEl.classList.contains('active')) {
                if (currentViewMode === 'chat' && !isNewTopicMode && currentTicket) {
                    fetchSupportChat(true, currentTicket.id);
                }
            } else {
                clearInterval(pollInterval);
                pollInterval = null;
            }
        }, 3500);
    };

    window.toggleSupportChat = function (event) {
        if (event) event.stopPropagation();
        if (typeof switchView === 'function') {
            switchView('view-support');
        }
    };
})();
