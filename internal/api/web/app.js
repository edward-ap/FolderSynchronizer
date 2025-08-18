/**
 * File Sync Application - Main JavaScript Module
 * Handles sync pairs management, scheduling, hooks, and UI interactions
 */

// ===== UTILITY FUNCTIONS =====

/**
 * Simple DOM selector utilities
 */
const $ = (selector, element = document) => element.querySelector(selector);
const $$ = (selector, element = document) => Array.from(element.querySelectorAll(selector));

/**
 * HTML escape utility to prevent XSS
 */
const esc = (s) => String(s ?? '').replace(/[&<>"']/g, c => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;'
}[c]));

/**
 * Date formatting utility
 */
const fmtDate = (timestamp) => {
    try {
        return new Date(timestamp).toLocaleString();
    } catch {
        return '—';
    }
};

// ===== API MODULE =====

/**
 * API client for sync pairs management
 */
const api = {
    list: () => fetchJSON('/api/pairs'),
    create: (pair) => fetchJSON('/api/pairs', { method: 'POST', body: JSON.stringify(pair) }),
    update: (id, pair) => fetchJSON(`/api/pairs/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(pair) }),
    remove: (id) => fetchJSON(`/api/pairs/${encodeURIComponent(id)}`, { method: 'DELETE' }),
    start: (id) => fetchJSON(`/api/pairs/${encodeURIComponent(id)}/start`, { method: 'POST' }),
    stop: (id) => fetchJSON(`/api/pairs/${encodeURIComponent(id)}/stop`, { method: 'POST' }),
    sync: (id) => fetchJSON(`/api/pairs/${encodeURIComponent(id)}/sync`, { method: 'POST' }),
    syncAll: () => fetchJSON('/api/syncAll', { method: 'POST' }),
    testHook: (id) => fetchJSON(`/api/pairs/${encodeURIComponent(id)}/test-hook`, { method: 'POST' }),
    examples: () => fetchJSON('/api/schedules/examples')
};

/**
 * HTTP utility for JSON API calls
 */
async function fetchJSON(url, opts = {}) {
    const headers = { 'Content-Type': 'application/json' };
    const res = await fetch(url, { ...opts, headers });
    const txt = await res.text();

    let data = null;
    try {
        data = txt ? JSON.parse(txt) : null;
    } catch {
        data = txt;
    }

    if (!res.ok) {
        throw new Error((data && data.error) || res.statusText || `HTTP ${res.status}`);
    }

    return data;
}

// ===== NOTIFICATION MODULE =====

/**
 * Toast notification system
 */
const toasts = $('#toasts');

function toast(message, type = 'ok') {
    const notification = document.createElement('div');
    notification.className = `toast ${type}`;
    notification.textContent = message;
    toasts.appendChild(notification);

    setTimeout(() => notification.remove(), 3000);
}

// ===== FOLDER SELECTION MODULE =====

/**
 * Folder selection utility (placeholder for desktop implementation)
 */
function selectFolder(inputId) {
    // Note: This is a placeholder function. In a real application,
    // you would need to implement native folder selection.
    // For web applications, you could use the File System Access API or electron APIs
    const input = $('#' + inputId);
    if (input) {
        input.focus();
        toast('Folder selection would open native dialog in desktop app', 'ok');

        // In a real desktop app (Electron, Tauri, etc.), this would be:
        // const result = await window.electronAPI.selectFolder();
        // if (result) input.value = result;
    }
}

// ===== MAIN UI RENDERING MODULE =====

/**
 * DOM elements for main UI
 */
const syncItems = $('#syncItems');

/**
 * Renders the main list of sync pairs
 */
function renderPairs(pairs) {
    syncItems.innerHTML = '';

    if (!pairs || pairs.length === 0) {
        syncItems.innerHTML = `<div class="empty">No pairs yet</div>`;
        return;
    }

    for (const pair of pairs) {
        const card = createPairCard(pair);
        syncItems.appendChild(card);
    }
}

/**
 * Creates a single sync pair card element
 */
function createPairCard(pair) {
    const card = document.createElement('div');
    card.className = 'sync-item';
    card.dataset.id = pair.id;

    const isRunning = !!pair.enabled;
    const statusText = isRunning ? 'Running' : 'Stopped';
    const actionText = isRunning ? 'Stop' : 'Start';
    const actionIcon = isRunning ? 'M6 6h12v12H6z' : 'M8 5v14l11-7z';

    const description = generateDescription(pair);
    const scheduleInfo = renderScheduleInfo(pair);
    const hooksInfo = renderHooksInfo(pair);

    const lastRun = pair.status?.lastRun ? fmtDate(pair.status.lastRun) : '—';
    const nextRun = pair.status?.nextRun ? fmtDate(pair.status.nextRun)
        : (isRunning && pair.status?.scheduleType === 'watcher' ? 'Watching…' : '—');

    card.innerHTML = `
        <div class="sync-header" onclick="headerClick(event, this.parentElement)">
            <svg class="chevron icon" viewBox="0 0 24 24">
                <path d="M8.59 16.59 13.17 12 8.59 7.41 10 6l6 6-6 6z"/>
            </svg>
            <div class="status-indicator">
                <span class="status-dot ${isRunning ? 'running' : 'stopped'}"></span>
                <span class="status-text">${statusText}</span>
            </div>
            <div>
                <div class="sync-id">${esc(pair.id)}</div>
                <div class="sync-description">${esc(description)}</div>
            </div>
            <div class="sync-actions">
                <button class="btn" data-act="${isRunning ? 'stop' : 'start'}" data-id="${pair.id}">
                    <svg class="icon" viewBox="0 0 24 24"><path d="${actionIcon}"/></svg>${actionText}
                </button>
                <button class="btn" data-act="edit" data-id="${pair.id}">
                    <svg class="icon" viewBox="0 0 24 24">
                        <path d="M3 17.25V21h3.75L17.81 9.94l-3.75-3.75L3 17.25zM20.71 7.04a1.0 1.0 0 0 0 0-1.41l-2.34-2.34a1.0 1.0 0 0 0-1.41 0l-1.83 1.83 3.75 3.75 1.83-1.83z"/>
                    </svg>Edit
                </button>
                <button class="btn" data-act="del" data-id="${pair.id}">
                    <svg class="icon" viewBox="0 0 24 24">
                        <path d="M6 7h12v2H6zm2 3h8v10H8zM9 3h6v2H9z"/>
                    </svg>Delete
                </button>
            </div>
        </div>

        <div class="sync-detail">
            <div class="detail-sections">
                <div class="detail-section">
                    <div class="section-title">📁 Paths & Filters</div>
                    ${renderPathsSection(pair)}
                </div>
                <div class="detail-section">
                    <div class="section-title">⏱️ Schedule & Hooks</div>
                    ${renderScheduleSection(pair, scheduleInfo, lastRun, nextRun, hooksInfo)}
                </div>
            </div>
        </div>
    `;

    return card;
}

/**
 * Renders the paths and filters section
 */
function renderPathsSection(pair) {
    return `
        <div class="detail-field">
            <div class="field-label">Source directory</div>
            <div class="field-value short">${esc(pair.source)}</div>
        </div>
        <div class="detail-field">
            <div class="field-label">Target directory</div>
            <div class="field-value short">${esc(pair.target)}</div>
        </div>
        <div class="detail-field">
            <div class="field-label">Include extensions</div>
            <div class="field-value short">${renderExtensionPills(pair.includeExtensions)}</div>
        </div>
        ${pair.excludeGlobs?.length ? `
        <div class="detail-field">
            <div class="field-label">Exclude patterns</div>
            <div class="field-value short">
                <div class="pills-container">
                    ${pair.excludeGlobs.map(g => `<span class="pill">${esc(g)}</span>`).join('')}
                </div>
            </div>
        </div>` : ''}
        <div class="detail-field">
            <div class="field-label">Mirror deletes</div>
            <div class="field-value short">${pair.mirrorDeletes ? '✅ Enabled' : '❌ Disabled'}</div>
        </div>
    `;
}

/**
 * Renders the schedule and hooks section
 */
function renderScheduleSection(pair, scheduleInfo, lastRun, nextRun, hooksInfo) {
    return `
        <div class="detail-field">
            <div class="field-label">Schedule</div>
            <div class="field-value short">${esc(scheduleInfo)}</div>
        </div>
        <div class="detail-field">
            <div class="field-label">Last run</div>
            <div class="field-value short">${esc(lastRun)}</div>
        </div>
        <div class="detail-field">
            <div class="field-label">Next run</div>
            <div class="field-value short">${esc(nextRun)}</div>
        </div>
        <div class="detail-field">
            <div class="field-label">Hooks</div>
            <div class="field-value short">${hooksInfo}</div>
        </div>
    `;
}

/**
 * Utility functions for rendering pair information
 */
function renderExtensionPills(extensions) {
    if (!extensions || extensions.length === 0) {
        return '<span class="pill">All files</span>';
    }
    return extensions.map(ext => `<span class="pill">${esc(ext)}</span>`).join('');
}

function renderHooksInfo(pair) {
    if (!pair.hooks || pair.hooks.length === 0) {
        return '❌ No hooks configured';
    }
    const hookDescriptions = pair.hooks.map(hook => {
        if (hook.http) return `HTTP ${hook.http.method || 'POST'}`;
        if (hook.command) return `Command: ${hook.command.executable}`;
        return 'Unknown';
    });
    return `✅ ${hookDescriptions.join(', ')}`;
}

function renderScheduleInfo(pair) {
    const schedule = pair.schedule || {};
    switch (schedule.type) {
        case 'watcher': return 'File watcher (sync on changes)';
        case 'interval': return `Every ${schedule.interval || '?'}`;
        case 'cron': return `Cron: ${schedule.cronExpr || '?'}`;
        case 'custom': return `Custom schedule`;
        case 'disabled': return 'Manual only';
        default: return '—';
    }
}

function generateDescription(pair) {
    const types = pair.includeExtensions?.length ? pair.includeExtensions.join(', ') : 'all files';
    return `${renderScheduleInfo(pair)} of ${types}`;
}

/**
 * Header click handler that ignores clicks on action buttons
 */
window.headerClick = (event, element) => {
    if (event.target.closest('.sync-actions')) return;
    toggleExpanded(element);
};

function toggleExpanded(element) {
    element.classList.toggle('expanded');
}

// ===== SCHEDULE MODULE =====

/**
 * Schedule management functions
 */
function activeScheduleType() {
    const activeType = $('.schedule-type.active');
    return activeType ? activeType.dataset.type : 'watcher';
}

/**
 * Converts UI form data to schedule object
 */
function uiToSchedule() {
    const type = activeScheduleType();

    if (type === 'watcher') return { type: 'watcher' };
    if (type === 'disabled') return { type: 'disabled' };

    if (type === 'interval') {
        const startDateTime = $('#schedule-start-datetime').value;
        const endDateTime = $('#schedule-end-datetime').value;
        const seconds = parseInt($('#schedule-interval-seconds').value) || 900;
        const repeatAfter = $('#schedule-repeat-after').checked;

        return {
            type: 'interval',
            interval: `${seconds}s`,
            startDate: startDateTime ? new Date(startDateTime).toISOString() : null,
            endDate: endDateTime ? new Date(endDateTime).toISOString() : null,
            repeatAfterCompletion: repeatAfter
        };
    }

    if (type === 'cron') {
        return { type: 'cron', cronExpr: $('#schedule-cron').value.trim() };
    }

    if (type === 'custom') {
        return buildCustomSchedule();
    }
}

/**
 * Builds custom schedule configuration
 */
function buildCustomSchedule() {
    const customType = $('#custom-schedule-type').value;

    if (customType === 'repeating') {
        const days = $$('.weekday.active').map(x => Number(x.dataset.day));
        return {
            type: 'custom',
            custom: {
                scheduleType: 'repeating',
                weekDays: days,
                startTime: $('#schedule-start-time').value,
                endTime: $('#schedule-end-time').value,
                interval: $('#schedule-custom-interval').value
            }
        };
    } else {
        // Fixed schedule
        const months = $$('#fixed-schedule input[id^="month-"]:checked').map(x => parseInt(x.value));
        const days = $$('#fixed-schedule input[id^="day-"]:checked').map(x => parseInt(x.value));
        const weekdays = $$('#fixed-schedule input[id^="fixed-weekday-"]:checked').map(x => parseInt(x.value));
        const hours = $$('#fixed-schedule input[id^="hour-"]:checked').map(x => parseInt(x.value));
        const minutes = $$('#fixed-schedule input[id^="minute-"]:checked').map(x => parseInt(x.value));

        return {
            type: 'custom',
            custom: {
                scheduleType: 'fixed',
                months: months,
                days: days,
                weekDays: weekdays,
                hours: hours,
                minutes: minutes
            }
        };
    }
}

/**
 * Converts schedule object to UI form
 */
function scheduleToUI(schedule) {
    // Reset all schedule UI elements
    $$('.schedule-type').forEach(x => x.classList.remove('active'));
    $$('.schedule-config').forEach(x => x.classList.remove('active'));

    const type = schedule?.type || 'watcher';
    const tile = $(`.schedule-type[data-type="${type}"]`);
    if (tile) tile.classList.add('active');

    const config = $(`#${type}-config`);
    if (config) config.classList.add('active');

    if (type === 'interval') {
        populateIntervalSchedule(schedule);
    }

    if (type === 'cron') {
        $('#schedule-cron').value = schedule.cronExpr || '';
    }

    if (type === 'custom') {
        populateCustomSchedule(schedule.custom || {});
    }
}

/**
 * Populates interval schedule UI
 */
function populateIntervalSchedule(schedule) {
    if (schedule.startDate) {
        $('#schedule-start-datetime').value = new Date(schedule.startDate).toISOString().slice(0, 16);
    }
    if (schedule.endDate) {
        $('#schedule-end-datetime').value = new Date(schedule.endDate).toISOString().slice(0, 16);
    }

    const intervalMatch = schedule.interval?.match(/^(\d+)s?$/);
    if (intervalMatch) {
        $('#schedule-interval-seconds').value = intervalMatch[1];
    }

    $('#schedule-repeat-after').checked = schedule.repeatAfterCompletion !== false;
}

/**
 * Populates custom schedule UI
 */
function populateCustomSchedule(custom) {
    const scheduleType = custom.scheduleType || 'repeating';
    $('#custom-schedule-type').value = scheduleType;
    updateCustomScheduleType();

    if (scheduleType === 'repeating') {
        populateRepeatingSchedule(custom);
    } else {
        populateFixedSchedule(custom);
    }
}

/**
 * Populates repeating schedule UI
 */
function populateRepeatingSchedule(custom) {
    const days = new Set(custom.weekDays || []);
    $$('.weekday').forEach(weekday => {
        if (days.has(Number(weekday.dataset.day))) {
            weekday.classList.add('active');
        } else {
            weekday.classList.remove('active');
        }
    });
    $('#schedule-start-time').value = custom.startTime || '08:00';
    $('#schedule-end-time').value = custom.endTime || '20:00';
    $('#schedule-custom-interval').value = custom.interval || '1h30m';
}

/**
 * Populates fixed schedule UI
 */
function populateFixedSchedule(custom) {
    // Clear all checkboxes first
    $$('#fixed-schedule input[type="checkbox"]').forEach(cb => cb.checked = false);

    // Set selected values
    (custom.months || []).forEach(month => {
        const checkbox = $(`#month-${month}`);
        if (checkbox) checkbox.checked = true;
    });

    (custom.days || []).forEach(day => {
        const checkbox = $(`#day-${day}`);
        if (checkbox) checkbox.checked = true;
    });

    (custom.weekDays || []).forEach(weekday => {
        const checkbox = $(`#fixed-weekday-${weekday}`);
        if (checkbox) checkbox.checked = true;
    });

    (custom.hours || []).forEach(hour => {
        const checkbox = $(`#hour-${hour}`);
        if (checkbox) checkbox.checked = true;
    });

    (custom.minutes || []).forEach(minute => {
        const checkbox = $(`#minute-${minute}`);
        if (checkbox) checkbox.checked = true;
    });
}

function updateCustomScheduleType() {
    const type = $('#custom-schedule-type').value;
    const repeating = $('#repeating-schedule');
    const fixed = $('#fixed-schedule');

    if (type === 'repeating') {
        repeating.style.display = 'block';
        fixed.style.display = 'none';
    } else {
        repeating.style.display = 'none';
        fixed.style.display = 'block';
    }
}

// Global function for setting cron values (used by UI examples)
window.setScheduleCron = (value) => {
    const input = $('#schedule-cron');
    if (input) input.value = value;
};

// ===== MODAL MODULE =====

/**
 * Modal management and form handling
 */
const modal = $('#modalBackdrop');
const form = $('#pairForm');
const fields = {
    id: $('#f_id'),
    description: $('#f_description'),
    source: $('#f_source'),
    target: $('#f_target'),
    includeExtensions: $('#f_inc'),
    excludeGlobs: $('#f_exc'),
    mirrorDeletes: $('#f_mirr')
};

let editId = null;
let editingEnabled = false;
let currentHooks = [];

function openModal(title, pair) {
    $('#modalTitle').textContent = title;
    modal.style.display = 'flex';
    modal.setAttribute('aria-hidden', 'false');

    // Set default tab
    $$('.tab').forEach(t => t.classList.remove('active'));
    $$('.tab-pane').forEach(p => p.classList.remove('active'));
    $('.tab[data-tab="basic"]').classList.add('active');
    $('#basic-tab').classList.add('active');

    if (pair) {
        fillForm(pair);
    } else {
        resetForm();
    }
}

function closeModal() {
    modal.style.display = 'none';
    modal.setAttribute('aria-hidden', 'true');
}

function resetForm() {
    editId = null;
    editingEnabled = false;
    form.reset();
    currentHooks = [];
    renderHooks();
    scheduleToUI({ type: 'watcher' });
}

function fillForm(pair) {
    editId = pair.id;
    editingEnabled = !!pair.enabled;

    fields.id.value = pair.id;
    fields.description.value = pair.description || '';
    fields.source.value = pair.source || '';
    fields.target.value = pair.target || '';
    fields.includeExtensions.value = (pair.includeExtensions || []).join(',');
    fields.excludeGlobs.value = (pair.excludeGlobs || []).join(',');
    fields.mirrorDeletes.checked = !!pair.mirrorDeletes;

    currentHooks = processHooksForUI(pair.hooks || []);
    renderHooks();
    scheduleToUI(pair.schedule || { type: 'watcher' });
}

/**
 * Processes hooks data for UI rendering
 */
function processHooksForUI(hooks) {
    return hooks.map(hook => {
        const processedHook = {
            type: hook.http ? 'http' : 'command',
            matchExtensions: hook.matchExtensions || [],
            matchGlobs: hook.matchGlobs || [],
            command: hook.command || null
        };

        if (hook.http) {
            const httpConfig = { ...hook.http };

            // Convert headers object to array of rows
            httpConfig.headerRows = Object.entries(httpConfig.headers || {})
                .map(([name, value]) => ({ name, value }));
            if (httpConfig.headerRows.length === 0) {
                httpConfig.headerRows = [{ name: '', value: '' }];
            }

            // Extract URL parameters
            try {
                const url = new URL(httpConfig.url);
                httpConfig.paramRows = [...url.searchParams.entries()]
                    .map(([name, value]) => ({ name, value }));
                httpConfig.url = url.origin + url.pathname;
            } catch {
                httpConfig.paramRows = httpConfig.paramRows && httpConfig.paramRows.length ?
                    httpConfig.paramRows : [{ name: '', value: '' }];
            }

            if (!httpConfig.uiActiveTab) {
                httpConfig.uiActiveTab = 'params';
            }

            processedHook.http = httpConfig;
        }

        return processedHook;
    });
}

/**
 * Reads form data and converts to API format
 */
function readForm() {
    const pair = {
        id: fields.id.value.trim(),
        description: fields.description.value.trim(),
        enabled: editingEnabled,
        source: fields.source.value.trim(),
        target: fields.target.value.trim(),
        includeExtensions: processIncludeExtensions(),
        excludeGlobs: processExcludeGlobs(),
        mirrorDeletes: fields.mirrorDeletes.checked,
        hooks: processHooksFromUI(),
        schedule: uiToSchedule()
    };

    return pair;
}

/**
 * Processes include extensions from form input
 */
function processIncludeExtensions() {
    const raw = fields.includeExtensions.value.trim();
    if (!raw) return [];

    let parts = raw.split(',')
        .map(s => s.trim())
        .filter(Boolean)
        .map(s => s.toLowerCase())
        .map(s => s === '*' || s === '.*' ? s : (s.startsWith('.') ? s : '.' + s));

    parts = parts.filter(s => s !== '*' && s !== '.*');

    // Remove duplicates
    const seen = new Set();
    const unique = [];
    for (const ext of parts) {
        if (!seen.has(ext)) {
            seen.add(ext);
            unique.push(ext);
        }
    }

    return unique;
}

/**
 * Processes exclude globs from form input
 */
function processExcludeGlobs() {
    const raw = fields.excludeGlobs.value.trim();
    return raw ? raw.split(',').map(s => s.trim()).filter(Boolean) : [];
}

// ===== HOOKS MODULE =====

/**
 * Hook management functions
 */
function renderHooks() {
    const container = $('#hooksContainer');
    if (!container) return;

    container.innerHTML = currentHooks.map((hook, index) => {
        return createHookHTML(hook, index);
    }).join('');
}

/**
 * Creates HTML for a single hook
 */
function createHookHTML(hook, index) {
    const isHttp = hook.type === 'http';

    if (isHttp) {
        return createHttpHookHTML(hook, index);
    } else {
        return createCommandHookHTML(hook, index);
    }
}

/**
 * Creates HTML for HTTP hook
 */
function createHttpHookHTML(hook, index) {
    const http = hook.http || {};
    const activeTab = http.uiActiveTab || 'params';

    return `
        <div class="hook">
            <div class="hook-header">
                <div class="hook-type">
                    <select onchange="updateHook(${index}, 'type', this.value)">
                        <option value="http" selected>HTTP</option>
                        <option value="command">Command</option>
                    </select>
                    <div class="pills-container">
                        ${(hook.matchExtensions || []).map(e => `<span class="pill">${esc(e)}</span>`).join('')}
                        ${(hook.matchGlobs || []).map(g => `<span class="pill">${esc(g)}</span>`).join('')}
                    </div>
                </div>
                <div class="hook-actions">
                    <button class="btn" title="Which files trigger this hook (extensions & globs)"
                            onclick="editHookFilters(${index})">File filters…</button>
                    <button class="btn" onclick="removeHook(${index})">Remove</button>
                </div>
            </div>

            <div class="hook-body">
                <div class="http-inline">
                    <select class="method-select" onchange="updateHook(${index}, 'http.method', this.value)">
                        ${['GET', 'POST', 'PUT', 'PATCH', 'DELETE'].map(method => `
                            <option value="${method}" ${(http.method || 'POST').toUpperCase() === method ? 'selected' : ''}>
                                ${method}
                            </option>
                        `).join('')}
                    </select>
                    <input class="url-input" placeholder="https://server/api"
                           value="${esc(http.url || '')}"
                           onchange="updateHook(${index}, 'http.url', this.value)">
                </div>

                <div class="tabs">
                    <button class="tab ${activeTab === 'params' ? 'active' : ''}" 
                            onclick="switchHookTab(${index}, 'params')">Params</button>
                    <button class="tab ${activeTab === 'headers' ? 'active' : ''}" 
                            onclick="switchHookTab(${index}, 'headers')">Headers</button>
                    <button class="tab ${activeTab === 'body' ? 'active' : ''}" 
                            onclick="switchHookTab(${index}, 'body')">Body</button>
                </div>

                <div class="tabpanes">
                    <div class="tabpane ${activeTab === 'params' ? 'active' : ''}">
                        ${renderKeyValueRows(http.paramRows || [{ name: '', value: '' }], index, 'param')}
                    </div>
                    <div class="tabpane ${activeTab === 'headers' ? 'active' : ''}">
                        ${renderKeyValueRows(http.headerRows || [{ name: '', value: '' }], index, 'header')}
                    </div>
                    <div class="tabpane ${activeTab === 'body' ? 'active' : ''}">
                        <label>Body (optional)</label>
                        <textarea placeholder="Leave empty. If method ≠ GET and Params are set, they will be sent as JSON body. Use {fileName} to inject path."
                                  onchange="updateHook(${index}, 'http.bodyTemplate', this.value)">${esc(http.bodyTemplate || '')}</textarea>
                    </div>
                </div>
            </div>
        </div>
    `;
}

/**
 * Creates HTML for command hook
 */
function createCommandHookHTML(hook, index) {
    return `
        <div class="hook">
            <div class="hook-header">
                <div class="hook-type">
                    <select onchange="updateHook(${index}, 'type', this.value)">
                        <option value="http">HTTP</option>
                        <option value="command" selected>Command</option>
                    </select>
                    <div class="pills-container">
                        ${(hook.matchExtensions || []).map(e => `<span class="pill">${esc(e)}</span>`).join('')}
                        ${(hook.matchGlobs || []).map(g => `<span class="pill">${esc(g)}</span>`).join('')}
                    </div>
                </div>
                <div class="hook-actions">
                    <button class="btn" title="Which files trigger this hook (extensions & globs)"
                            onclick="editHookFilters(${index})">File filters…</button>
                    <button class="btn" onclick="removeHook(${index})">Remove</button>
                </div>
            </div>

            <div class="hook-body">
                <label>Executable</label>
                <input value="${esc(hook.command?.executable || '')}" 
                       placeholder="curl, powershell, bash"
                       onchange="updateHook(${index}, 'command.executable', this.value)">
                
                <label>Arguments (JSON array)</label>
                <textarea placeholder='["-X","POST","http://server/api","-d","file={{.RelPath}}"]'
                          onchange="try{ updateHook(${index}, 'command.args', JSON.parse(this.value||'[]')) }catch(e){ toast('Invalid JSON','err') }">${hook.command?.args ? esc(JSON.stringify(hook.command.args, null, 2)) : ''}</textarea>
                
                <label>Work dir</label>
                <input value="${esc(hook.command?.workDir || '')}" 
                       onchange="updateHook(${index}, 'command.workDir', this.value)">
                
                <label>Env (JSON object)</label>
                <textarea placeholder='{"API_KEY":"..."}'
                          onchange="try{ updateHook(${index}, 'command.envVars', JSON.parse(this.value||'{}')) }catch(e){ toast('Invalid JSON','err') }">${hook.command?.envVars ? esc(JSON.stringify(hook.command.envVars, null, 2)) : ''}</textarea>
            </div>
        </div>
    `;
}

/**
 * Renders key-value rows for parameters or headers
 */
function renderKeyValueRows(rows, hookIndex, type) {
    const addFunction = type === 'header' ? 'addHeaderRowUI' : 'addParamRowUI';
    const removeFunction = type === 'header' ? 'removeHeaderRow' : 'removeParamRow';
    const updateFunction = type === 'header' ? 'updateHeaderKV' : 'updateParamKV';
    const namePlaceholder = type === 'header' ? 'Header name (e.g. Authorization)' : 'Param name (e.g. service)';
    const valuePlaceholder = type === 'header' ? 'Header value' : 'Param value';

    return `
        <div class="kv-table">
            ${rows.map((row, rowIndex) => `
                <div class="kv-row">
                    <input class="kv-name" placeholder="${namePlaceholder}"
                           value="${esc(row.name || '')}"
                           oninput="${updateFunction}(${hookIndex}, ${rowIndex}, 'name', this, false)"
                           onblur="${updateFunction}(${hookIndex}, ${rowIndex}, 'name', this, true)">
                    <input class="kv-val" placeholder="${valuePlaceholder}"
                           value="${esc(row.value || '')}"
                           oninput="${updateFunction}(${hookIndex}, ${rowIndex}, 'value', this, false)"
                           onblur="${updateFunction}(${hookIndex}, ${rowIndex}, 'value', this, true)">
                    <button type="button" class="btn icon-btn" title="Remove"
                            onclick="${removeFunction}(${hookIndex}, ${rowIndex})">
                        <svg class="icon" viewBox="0 0 24 24">
                            <path d="M6 7h12v2H6zm2 3h8v10H8zM9 3h6v2H9z"/>
                        </svg>
                    </button>
                </div>
            `).join('')}
            <button type="button" class="btn" onclick="${addFunction}(${hookIndex})">+ Add ${type}</button>
        </div>
    `;
}

/**
 * Hook management utility functions
 */
function switchHookTab(index, tab) {
    const hook = currentHooks[index];
    if (!hook || hook.type !== 'http') return;

    if (!hook.http) hook.http = {};
    hook.http.uiActiveTab = tab;
    renderHooks();
}

function updateHook(index, path, value) {
    const segments = path.split('.');
    let object = currentHooks[index];

    while (segments.length > 1) {
        const key = segments.shift();
        object[key] = object[key] ?? {};
        object = object[key];
    }

    object[segments[0]] = value;
    renderHooks();
}

function editHookFilters(index) {
    const hook = currentHooks[index];
    const extensions = prompt('Match extensions (comma):', (hook.matchExtensions || []).join(',')) ?? '';
    const globs = prompt('Match globs (comma):', (hook.matchGlobs || []).join(',')) ?? '';

    hook.matchExtensions = extensions.split(',').map(s => s.trim()).filter(Boolean);
    hook.matchGlobs = globs.split(',').map(s => s.trim()).filter(Boolean);
    renderHooks();
}

function addHook() {
    currentHooks.push({
        type: 'http',
        matchExtensions: [],
        matchGlobs: [],
        http: {
            method: 'POST',
            url: '',
            headers: {},
            bodyTemplate: '',
            headerRows: [{ name: '', value: '' }],
            paramRows: [{ name: '', value: '' }],
            uiActiveTab: 'params'
        },
        command: null
    });
    renderHooks();
}

function removeHook(index) {
    currentHooks.splice(index, 1);
    renderHooks();
}

function showHookExamples() {
    const example = {
        type: 'http',
        matchExtensions: ['.jar'],
        http: {
            method: 'POST',
            url: 'http://server/api/deploy/notify',
            headers: { 'Authorization': 'Bearer YOUR_TOKEN' },
            bodyTemplate: '{"file":"{{.RelPath}}","timestamp":"{{.Timestamp}}"}'
        }
    };

    if (confirm('Add example HTTP hook?')) {
        currentHooks.push(example);
        renderHooks();
    }
}

async function testHooks() {
    if (!editId) {
        toast('Save pair first', 'err');
        return;
    }

    try {
        const result = await api.testHook(editId);
        toast('Hooks executed: ' + JSON.stringify(result.status || result), 'ok');
    } catch (error) {
        toast('Hook test failed: ' + error.message, 'err');
    }
}

/**
 * Header/Parameter row management functions
 */
function ensureHeaderRows(hook) {
    if (!hook.http) {
        hook.http = { method: 'POST', url: '', headers: {}, bodyTemplate: '' };
    }
    if (!Array.isArray(hook.http.headerRows)) {
        hook.http.headerRows = Object.entries(hook.http.headers || {})
            .map(([name, value]) => ({ name, value }));
    }
    if (hook.http.headerRows.length === 0) {
        hook.http.headerRows.push({ name: '', value: '' });
    }
}

function ensureParamRows(hook) {
    if (!hook.http) {
        hook.http = { method: 'POST', url: '', headers: {}, bodyTemplate: '' };
    }
    if (!Array.isArray(hook.http.paramRows)) {
        hook.http.paramRows = [{ name: '', value: '' }];
    }
    if (hook.http.paramRows.length === 0) {
        hook.http.paramRows.push({ name: '', value: '' });
    }
}

function addHeaderRowUI(index) {
    const hook = currentHooks[index];
    ensureHeaderRows(hook);
    hook.http.headerRows.push({ name: '', value: '' });
    renderHooks();
}

function removeHeaderRow(index, rowIndex) {
    const rows = currentHooks[index].http.headerRows || [];
    rows.splice(rowIndex, 1);
    if (rows.length === 0) {
        rows.push({ name: '', value: '' });
    }
    renderHooks();
}

function updateHeaderKV(index, rowIndex, field, element, commit) {
    const rows = (currentHooks[index]?.http?.headerRows) || [];
    const row = rows[rowIndex];
    if (!row) return;

    row[field] = element.value;

    // Auto-convert Authorization header to Basic token
    if (commit && field === 'value' && (row.name || '').toLowerCase() === 'authorization') {
        const raw = (element.value || '').trim();
        if (raw && !/^basic\s+/i.test(raw) && raw.includes(':')) {
            const token = 'Basic ' + btoa(raw);
            element.value = token;
            row.value = token;

            try {
                element.setSelectionRange(token.length, token.length);
            } catch {}

            toast('Authorization converted to Basic token', 'ok');
        }
    }
}

function addParamRowUI(index) {
    const hook = currentHooks[index];
    ensureParamRows(hook);
    hook.http.paramRows.push({ name: '', value: '' });
    renderHooks();
}

function removeParamRow(index, rowIndex) {
    const rows = currentHooks[index].http.paramRows || [];
    rows.splice(rowIndex, 1);
    if (rows.length === 0) {
        rows.push({ name: '', value: '' });
    }
    renderHooks();
}

function updateParamKV(index, rowIndex, field, element, commit) {
    const rows = (currentHooks[index]?.http?.paramRows) || [];
    const row = rows[rowIndex];
    if (!row) return;

    row[field] = element.value;
}

/**
 * Processes hooks from UI to API format
 */
function processHooksFromUI() {
    return currentHooks.map(hook => {
        const processed = {
            matchExtensions: hook.matchExtensions || [],
            matchGlobs: hook.matchGlobs || []
        };

        if (hook.type === 'http' && hook.http && hook.http.url) {
            processed.http = processHttpHook(hook.http);
        } else if (hook.type === 'command' && hook.command && hook.command.executable) {
            processed.command = hook.command;
        }

        return processed;
    }).filter(hook => hook.http || hook.command);
}

/**
 * Processes HTTP hook configuration
 */
function processHttpHook(http) {
    // Process headers
    const headerRows = (http.headerRows || []).filter(row => (row.name || '').trim());
    const headers = {};
    for (const row of headerRows) {
        let value = row.value ?? '';
        // Auto-convert basic auth if needed
        if ((row.name || '').toLowerCase() === 'authorization' && value && !/^basic\s+/i.test(value) && value.includes(':')) {
            value = 'Basic ' + btoa(value);
        }
        headers[row.name.trim()] = value;
    }

    // Process parameters
    const paramRows = (http.paramRows || []).filter(row => (row.name || '').trim());
    const params = {};
    for (const row of paramRows) {
        params[row.name.trim()] = row.value ?? '';
    }

    // Build URL with query parameters
    let url = withQuery(http.url, params);

    // Process body template
    const method = (http.method || 'POST').toUpperCase();
    let bodyTemplate = (http.bodyTemplate || '');

    // If no body template and method is not GET, use params as JSON body
    if (method !== 'GET' && !bodyTemplate.trim() && paramRows.length > 0) {
        try {
            bodyTemplate = JSON.stringify(params);
        } catch {}
    }

    // Replace {fileName} template with Go template syntax
    if (bodyTemplate && bodyTemplate.includes('{fileName}')) {
        bodyTemplate = bodyTemplate.split('{fileName}').join('{{.RelPath}}');
    }

    return {
        method,
        url,
        headers,
        bodyTemplate
    };
}

/**
 * Utility function to add query parameters to URL
 */
function withQuery(url, paramsObject) {
    if (!paramsObject || Object.keys(paramsObject).length === 0) {
        return url;
    }

    try {
        const urlObject = new URL(url);
        Object.entries(paramsObject).forEach(([key, value]) => {
            if (key?.trim()) {
                urlObject.searchParams.append(key.trim(), String(value ?? ''));
            }
        });
        return urlObject.toString();
    } catch {
        // Fallback for relative URLs
        const queryString = Object.entries(paramsObject)
            .map(([key, value]) => `${encodeURIComponent(key)}=${encodeURIComponent(String(value ?? ''))}`)
            .join('&');
        return url + (url.includes('?') ? '&' : '?') + queryString;
    }
}

// ===== TAB MANAGEMENT MODULE =====

/**
 * Initialize all tab functionality
 */
function initializeTabs() {
    // Modal tabs
    $$('.tab').forEach(tab => {
        tab.addEventListener('click', () => {
            $$('.tab').forEach(t => t.classList.remove('active'));
            $$('.tab-pane').forEach(p => p.classList.remove('active'));
            tab.classList.add('active');

            const pane = $('#' + tab.dataset.tab + '-tab');
            if (pane) pane.classList.add('active');
        });
    });

    // Schedule type selector
    $$('.schedule-type').forEach(element => {
        element.addEventListener('click', () => {
            $$('.schedule-type').forEach(x => x.classList.remove('active'));
            $$('.schedule-config').forEach(x => x.classList.remove('active'));
            element.classList.add('active');

            const config = $('#' + element.dataset.type + '-config');
            if (config) config.classList.add('active');
        });
    });

    // Weekday selector for repeating schedules
    document.addEventListener('click', (event) => {
        const weekday = event.target.closest('.weekday');
        if (!weekday) return;
        weekday.classList.toggle('active');
    });

    // Custom schedule type selector
    const customTypeSelect = $('#custom-schedule-type');
    if (customTypeSelect) {
        customTypeSelect.addEventListener('change', updateCustomScheduleType);
    }

    // Generate fixed schedule selectors
    generateFixedScheduleSelectors();
}

/**
 * Generate day, hour, and minute selectors for fixed schedules
 */
function generateFixedScheduleSelectors() {
    generateDaySelectors();
    generateHourSelectors();
    generateMinuteSelectors();
}

function generateDaySelectors() {
    const daysSelector = $('.days-selector');
    if (!daysSelector) return;

    for (let day = 1; day <= 31; day++) {
        const div = document.createElement('div');
        div.className = 'day-checkbox';
        div.innerHTML = `
            <input type="checkbox" id="day-${day}" value="${day}">
            <label for="day-${day}">${day}</label>
        `;
        daysSelector.appendChild(div);
    }
}

function generateHourSelectors() {
    const hoursSelector = $('.hours-selector');
    if (!hoursSelector) return;

    for (let hour = 0; hour <= 23; hour++) {
        const div = document.createElement('div');
        div.className = 'hour-checkbox';
        const hourString = hour.toString().padStart(2, '0');
        div.innerHTML = `
            <input type="checkbox" id="hour-${hour}" value="${hour}">
            <label for="hour-${hour}">${hourString}</label>
        `;
        hoursSelector.appendChild(div);
    }
}

function generateMinuteSelectors() {
    const minutesSelector = $('.minutes-selector');
    if (!minutesSelector) return;

    const commonMinutes = [0, 5, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55];
    commonMinutes.forEach(minute => {
        const div = document.createElement('div');
        div.className = 'minute-checkbox';
        const minuteString = minute.toString().padStart(2, '0');
        div.innerHTML = `
            <input type="checkbox" id="minute-${minute}" value="${minute}">
            <label for="minute-${minute}">${minuteString}</label>
        `;
        minutesSelector.appendChild(div);
    });
}

// ===== EVENT HANDLERS MODULE =====

/**
 * Initialize all event handlers
 */
function initializeEventHandlers() {
    // Main action buttons
    $('#newPairBtn').addEventListener('click', () => openModal('New Pair'));
    $('#closeModal').addEventListener('click', closeModal);
    $('#cancelBtn').addEventListener('click', closeModal);

    // Sync all button
    $('#syncAllBtn').addEventListener('click', handleSyncAll);

    // Keyboard shortcuts
    document.addEventListener('keydown', (event) => {
        if (event.key === 'R' || event.key === 'r') {
            refresh();
        }
    });

    // Delegated click handling for sync pair action buttons
    syncItems.addEventListener('click', handleSyncItemAction);

    // Form submission
    form.addEventListener('submit', handleFormSubmit);
}

/**
 * Handle sync all button click
 */
async function handleSyncAll() {
    try {
        $('#syncAllBtn').disabled = true;
        const result = await api.syncAll();
        toast(`Synced ${result.files} files (${result.bytes} bytes)`);
    } catch (error) {
        toast('Sync all failed: ' + error.message, 'err');
    } finally {
        $('#syncAllBtn').disabled = false;
    }
}

/**
 * Handle sync pair action button clicks
 */
async function handleSyncItemAction(event) {
    const button = event.target.closest('button[data-act]');
    if (!button) return;

    const id = button.dataset.id;
    const action = button.dataset.act;

    button.disabled = true;

    try {
        if (action === 'start') {
            await api.start(id);
        } else if (action === 'stop') {
            await api.stop(id);
        } else if (action === 'sync') {
            await api.sync(id);
        } else if (action === 'edit') {
            const pairs = await api.list();
            openModal('Edit Pair', pairs.find(pair => pair.id === id));
            button.disabled = false;
            return;
        } else if (action === 'del') {
            if (confirm(`Delete pair "${id}"?`)) {
                await api.remove(id);
            } else {
                button.disabled = false;
                return;
            }
        }

        await refresh();
    } catch (error) {
        toast(`Action failed: ${error.message}`, 'err');
    } finally {
        button.disabled = false;
    }
}

/**
 * Handle form submission
 */
async function handleFormSubmit(event) {
    event.preventDefault();

    const pair = readForm();
    if (!pair.id || !pair.source || !pair.target) {
        toast('ID, Source and Target are required', 'err');
        return;
    }

    try {
        $('#saveBtn').disabled = true;

        if (editId) {
            await api.update(editId, pair);
        } else {
            await api.create(pair);
        }

        toast('Saved');
        closeModal();
        await refresh();
    } catch (error) {
        toast('Save failed: ' + error.message, 'err');
    } finally {
        $('#saveBtn').disabled = false;
    }
}

// ===== DATA MANAGEMENT MODULE =====

/**
 * Refresh the sync pairs list from API
 */
async function refresh() {
    try {
        const pairs = await api.list();
        renderPairs(pairs);
    } catch (error) {
        toast('Failed to load pairs: ' + error.message, 'err');
    }
}

// ===== INITIALIZATION MODULE =====

/**
 * Initialize the application when DOM is ready
 */
document.addEventListener('DOMContentLoaded', () => {
    initializeTabs();
    initializeEventHandlers();
    refresh();
});

// ===== GLOBAL FUNCTION EXPORTS =====

/**
 * Expose functions that need to be called from inline HTML handlers
 * Note: In a production app, consider moving away from inline handlers
 * and using proper event delegation instead
 */
window.addHook = addHook;
window.showHookExamples = showHookExamples;
window.testHooks = testHooks;
window.selectFolder = selectFolder;
window.addHeaderRowUI = addHeaderRowUI;
window.removeHeaderRow = removeHeaderRow;
window.updateHeaderKV = updateHeaderKV;
window.addParamRowUI = addParamRowUI;
window.removeParamRow = removeParamRow;
window.updateParamKV = updateParamKV;
window.switchHookTab = switchHookTab;