// settings.js — app settings, API keys, prompt filter panels.
//
// Imports loadStats + setActivePassword from ../app.js at runtime (settings
// changes trigger a stats refresh / password update). Circular but safe: those
// calls are inside handler bodies, never at module load time.

import { state } from './state.js';
import {
  $,
  api,
  bindDialogBackdropClose,
  closeDialog,
  confirmAction,
  copyText,
  escapeAttr,
  escapeHtml,
  openDialog,
  refreshCustomSelects,
  t,
  toast,
  toastError,
  toastPrimary,
} from './core.js';
import { loadStats, setActivePassword } from '../app.js';

export async function loadSettings() {
  const res = await api('/settings');
  const d = await res.json();
  $('requireApiKey').checked = d.requireApiKey;
  $('allowOverUsage').checked = d.allowOverUsage || false;
  await Promise.all([loadThinkingConfig(), loadEndpointConfig(), loadProxyConfig(), loadPromptFilter(), loadBillingConfig(), loadTelegramConfig(), loadApiKeys(), loadSecuritySettings()]);
  refreshCustomSelects();
}
export async function loadThinkingConfig() {
  const res = await api('/thinking');
  const d = await res.json();
  $('thinkingSuffix').value = d.suffix || '-thinking';
  $('openaiThinkingFormat').value = d.openaiFormat || 'reasoning_content';
  $('claudeThinkingFormat').value = d.claudeFormat || 'thinking';
}
export async function saveThinkingConfig() {
  const res = await api('/thinking', {
    method: 'POST', body: JSON.stringify({
      suffix: $('thinkingSuffix').value || '-thinking',
      openaiFormat: $('openaiThinkingFormat').value,
      claudeFormat: $('claudeThinkingFormat').value
    })
  });
  const d = await res.json();
  if (d.success) toast(t('settings.thinkingSaved'), 'success');
  else toast(t('common.saveFailed') + ': ' + (d.error || ''), 'error');
}
export async function loadEndpointConfig() {
  const res = await api('/endpoint');
  const d = await res.json();
  $('preferredEndpoint').value = d.preferredEndpoint || 'auto';
  $('endpointFallback').checked = d.endpointFallback !== false;
}
export async function saveEndpointConfig() {
  const res = await api('/endpoint', {
    method: 'POST', body: JSON.stringify({
      preferredEndpoint: $('preferredEndpoint').value,
      endpointFallback: $('endpointFallback').checked
    })
  });
  const d = await res.json();
  if (d.success) toast(t('settings.endpointSaved'), 'success');
  else toast(t('common.saveFailed') + ': ' + (d.error || ''), 'error');
}
export async function loadProxyConfig() {
  const res = await api('/proxy');
  const d = await res.json();
  const url = d.proxyURL || '';
  if (!url) {
    $('proxyType').value = 'none';
    $('proxyFields').classList.add('hidden');
    return;
  }
  try {
    const u = new URL(url);
    const scheme = u.protocol.replace(':', '');
    $('proxyType').value = scheme.startsWith('socks5') ? 'socks5' : 'http';
    $('proxyHost').value = u.hostname;
    $('proxyPort').value = u.port;
    $('proxyUsername').value = decodeURIComponent(u.username);
    $('proxyPassword').value = decodeURIComponent(u.password);
    $('proxyFields').classList.remove('hidden');
  } catch (e) {
    $('proxyType').value = 'none';
    $('proxyFields').classList.add('hidden');
  }
}
export function onProxyTypeChange() {
  const type = $('proxyType').value;
  $('proxyFields').classList.toggle('hidden', type === 'none');
}
export async function saveProxyConfig() {
  const type = $('proxyType').value;
  let url = '';
  if (type !== 'none') {
    const host = $('proxyHost').value.trim();
    const port = $('proxyPort').value.trim();
    if (!host || !port) { toast(t('settings.proxyHostRequired'), 'warning'); return; }
    const u = $('proxyUsername').value.trim();
    const p = $('proxyPassword').value.trim();
    const auth = u ? (p ? encodeURIComponent(u) + ':' + encodeURIComponent(p) + '@' : encodeURIComponent(u) + '@') : '';
    url = type + '://' + auth + host + ':' + port;
  }
  const res = await api('/proxy', { method: 'POST', body: JSON.stringify({ proxyURL: url }) });
  const d = await res.json();
  if (d.success) toast(t('settings.proxySaved'), 'success');
  else toast(t('common.saveFailed') + ': ' + (d.error || ''), 'error');
}
export async function saveRequireApiKey() {
  try {
    const requireApiKey = $('requireApiKey').checked;
    if (requireApiKey) {
      const hasEnabledKey = Array.isArray(state.apiKeysCache) && state.apiKeysCache.some(k => k && k.enabled);
      if (!hasEnabledKey) {
        if (!confirm(t('apiKeys.requireWithoutEnabledKeyWarning'))) {
          $('requireApiKey').checked = false;
          return;
        }
      }
    }
    const res = await api('/settings', { method: 'POST', body: JSON.stringify({ requireApiKey }) });
    const d = await res.json().catch(() => ({}));
    if (!res.ok || d.success === false) throw new Error(d.error || t('common.saveFailed'));
    toast(t('detail.saved'), 'success');
  } catch (e) {
    toast((e && e.message) || t('common.saveFailed'), 'error');
  }
}
export async function saveOverUsageConfig() {
  const allowOverUsage = $('allowOverUsage').checked;
  await api('/settings', { method: 'POST', body: JSON.stringify({ allowOverUsage }) });
  toast(t('settings.overUsageSaved'), 'success');
}
export async function changePassword() {
  const np = $('newPassword').value;
  if (!np) return toast(t('settings.passwordRequired'), 'warning');
  try {
    const res = await api('/settings', { method: 'POST', body: JSON.stringify({ password: np }) });
    const d = await res.json().catch(() => ({}));
    if (!res.ok || d.success === false) throw new Error(d.error || t('common.saveFailed'));
    setActivePassword(np, localStorage.getItem('kiro_remember') === '1');
    toast(t('settings.passwordChanged'), 'success');
    $('newPassword').value = '';
  } catch (e) {
    toast((e && e.message) || t('common.saveFailed'), 'error');
  }
}
export async function resetStats() {
  const ok = await confirmAction(t('settings.confirmReset'), {
    title: t('settings.statistics'),
    confirmText: t('settings.resetStats'),
    variant: 'danger'
  });
  if (!ok) return;
  try {
    const res = await api('/stats/reset', { method: 'POST' });
    if (!res.ok) throw new Error(t('common.failed'));
    loadStats();
    toastPrimary(t('settings.statsReset'));
  } catch (e) {
    toastError((e && e.message) || t('common.failed'));
  }
}
// Multi API Key management

export async function loadApiKeys(opts = {}) {
  const render = opts.render !== false;
  const list = $('apiKeysList');
  try {
    const res = await api('/api-keys');
    if (!res.ok) throw new Error('http ' + res.status);
    const d = await res.json();
    state.apiKeysCache = Array.isArray(d.apiKeys) ? d.apiKeys : [];
    clampApiKeysPage();
    if (render) renderApiKeys();
    renderOverviewApiKeyStats();
  } catch (e) {
    state.apiKeysCache = [];
    if (list && render) {
      list.innerHTML = '<div class="empty-state">' + escapeHtml(t('apiKeys.loadFailed')) + '</div>';
    }
    renderOverviewApiKeyStats();
  }
}

export function formatNumber(n) {
  if (n == null || isNaN(n)) return '0';
  if (Math.abs(n) >= 1 && Math.floor(n) === n) return Number(n).toLocaleString('en-US');
  return Number(n).toLocaleString('en-US', { maximumFractionDigits: 4 });
}

export function usageBar(used, limit) {
  if (!limit || limit <= 0) return '';
  const ratio = Math.max(0, Math.min(1, used / limit));
  const pct = (ratio * 100).toFixed(1);
  let fillClass = 'usage-fill';
  if (ratio >= 0.95) fillClass += ' critical';
  else if (ratio >= 0.8) fillClass += ' high';
  return '<div class="usage-bar" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow="' + Math.round(ratio * 100) + '">' +
    '<div class="' + fillClass + '" style="width:' + pct + '%;"></div>' +
    '</div>';
}

export function usageLine(label, used, limit, options) {
  options = options || {};
  const fmt = options.fmt || formatNumber;
  const usedText = fmt(used);
  const limitText = (!limit || limit <= 0) ? t('apiKeys.unlimited') : fmt(limit);
  return '<div class="api-key-usage-item">' +
    '<div class="api-key-usage-label"><span>' + escapeHtml(label) + '</span><strong>' +
    escapeHtml(usedText) + ' / ' + escapeHtml(limitText) +
    '</strong></div>' +
    usageBar(used, limit) +
    '</div>';
}

function usageCell(used, limit) {
  const usedText = formatNumber(used || 0);
  const limitText = (!limit || limit <= 0) ? t('apiKeys.unlimited') : formatNumber(limit);
  return '<div class="api-keys-usage-cell">' +
    '<div class="api-keys-usage-text">' + escapeHtml(usedText) + ' / ' + escapeHtml(limitText) + '</div>' +
    usageBar(used || 0, limit || 0) +
    '</div>';
}

export function getFilteredApiKeys() {
  const kw = (state.apiKeysFilterKeyword || '').trim().toLowerCase();
  const status = state.apiKeysFilterStatus || 'all';
  return (state.apiKeysCache || []).filter(item => {
    if (!item) return false;
    if (status === 'enabled' && (!item.enabled || item.expired)) return false;
    if (status === 'disabled' && item.enabled) return false;
    if (status === 'expired' && !item.expired) return false;
    if (kw) {
      const name = (item.name || '').toLowerCase();
      const masked = (item.keyMasked || '').toLowerCase();
      const id = (item.id || '').toLowerCase();
      let plain = '';
      if (state.apiKeyRevealCache && state.apiKeyRevealCache[item.id]) {
        plain = String(state.apiKeyRevealCache[item.id]).toLowerCase();
      }
      if (!(name.includes(kw) || masked.includes(kw) || id.includes(kw) || plain.includes(kw))) {
        return false;
      }
    }
    return true;
  });
}

export function clampApiKeysPage() {
  const size = Math.max(1, parseInt(state.apiKeysPageSize, 10) || 20);
  state.apiKeysPageSize = size;
  const total = getFilteredApiKeys().length;
  const pages = Math.max(1, Math.ceil(total / size) || 1);
  let page = parseInt(state.apiKeysPage, 10) || 1;
  if (page < 1) page = 1;
  if (page > pages) page = pages;
  state.apiKeysPage = page;
}

export function getApiKeysPageSlice() {
  clampApiKeysPage();
  const filtered = getFilteredApiKeys();
  const size = state.apiKeysPageSize;
  const page = state.apiKeysPage;
  const start = (page - 1) * size;
  return {
    filtered,
    total: filtered.length,
    page,
    size,
    pages: Math.max(1, Math.ceil(filtered.length / size) || 1),
    start,
    items: filtered.slice(start, start + size)
  };
}

export function onApiKeysFilterChange() {
  const search = $('apiKeysSearch');
  const status = $('apiKeysStatusFilter');
  const pageSize = $('apiKeysPageSize');
  if (search) state.apiKeysFilterKeyword = search.value || '';
  if (status) state.apiKeysFilterStatus = status.value || 'all';
  if (pageSize) {
    const n = parseInt(pageSize.value, 10);
    state.apiKeysPageSize = (!isNaN(n) && n > 0) ? n : 20;
  }
  state.apiKeysPage = 1;
  renderApiKeys();
}

export function setApiKeysPage(page) {
  state.apiKeysPage = page;
  clampApiKeysPage();
  renderApiKeys();
}

function syncApiKeysToolbarFromState() {
  const search = $('apiKeysSearch');
  const status = $('apiKeysStatusFilter');
  const pageSize = $('apiKeysPageSize');
  if (search && search.value !== (state.apiKeysFilterKeyword || '')) {
    search.value = state.apiKeysFilterKeyword || '';
  }
  if (status && status.value !== (state.apiKeysFilterStatus || 'all')) {
    status.value = state.apiKeysFilterStatus || 'all';
  }
  if (pageSize) {
    const val = String(state.apiKeysPageSize || 20);
    if (pageSize.value !== val) pageSize.value = val;
  }
}

export function renderApiKeys() {
  const list = $('apiKeysList');
  if (!list) return;
  syncApiKeysToolbarFromState();

  if (!state.apiKeysCache.length) {
    list.innerHTML = '<div class="empty-state">' + escapeHtml(t('apiKeys.empty')) + '</div>';
    return;
  }

  const slice = getApiKeysPageSlice();
  if (!slice.total) {
    list.innerHTML = '<div class="empty-state">' + escapeHtml(t('apiKeys.noMatches')) + '</div>';
    return;
  }

  const rows = slice.items.map(item => {
    const id = escapeAttr(item.id || '');
    const rawId = item.id || '';
    const name = item.name
      ? escapeHtml(item.name)
      : '<span class="muted-text">' + escapeHtml(t('apiKeys.unnamed')) + '</span>';
    const revealed = !!(state.apiKeyRevealed && state.apiKeyRevealed[rawId]);
    const plain = (state.apiKeyRevealCache && state.apiKeyRevealCache[rawId]) || '';
    const displayKey = revealed && plain ? plain : (item.keyMasked || '');
    const keyText = escapeHtml(displayKey);
    const eyeIcon = revealed ? 'fa-solid fa-eye-slash' : 'fa-solid fa-eye';
    const eyeLabel = revealed ? t('apiKeys.hideKey') : t('apiKeys.showKey');
    const badges = [];
    if (item.enabled && !item.expired) {
      badges.push('<span class="badge badge-success">' + escapeHtml(t('apiKeys.statusEnabled')) + '</span>');
    }
    if (!item.enabled) {
      badges.push('<span class="badge badge-error">' + escapeHtml(t('apiKeys.disabled')) + '</span>');
    }
    if (item.expired) {
      badges.push('<span class="badge badge-error">' + escapeHtml(t('apiKeys.expired')) + '</span>');
    }
    if (item.migrated) {
      badges.push('<span class="badge badge-info">' + escapeHtml(t('apiKeys.migrated')) + '</span>');
    }
    const expiryText = item.expiresAt
      ? new Date(item.expiresAt * 1000 - 1000).toLocaleDateString()
      : t('apiKeys.neverExpires');
    return '<tr data-apikey-id="' + id + '">' +
      '<td><span class="api-key-name">' + name + '</span></td>' +
      '<td class="api-key-cell">' +
        '<div class="api-key-card-key-row">' +
          '<span class="api-key-value" data-apikey-value="' + id + '" title="' + escapeAttr(displayKey) + '">' + keyText + '</span>' +
          '<button class="btn btn-icon btn-sm btn-ghost api-key-icon-btn" type="button" data-apikey-action="toggleReveal" data-id="' + id + '" title="' + escapeAttr(eyeLabel) + '" aria-label="' + escapeAttr(eyeLabel) + '" aria-pressed="' + (revealed ? 'true' : 'false') + '">' +
            '<i class="' + eyeIcon + '" aria-hidden="true"></i></button>' +
          '<button class="btn btn-icon btn-sm btn-ghost api-key-icon-btn" type="button" data-apikey-action="copy" data-id="' + id + '" title="' + escapeAttr(t('apiKeys.copyKey')) + '" aria-label="' + escapeAttr(t('apiKeys.copyKey')) + '">' +
            '<i class="fa-regular fa-copy" aria-hidden="true"></i></button>' +
        '</div>' +
      '</td>' +
      '<td><div class="api-keys-status">' + badges.join('') + '</div></td>' +
      '<td>' + usageCell(item.tokensUsed || 0, item.tokenLimit || 0) + '</td>' +
      '<td>' + usageCell(item.creditsUsed || 0, item.creditLimit || 0) + '</td>' +
      '<td class="num">' + escapeHtml(formatNumber(item.requestsCount || 0)) + '</td>' +
      '<td class="num">' +
        '<button type="button" class="btn btn-ghost btn-sm api-keys-ip-badge' + ((item.uniqueIps || 0) >= 2 ? ' is-multi' : '') + '" data-apikey-action="viewIPs" data-id="' + id + '" title="' + escapeAttr(t('apiKeys.viewIPs')) + '">' +
          escapeHtml(formatNumber(item.uniqueIps || 0)) +
        '</button>' +
      '</td>' +
      '<td class="num" title="' + escapeAttr(t('apiKeys.rpmHint')) + '">' +
        '<span class="api-keys-rpm-badge' + ((item.rpm || 0) > 0 ? ' is-active' : '') + '">' +
          escapeHtml(formatNumber(item.rpm || 0)) +
        '</span>' +
      '</td>' +
      '<td>' + escapeHtml(expiryText) + '</td>' +
      '<td class="api-keys-actions-cell">' +
        '<div class="api-keys-row-actions">' +
          '<label class="switch" title="' + escapeAttr(item.enabled ? t('accounts.disable') : t('accounts.enable')) + '">' +
            '<input type="checkbox" data-apikey-action="toggle" data-id="' + id + '"' + (item.enabled ? ' checked' : '') + ' />' +
            '<span class="slider"></span>' +
          '</label>' +
          '<button class="btn btn-outline btn-sm" type="button" data-apikey-action="edit" data-id="' + id + '">' + escapeHtml(t('apiKeys.actionEdit')) + '</button>' +
          '<button class="btn btn-outline btn-sm" type="button" data-apikey-action="reset" data-id="' + id + '">' + escapeHtml(t('apiKeys.actionReset')) + '</button>' +
          '<button class="btn btn-danger btn-sm" type="button" data-apikey-action="delete" data-id="' + id + '">' + escapeHtml(t('apiKeys.actionDelete')) + '</button>' +
        '</div>' +
      '</td>' +
    '</tr>';
  }).join('');

  const from = slice.total ? (slice.start + 1) : 0;
  const to = Math.min(slice.start + slice.size, slice.total);
  const pager = '<div class="api-keys-pagination">' +
    '<span>' + escapeHtml(t('apiKeys.showing', String(from), String(to), String(slice.total))) + '</span>' +
    '<div class="api-keys-pagination-controls">' +
      '<button class="btn btn-outline btn-sm" type="button" data-apikey-page="prev"' + (slice.page <= 1 ? ' disabled' : '') + '>' +
        escapeHtml(t('apiKeys.prev')) +
      '</button>' +
      '<span>' + escapeHtml(t('apiKeys.pageOf', String(slice.page), String(slice.pages))) + '</span>' +
      '<button class="btn btn-outline btn-sm" type="button" data-apikey-page="next"' + (slice.page >= slice.pages ? ' disabled' : '') + '>' +
        escapeHtml(t('apiKeys.next')) +
      '</button>' +
    '</div>' +
  '</div>';

  list.innerHTML =
    '<div class="api-keys-table-wrap">' +
      '<table class="api-keys-table">' +
        '<thead><tr>' +
          '<th>' + escapeHtml(t('apiKeys.colName')) + '</th>' +
          '<th>' + escapeHtml(t('apiKeys.colKey')) + '</th>' +
          '<th>' + escapeHtml(t('apiKeys.colStatus')) + '</th>' +
          '<th>' + escapeHtml(t('apiKeys.tokens')) + '</th>' +
          '<th>' + escapeHtml(t('apiKeys.credits')) + '</th>' +
          '<th class="num">' + escapeHtml(t('apiKeys.requests')) + '</th>' +
          '<th class="num">' + escapeHtml(t('apiKeys.colIPs')) + '</th>' +
          '<th class="num" title="' + escapeAttr(t('apiKeys.rpmHint')) + '">' + escapeHtml(t('apiKeys.colRpm')) + '</th>' +
          '<th>' + escapeHtml(t('apiKeys.expiry')) + '</th>' +
          '<th>' + escapeHtml(t('apiKeys.colActions')) + '</th>' +
        '</tr></thead>' +
        '<tbody>' + rows + '</tbody>' +
      '</table>' +
    '</div>' +
    pager;
}

// Expiry uses browser-local time. The date picker holds the last valid day;
// the stored expiresAt is 00:00 local of the following day, so the key works
// through the end of the selected day and dies at midnight after it.
export function dateInputToExpiresAt(dateStr) {
  if (!dateStr) return 0;
  const parts = dateStr.split('-');
  if (parts.length !== 3) return 0;
  const y = parseInt(parts[0], 10);
  const m = parseInt(parts[1], 10);
  const d = parseInt(parts[2], 10);
  if (isNaN(y) || isNaN(m) || isNaN(d)) return 0;
  // Local midnight of the day AFTER the selected day.
  const dt = new Date(y, m - 1, d + 1, 0, 0, 0, 0);
  return Math.floor(dt.getTime() / 1000);
}

export function expiresAtToDateInput(expiresAt) {
  if (!expiresAt) return '';
  // Step back 1s so we land on the selected day, then format local YYYY-MM-DD.
  const dt = new Date((expiresAt - 1) * 1000);
  const y = dt.getFullYear();
  const m = String(dt.getMonth() + 1).padStart(2, '0');
  const d = String(dt.getDate()).padStart(2, '0');
  return y + '-' + m + '-' + d;
}

export function openApiKeyModal(entry) {
  state.apiKeyEditingId = entry ? (entry.id || '') : '';
  const titleEl = $('apiKeyModalTitle');
  titleEl.textContent = t(state.apiKeyEditingId ? 'apiKeys.modalTitleEdit' : 'apiKeys.modalTitleCreate');
  $('apiKeyForm_name').value = entry ? (entry.name || '') : '';
  const keyEl = $('apiKeyForm_key');
  if (state.apiKeyEditingId) {
    keyEl.value = entry.keyMasked || '';
    keyEl.readOnly = true;
  } else {
    keyEl.value = '';
    keyEl.readOnly = false;
  }
  $('apiKeyForm_enabled').checked = entry ? !!entry.enabled : true;
  $('apiKeyForm_tokenLimit').value = entry ? String(entry.tokenLimit || 0) : '0';
  $('apiKeyForm_creditLimit').value = entry ? String(entry.creditLimit || 0) : '0';
  $('apiKeyForm_expiryDate').value = entry && entry.expiresAt ? expiresAtToDateInput(entry.expiresAt) : '';
  state.apiKeyModalSubmitting = false;
  $('apiKeyModalSaveBtn').disabled = false;
  openDialog('apiKeyModal');
}

export function closeApiKeyModal() {
  closeDialog('apiKeyModal');
  state.apiKeyEditingId = '';
  state.apiKeyModalSubmitting = false;
  $('apiKeyModalSaveBtn').disabled = false;
}

export async function submitApiKeyModal() {
  if (state.apiKeyModalSubmitting) return;
  state.apiKeyModalSubmitting = true;
  const saveBtn = $('apiKeyModalSaveBtn');
  saveBtn.disabled = true;
  try {
    const name = $('apiKeyForm_name').value.trim();
    const enabled = $('apiKeyForm_enabled').checked;
    const tokenLimit = parseInt($('apiKeyForm_tokenLimit').value, 10);
    const creditLimit = parseFloat($('apiKeyForm_creditLimit').value);
    const payload = {
      name: name,
      enabled: enabled,
      tokenLimit: isNaN(tokenLimit) || tokenLimit < 0 ? 0 : tokenLimit,
      creditLimit: isNaN(creditLimit) || creditLimit < 0 ? 0 : creditLimit,
      expiresAt: dateInputToExpiresAt($('apiKeyForm_expiryDate').value)
    };
    let res, d;
    if (state.apiKeyEditingId) {
      res = await api('/api-keys/' + encodeURIComponent(state.apiKeyEditingId), { method: 'PUT', body: JSON.stringify(payload) });
      d = await res.json().catch(() => ({}));
      if (!res.ok || d.success === false) throw new Error(d.error || t('common.saveFailed'));
      toast(t('apiKeys.updated'), 'success');
      closeApiKeyModal();
      await loadApiKeys();
    } else {
      const keyVal = $('apiKeyForm_key').value.trim();
      if (keyVal) payload.key = keyVal;
      res = await api('/api-keys', { method: 'POST', body: JSON.stringify(payload) });
      d = await res.json().catch(() => ({}));
      if (!res.ok || d.success === false) throw new Error(d.error || t('common.saveFailed'));
      toast(t('apiKeys.created'), 'success');
      closeApiKeyModal();
      await loadApiKeys();
      if (d.key) showNewApiKey(d.key);
    }
  } catch (e) {
    toast((e && e.message) || t('common.saveFailed'), 'error');
    state.apiKeyModalSubmitting = false;
    saveBtn.disabled = false;
  }
}

export async function toggleApiKeyEntry(id, enabled) {
  try {
    const res = await api('/api-keys/' + encodeURIComponent(id), { method: 'PUT', body: JSON.stringify({ enabled }) });
    const d = await res.json().catch(() => ({}));
    if (!res.ok || d.success === false) throw new Error(d.error || t('common.saveFailed'));
    const item = state.apiKeysCache.find(x => x.id === id);
    if (item) item.enabled = enabled;
    renderApiKeys();
    renderOverviewApiKeyStats();
  } catch (e) {
    toast((e && e.message) || t('common.saveFailed'), 'error');
    await loadApiKeys();
  }
}

export async function deleteApiKeyEntry(id, name) {
  const ok = await confirmAction(t('apiKeys.confirmDelete', name || t('apiKeys.unnamed')), {
    title: t('apiKeys.actionDelete'),
    confirmText: t('apiKeys.actionDelete'),
    variant: 'danger'
  });
  if (!ok) return;
  try {
    const res = await api('/api-keys/' + encodeURIComponent(id), { method: 'DELETE' });
    const d = await res.json().catch(() => ({}));
    if (!res.ok || d.success === false) throw new Error(d.error || t('common.failed'));
    toast(t('apiKeys.deleteSuccess'), 'success');
    if (state.apiKeyRevealCache) delete state.apiKeyRevealCache[id];
    if (state.apiKeyRevealed) delete state.apiKeyRevealed[id];
    await loadApiKeys();
  } catch (e) {
    toast((e && e.message) || t('common.failed'), 'error');
  }
}

export async function resetApiKeyUsageEntry(id, name) {
  const ok = await confirmAction(t('apiKeys.confirmReset', name || t('apiKeys.unnamed')), {
    title: t('apiKeys.actionReset'),
    confirmText: t('apiKeys.actionReset')
  });
  if (!ok) return;
  try {
    const res = await api('/api-keys/' + encodeURIComponent(id) + '/reset-usage', { method: 'POST' });
    const d = await res.json().catch(() => ({}));
    if (!res.ok || d.success === false) throw new Error(d.error || t('common.failed'));
    toast(t('apiKeys.usageReset'), 'success');
    await loadApiKeys();
  } catch (e) {
    toast((e && e.message) || t('common.failed'), 'error');
  }
}


export async function fetchApiKeyPlaintext(id) {
  if (!id) throw new Error(t('apiKeys.revealFailed'));
  if (state.apiKeyRevealCache && state.apiKeyRevealCache[id]) {
    return state.apiKeyRevealCache[id];
  }
  const res = await api('/api-keys/' + encodeURIComponent(id) + '/reveal');
  const d = await res.json().catch(() => ({}));
  if (!res.ok || d.success === false || !d.key) {
    throw new Error(d.error || t('apiKeys.revealFailed'));
  }
  if (!state.apiKeyRevealCache) state.apiKeyRevealCache = {};
  state.apiKeyRevealCache[id] = d.key;
  return d.key;
}

export async function toggleApiKeyReveal(id) {
  if (!id) return;
  if (!state.apiKeyRevealed) state.apiKeyRevealed = {};
  // Hide path: always return to masked value without another network call.
  if (state.apiKeyRevealed[id] === true) {
    state.apiKeyRevealed[id] = false;
    renderApiKeys();
    return;
  }
  try {
    await fetchApiKeyPlaintext(id);
    state.apiKeyRevealed[id] = true;
    renderApiKeys();
  } catch (e) {
    state.apiKeyRevealed[id] = false;
    toast((e && e.message) || t('apiKeys.revealFailed'), 'error');
  }
}

export async function copyApiKeyValue(id, btn) {
  if (!id) return;
  try {
    const key = await fetchApiKeyPlaintext(id);
    await copyText(key);
    toast(t('apiKeys.copySuccess'), 'success');
    if (btn) {
      const html = btn.innerHTML;
      btn.innerHTML = '<i class="fa-solid fa-check" aria-hidden="true"></i>';
      setTimeout(() => { btn.innerHTML = html; }, 800);
    }
  } catch (e) {
    toast((e && e.message) || t('apiKeys.revealFailed'), 'error');
  }
}

export function showNewApiKey(plaintext) {
  $('apiKeyShowValue').value = plaintext || '';
  openDialog('apiKeyShowModal');
  setTimeout(() => {
    const el = $('apiKeyShowValue');
    if (el) { try { el.select(); } catch (_) { } }
  }, 0);
}

export function closeShowApiKeyModal() {
  closeDialog('apiKeyShowModal');
  $('apiKeyShowValue').value = '';
}

export async function copyNewApiKey() {
  const val = $('apiKeyShowValue').value;
  if (!val) return;
  try {
    await copyText(val);
    toast(t('apiKeys.copySuccess'), 'success');
  } catch (e) {
    toast(t('common.failed'), 'error');
  }
}

export function bindApiKeyEvents() {
  const list = $('apiKeysList');
  if (list) {
    list.addEventListener('click', e => {
      const pageBtn = e.target.closest('[data-apikey-page]');
      if (pageBtn) {
        e.preventDefault();
        const dir = pageBtn.dataset.apikeyPage;
        if (dir === 'prev') setApiKeysPage((state.apiKeysPage || 1) - 1);
        else if (dir === 'next') setApiKeysPage((state.apiKeysPage || 1) + 1);
        return;
      }
      const btn = e.target.closest('[data-apikey-action]');
      if (!btn) return;
      e.preventDefault();
      e.stopPropagation();
      const action = btn.dataset.apikeyAction;
      const id = btn.dataset.id;
      if (!id) return;
      const entry = state.apiKeysCache.find(x => x.id === id);
      const name = entry ? entry.name : '';
      if (action === 'edit') openApiKeyModal(entry);
      else if (action === 'delete') deleteApiKeyEntry(id, name);
      else if (action === 'reset') resetApiKeyUsageEntry(id, name);
      else if (action === 'toggleReveal') toggleApiKeyReveal(id);
      else if (action === 'copy') copyApiKeyValue(id, btn);
      else if (action === 'viewIPs') openApiKeyIPsModal(id, name);
    });
    list.addEventListener('change', e => {
      const cb = e.target.closest('input[data-apikey-action="toggle"]');
      if (!cb) return;
      const id = cb.dataset.id;
      if (!id) return;
      toggleApiKeyEntry(id, cb.checked);
    });
  }
  const search = $('apiKeysSearch');
  if (search) {
    search.addEventListener('input', onApiKeysFilterChange);
  }
  const status = $('apiKeysStatusFilter');
  if (status) {
    status.addEventListener('change', onApiKeysFilterChange);
  }
  const pageSize = $('apiKeysPageSize');
  if (pageSize) {
    pageSize.addEventListener('change', onApiKeysFilterChange);
  }
  const addBtn = $('addApiKeyBtn');
  if (addBtn) addBtn.addEventListener('click', () => openApiKeyModal(null));
  const saveBtn = $('apiKeyModalSaveBtn');
  if (saveBtn) saveBtn.addEventListener('click', submitApiKeyModal);
  const cancelBtn = $('apiKeyModalCancelBtn');
  if (cancelBtn) cancelBtn.addEventListener('click', closeApiKeyModal);
  const closeBtn = $('apiKeyModalClose');
  if (closeBtn) closeBtn.addEventListener('click', closeApiKeyModal);
  const showCloseBtn = $('apiKeyShowCloseBtn');
  if (showCloseBtn) showCloseBtn.addEventListener('click', closeShowApiKeyModal);
  const showCloseX = $('apiKeyShowClose');
  if (showCloseX) showCloseX.addEventListener('click', closeShowApiKeyModal);
  const copyBtn = $('apiKeyShowCopyBtn');
  if (copyBtn) copyBtn.addEventListener('click', copyNewApiKey);
  bindDialogBackdropClose('apiKeyModal', closeApiKeyModal);
  bindDialogBackdropClose('apiKeyShowModal', closeShowApiKeyModal);

  const saveSec = $('saveSecurityBtn');
  if (saveSec) saveSec.addEventListener('click', saveSecuritySettings);
  const blockBtn = $('securityBlockBtn');
  if (blockBtn) blockBtn.addEventListener('click', async () => {
    const ipEl = $('securityBlockIP');
    const reasonEl = $('securityBlockReason');
    const ok = await blockIPAddress(ipEl ? ipEl.value : '', reasonEl ? reasonEl.value : '');
    if (ok && ipEl) ipEl.value = '';
    if (ok && reasonEl) reasonEl.value = '';
  });
  const blockedList = $('blockedIPsList');
  if (blockedList) blockedList.addEventListener('click', e => {
    const btn = e.target.closest('[data-unblock-ip]');
    if (!btn) return;
    unblockIPAddress(btn.dataset.unblockIp);
  });
  const ipsBody = $('apiKeyIPsBody');
  if (ipsBody) ipsBody.addEventListener('click', async e => {
    const btn = e.target.closest('[data-ban-ip]');
    if (!btn) return;
    const ip = btn.dataset.banIp;
    const ok = await confirmAction(t('apiKeys.banIPConfirm', ip), {
      title: t('apiKeys.banIP'),
      confirmText: t('apiKeys.banIP'),
      variant: 'danger'
    });
    if (!ok) return;
    await blockIPAddress(ip, 'admin');
  });
  const ipsClose = $('apiKeyIPsCloseBtn');
  if (ipsClose) ipsClose.addEventListener('click', closeApiKeyIPsModal);
  const ipsCloseX = $('apiKeyIPsModalClose');
  if (ipsCloseX) ipsCloseX.addEventListener('click', closeApiKeyIPsModal);
  bindDialogBackdropClose('apiKeyIPsModal', closeApiKeyIPsModal);
}


// Overview API key analytics (client-side aggregate from apiKeysCache)

export function aggregateApiKeyStats(keys, nowMs = Date.now()) {
  const list = Array.isArray(keys) ? keys : [];
  const nowSec = Math.floor(nowMs / 1000);
  const dayMs = 86400000;
  const nowDate = new Date(nowMs);
  const todayLocal0 = new Date(nowDate.getFullYear(), nowDate.getMonth(), nowDate.getDate()).getTime();
  const windowStart = todayLocal0 - 13 * dayMs;
  const created7dCut = nowSec - 7 * 86400;
  const expiringCut = nowSec + 7 * 86400;

  let total = 0;
  let active = 0;
  let disabled = 0;
  let expired = 0;
  let expiringSoon = 0;
  let created7d = 0;
  let sumRequests = 0;
  let sumTokens = 0;
  let sumCredits = 0;
  let neverUsed = 0;
  const createdBuckets = new Array(14).fill(0);
  const topCandidates = [];

  for (let i = 0; i < list.length; i++) {
    const item = list[i];
    if (!item) continue;
    total++;

    const isExpired = !!item.expired || (item.expiresAt > 0 && nowSec >= item.expiresAt);
    if (isExpired) expired++;
    else if (!item.enabled) disabled++;
    else active++;

    if (!isExpired && item.expiresAt > 0 && item.expiresAt <= expiringCut && item.expiresAt > nowSec) {
      expiringSoon++;
    }

    const createdAt = item.createdAt || 0;
    if (createdAt >= created7dCut) created7d++;
    if (createdAt > 0) {
      const createdMs = createdAt * 1000;
      if (createdMs >= windowStart && createdMs < todayLocal0 + dayMs) {
        const idx = Math.floor((createdMs - windowStart) / dayMs);
        if (idx >= 0 && idx < 14) createdBuckets[idx]++;
      }
    }

    const req = item.requestsCount || 0;
    const tok = item.tokensUsed || 0;
    const cred = item.creditsUsed || 0;
    sumRequests += req;
    sumTokens += tok;
    sumCredits += cred;
    if (!item.lastUsedAt && !req) neverUsed++;

    const tokenLimit = item.tokenLimit || 0;
    const creditLimit = item.creditLimit || 0;
    const tokenRatio = tokenLimit > 0 ? tok / tokenLimit : 0;
    const creditRatio = creditLimit > 0 ? cred / creditLimit : 0;
    const pressure = Math.max(tokenRatio, creditRatio);
    topCandidates.push({
      id: item.id || '',
      name: item.name || '',
      enabled: !!item.enabled,
      expired: isExpired,
      requestsCount: req,
      tokensUsed: tok,
      creditsUsed: cred,
      tokenLimit: tokenLimit,
      creditLimit: creditLimit,
      pressure: pressure
    });
  }

  topCandidates.sort((a, b) => {
    if (b.requestsCount !== a.requestsCount) return b.requestsCount - a.requestsCount;
    if (b.tokensUsed !== a.tokensUsed) return b.tokensUsed - a.tokensUsed;
    return (b.pressure || 0) - (a.pressure || 0);
  });
  const top = topCandidates.filter(x => x.requestsCount > 0).slice(0, 5);

  const fingerprint = [
    total, active, disabled, expired, expiringSoon, created7d,
    sumRequests, sumTokens, Math.round(sumCredits * 1000),
    createdBuckets.join(','),
    top.map(x => x.id + ':' + x.requestsCount + ':' + x.tokensUsed).join(',')
  ].join('|');

  return {
    total, active, disabled, expired, expiringSoon, created7d,
    createdBuckets, sumRequests, sumTokens, sumCredits, neverUsed, top,
    fingerprint, windowStart, todayLocal0
  };
}

function setText(id, value) {
  const el = $(id);
  if (el) el.textContent = value;
}

// --- Overview charts (Chart.js) ---------------------------------------------
// Admin UI is vanilla JS (no React), so we use Chart.js instead of Recharts.
// Chart.js is loaded from /admin/vendor/chart.js/chart.umd.min.js as window.Chart.

let overviewStatusChart = null;
let overviewCreatedChart = null;

function cssVar(name, fallback) {
  try {
    const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
    return v || fallback;
  } catch (_) {
    return fallback;
  }
}

function overviewChartColors() {
  return {
    success: cssVar('--success', '#2dd4bf'),
    destructive: cssVar('--destructive', '#ff5b5b'),
    mutedFg: cssVar('--muted-foreground', '#a4a4a4'),
    muted: cssVar('--muted', '#1d1d1d'),
    foreground: cssVar('--foreground', '#ffffff'),
    border: cssVar('--border', '#242424'),
    card: cssVar('--card', '#090909'),
    primary: cssVar('--primary', '#ffffff'),
  };
}

function destroyOverviewCharts() {
  if (overviewStatusChart) {
    try { overviewStatusChart.destroy(); } catch (_) {}
    overviewStatusChart = null;
  }
  if (overviewCreatedChart) {
    try { overviewCreatedChart.destroy(); } catch (_) {}
    overviewCreatedChart = null;
  }
}

function hasChartJs() {
  return typeof window !== 'undefined' && typeof window.Chart === 'function';
}

function renderOverviewStatusDist(agg) {
  const canvas = $('apiKeyStatusChart');
  const legend = $('apiKeyStatusLegend');
  // Legacy DOM (pre-Chart.js) still supported for safety.
  const bar = $('apiKeyStatusBar');
  if (!legend) return;

  const total = Math.max(agg.total, 0);
  const aria = t('stats.apiKeysStatusAria', String(agg.active), String(agg.disabled), String(agg.expired));

  const segs = [
    { key: 'active', count: agg.active, sw: 'overview-status-swatch--active', label: t('apiKeys.statusEnabled'), colorKey: 'success' },
    { key: 'disabled', count: agg.disabled, sw: 'overview-status-swatch--disabled', label: t('apiKeys.disabled'), colorKey: 'mutedFg' },
    { key: 'expired', count: agg.expired, sw: 'overview-status-swatch--expired', label: t('apiKeys.expired'), colorKey: 'destructive' }
  ];

  if (!total) {
    if (overviewStatusChart) {
      try { overviewStatusChart.destroy(); } catch (_) {}
      overviewStatusChart = null;
    }
    if (canvas) {
      canvas.setAttribute('aria-label', aria);
      const ctx = canvas.getContext && canvas.getContext('2d');
      if (ctx) ctx.clearRect(0, 0, canvas.width, canvas.height);
    }
    if (bar) bar.innerHTML = '';
    legend.innerHTML = '<li class="muted-text">' + escapeHtml(t('stats.apiKeysNoKeys')) + '</li>';
    return;
  }

  legend.innerHTML = segs.map(s => {
    const pct = total ? Math.round(100 * s.count / total) : 0;
    return '<li><span class="overview-status-swatch ' + s.sw + '" aria-hidden="true"></span>' +
      escapeHtml(s.label) + ' <strong>' + escapeHtml(String(s.count)) + '</strong>' +
      ' <span class="muted-text">(' + pct + '%)</span></li>';
  }).join('');

  const colors = overviewChartColors();
  // Doughnut only needs non-zero slices; legend still shows full breakdown.
  const plot = segs.filter(s => s.count > 0);
  const labels = plot.map(s => s.label);
  const data = plot.map(s => s.count);
  const bg = plot.map(s => {
    if (s.colorKey === 'success') return colors.success;
    if (s.colorKey === 'destructive') return colors.destructive;
    return colors.mutedFg;
  });

  if (hasChartJs() && canvas) {
    canvas.setAttribute('aria-label', aria);
    if (overviewStatusChart) {
      overviewStatusChart.data.labels = labels;
      overviewStatusChart.data.datasets[0].data = data;
      overviewStatusChart.data.datasets[0].backgroundColor = bg;
      overviewStatusChart.data.datasets[0].borderColor = colors.card;
      overviewStatusChart.options.plugins.tooltip.callbacks = {
        label(ctx) {
          const v = ctx.raw || 0;
          const pct = total ? Math.round(100 * v / total) : 0;
          return ' ' + ctx.label + ': ' + v + ' (' + pct + '%)';
        }
      };
      overviewStatusChart.update('active');
    } else {
      overviewStatusChart = new window.Chart(canvas, {
        type: 'doughnut',
        data: {
          labels,
          datasets: [{
            data,
            backgroundColor: bg,
            borderColor: colors.card,
            borderWidth: 2,
            hoverOffset: 6,
            borderRadius: 4,
            spacing: 2,
          }]
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          cutout: '72%',
          animation: { duration: 450, easing: 'easeOutQuart' },
          plugins: {
            legend: { display: false },
            tooltip: {
              backgroundColor: colors.card,
              titleColor: colors.foreground,
              bodyColor: colors.mutedFg,
              borderColor: colors.border,
              borderWidth: 1,
              padding: 10,
              displayColors: true,
              callbacks: {
                label(ctx) {
                  const v = ctx.raw || 0;
                  const pct = total ? Math.round(100 * v / total) : 0;
                  return ' ' + ctx.label + ': ' + v + ' (' + pct + '%)';
                }
              }
            }
          }
        }
      });
    }
    return;
  }

  // Fallback: CSS segmented bar (no Chart.js)
  if (bar) {
    bar.setAttribute('aria-label', aria);
    bar.innerHTML = segs.map(s => {
      if (!s.count) return '';
      const pct = (100 * s.count / total).toFixed(2);
      const cls = s.key === 'active' ? 'overview-status-seg--active'
        : s.key === 'disabled' ? 'overview-status-seg--disabled'
        : 'overview-status-seg--expired';
      return '<span class="overview-status-seg ' + cls + '" style="width:' + pct + '%" title="' +
        escapeAttr(s.label + ': ' + s.count) + '"></span>';
    }).join('');
  }
}

function renderOverviewCreatedChart(agg) {
  const canvas = $('apiKeyCreatedChart');
  const summary = $('apiKeyCreatedSummary');
  if (!canvas && !summary) return;

  const buckets = agg.createdBuckets || [];
  const periodTotal = buckets.reduce((a, b) => a + b, 0);
  const dayMs = 86400000;
  const start = agg.windowStart || (Date.now() - 13 * dayMs);
  const labels = [];
  for (let i = 0; i < 14; i++) {
    const d = new Date(start + i * dayMs);
    labels.push(String(d.getMonth() + 1).padStart(2, '0') + '/' + String(d.getDate()).padStart(2, '0'));
  }
  if (summary) summary.textContent = t('stats.apiKeysCreatedCount', String(periodTotal));

  const aria = t('stats.apiKeysCreatedAria');
  const colors = overviewChartColors();

  if (hasChartJs() && canvas && canvas.tagName === 'CANVAS') {
    canvas.setAttribute('aria-label', aria);
    const data = buckets.map(v => v || 0);
    if (overviewCreatedChart) {
      overviewCreatedChart.data.labels = labels;
      overviewCreatedChart.data.datasets[0].data = data;
      overviewCreatedChart.data.datasets[0].backgroundColor = colors.success;
      overviewCreatedChart.data.datasets[0].hoverBackgroundColor = colors.foreground;
      overviewCreatedChart.options.scales.x.ticks.color = colors.mutedFg;
      overviewCreatedChart.options.scales.y.ticks.color = colors.mutedFg;
      overviewCreatedChart.options.scales.y.grid.color = colors.border;
      overviewCreatedChart.update('active');
    } else {
      overviewCreatedChart = new window.Chart(canvas, {
        type: 'bar',
        data: {
          labels,
          datasets: [{
            data,
            backgroundColor: colors.success,
            hoverBackgroundColor: colors.foreground,
            borderRadius: 4,
            borderSkipped: false,
            maxBarThickness: 14,
          }]
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          animation: { duration: 450, easing: 'easeOutQuart' },
          interaction: { mode: 'index', intersect: false },
          plugins: {
            legend: { display: false },
            tooltip: {
              backgroundColor: colors.card,
              titleColor: colors.foreground,
              bodyColor: colors.mutedFg,
              borderColor: colors.border,
              borderWidth: 1,
              padding: 10,
              displayColors: false,
              callbacks: {
                title(items) { return items[0] ? items[0].label : ''; },
                label(ctx) { return ' ' + (ctx.raw || 0); }
              }
            }
          },
          scales: {
            x: {
              grid: { display: false },
              border: { display: false },
              ticks: {
                color: colors.mutedFg,
                font: { size: 10 },
                maxRotation: 0,
                autoSkip: true,
                maxTicksLimit: 5,
              }
            },
            y: {
              beginAtZero: true,
              border: { display: false },
              grid: {
                color: colors.border,
                drawTicks: false,
              },
              ticks: {
                color: colors.mutedFg,
                font: { size: 10 },
                precision: 0,
                maxTicksLimit: 4,
              }
            }
          }
        }
      });
    }
    return;
  }

  // Fallback: pure CSS bars if canvas/Chart.js unavailable
  const chart = canvas || $('apiKeyCreatedChart');
  if (!chart || chart.tagName === 'CANVAS') return;
  chart.setAttribute('aria-label', aria);
  const maxH = Math.max(1, ...buckets);
  let html = '';
  for (let i = 0; i < 14; i++) {
    const count = buckets[i] || 0;
    const h = Math.max(2, Math.round(100 * count / maxH));
    const label = labels[i];
    const showLabel = i === 0 || i === 6 || i === 13;
    html += '<div class="overview-created-col" title="' + escapeAttr(label + ': ' + count) + '">' +
      '<div class="overview-created-bar' + (count ? '' : ' is-zero') + '" style="height:' + (count ? h : 2) + '%"></div>' +
      '<span class="overview-created-label">' + (showLabel ? escapeHtml(label) : '') + '</span>' +
      '</div>';
  }
  chart.innerHTML = html;
}

function renderOverviewTopTable(agg) {
  const host = $('apiKeyTopTable');
  if (!host) return;

  if (!agg.total) {
    host.innerHTML = '<div class="empty-state">' + escapeHtml(t('stats.apiKeysNoKeys')) + '</div>';
    return;
  }
  if (!agg.top || !agg.top.length) {
    host.innerHTML = '<div class="empty-state">' + escapeHtml(t('stats.apiKeysTopEmpty')) + '</div>';
    return;
  }

  const rows = agg.top.map(item => {
    const name = item.name
      ? escapeHtml(item.name)
      : '<span class="muted-text">' + escapeHtml(t('apiKeys.unnamed')) + '</span>';
    let statusBadge;
    if (item.expired) statusBadge = '<span class="badge badge-error">' + escapeHtml(t('apiKeys.expired')) + '</span>';
    else if (!item.enabled) statusBadge = '<span class="badge badge-error">' + escapeHtml(t('apiKeys.disabled')) + '</span>';
    else statusBadge = '<span class="badge badge-success">' + escapeHtml(t('apiKeys.statusEnabled')) + '</span>';

    const pressureCell = (item.tokenLimit > 0 || item.creditLimit > 0)
      ? (
          (item.tokenLimit > 0 ? usageBar(item.tokensUsed, item.tokenLimit) : '') +
          (item.creditLimit > 0 ? usageBar(item.creditsUsed, item.creditLimit) : '')
        )
      : '<span class="muted-text">' + escapeHtml(t('apiKeys.unlimited')) + '</span>';

    return '<tr>' +
      '<td>' + name + '</td>' +
      '<td>' + statusBadge + '</td>' +
      '<td class="num">' + escapeHtml(formatNumber(item.requestsCount || 0)) + '</td>' +
      '<td class="num">' + escapeHtml(formatNumber(item.tokensUsed || 0)) + '</td>' +
      '<td class="num">' + escapeHtml(formatNumber(item.creditsUsed || 0)) + '</td>' +
      '<td class="api-keys-usage-cell">' + pressureCell + '</td>' +
      '</tr>';
  }).join('');

  host.innerHTML =
    '<div class="usage-table-wrap">' +
      '<table class="usage-table">' +
        '<thead><tr>' +
          '<th>' + escapeHtml(t('apiKeys.colName')) + '</th>' +
          '<th>' + escapeHtml(t('apiKeys.colStatus')) + '</th>' +
          '<th class="num">' + escapeHtml(t('apiKeys.requests')) + '</th>' +
          '<th class="num">' + escapeHtml(t('apiKeys.tokens')) + '</th>' +
          '<th class="num">' + escapeHtml(t('apiKeys.credits')) + '</th>' +
          '<th>' + escapeHtml(t('stats.apiKeysColPressure')) + '</th>' +
        '</tr></thead>' +
        '<tbody>' + rows + '</tbody>' +
      '</table>' +
    '</div>';
}

export function renderOverviewApiKeyStats(force) {
  const root = $('overviewApiKeys') || $('viewOverview');
  if (!root) return;

  const agg = aggregateApiKeyStats(state.apiKeysCache || []);
  const langTag = state.currentLang || '';
  const themeTag = document.documentElement.classList.contains('dark') ? 'dark' : 'light';
  const chartTag = hasChartJs() ? 'chartjs' : 'css';
  const fp = langTag + '|' + themeTag + '|' + chartTag + '|' + agg.fingerprint;
  if (!force && state.overviewApiKeyStatsFp === fp) {
    // Still refresh KPI text cheaply in case format locale-independent numbers only —
    // skip chart/table rebuild.
    return;
  }
  state.overviewApiKeyStatsFp = fp;

  setText('statApiKeysTotal', formatNumber(agg.total));
  setText('statApiKeysActive', formatNumber(agg.active));
  setText('statApiKeysExpired', formatNumber(agg.expired));
  setText('statApiKeysRequests', formatNumber(agg.sumRequests));
  setText('statApiKeysTokens', formatNumber(agg.sumTokens));

  const createdLabel = $('statApiKeysCreated7dLabel');
  if (createdLabel) createdLabel.textContent = t('stats.apiKeysCreated7d', formatNumber(agg.created7d));
  const disabledLabel = $('statApiKeysDisabledLabel');
  if (disabledLabel) disabledLabel.textContent = t('stats.apiKeysDisabled', formatNumber(agg.disabled));
  const expiringLabel = $('statApiKeysExpiringSoonLabel');
  if (expiringLabel) expiringLabel.textContent = t('stats.apiKeysExpiringSoon', formatNumber(agg.expiringSoon));

  renderOverviewStatusDist(agg);
  renderOverviewCreatedChart(agg);
  renderOverviewTopTable(agg);
}

// Recolor Chart.js canvases when theme tokens change.
if (typeof window !== 'undefined' && !window.__kiroOverviewThemeBound) {
  window.__kiroOverviewThemeBound = true;
  window.addEventListener('kiro:themechange', () => {
    try { renderOverviewApiKeyStats(true); } catch (_) {}
  });
}



// --- Access security (blocked IPs + trust proxy) ---

export async function loadSecuritySettings() {
  try {
    const res = await api('/security/settings');
    if (res.ok) {
      const d = await res.json();
      const el = $('securityTrustProxy');
      if (el) el.checked = !!d.trustProxyHeaders;
    }
  } catch (e) { /* ignore */ }
  await loadBlockedIPs();
}

export async function saveSecuritySettings() {
  try {
    const el = $('securityTrustProxy');
    const res = await api('/security/settings', {
      method: 'POST',
      body: JSON.stringify({ trustProxyHeaders: !!(el && el.checked) })
    });
    const d = await res.json().catch(() => ({}));
    if (!res.ok || d.success === false) throw new Error(d.error || t('common.saveFailed'));
    toast(t('security.saved'), 'success');
  } catch (e) {
    toastError((e && e.message) || t('common.saveFailed'));
  }
}

export async function loadBlockedIPs() {
  const host = $('blockedIPsList');
  if (!host) return;
  try {
    const res = await api('/security/blocked-ips');
    if (!res.ok) throw new Error('http');
    const d = await res.json();
    const list = Array.isArray(d.blockedIPs) ? d.blockedIPs : [];
    if (!list.length) {
      host.innerHTML = '<div class="empty-state">' + escapeHtml(t('security.blockedEmpty')) + '</div>';
      return;
    }
    host.innerHTML = list.map(item => {
      const ip = item.ip || item.IP || item;
      const ipStr = typeof ip === 'string' ? ip : String(ip || '');
      const reason = (item && item.reason) || '';
      return '<div class="blocked-ip-row" data-ip="' + escapeAttr(ipStr) + '">' +
        '<div class="blocked-ip-meta">' +
          '<div class="blocked-ip-addr">' + escapeHtml(ipStr) + '</div>' +
          (reason ? '<div class="blocked-ip-reason">' + escapeHtml(reason) + '</div>' : '') +
        '</div>' +
        '<button type="button" class="btn btn-outline btn-sm" data-unblock-ip="' + escapeAttr(ipStr) + '">' +
          escapeHtml(t('security.unblock')) +
        '</button>' +
      '</div>';
    }).join('');
  } catch (e) {
    host.innerHTML = '<div class="empty-state">' + escapeHtml(t('common.failed')) + '</div>';
  }
}

export async function blockIPAddress(ip, reason) {
  ip = (ip || '').trim();
  if (!ip) {
    toastError(t('security.invalidIP'));
    return false;
  }
  try {
    const res = await api('/security/blocked-ips', {
      method: 'POST',
      body: JSON.stringify({ ip: ip, reason: (reason || '').trim() })
    });
    const d = await res.json().catch(() => ({}));
    if (!res.ok || d.success === false) throw new Error(d.error || t('common.failed'));
    toast(t('security.blocked'), 'success');
    await loadBlockedIPs();
    return true;
  } catch (e) {
    toastError((e && e.message) || t('common.failed'));
    return false;
  }
}

export async function unblockIPAddress(ip) {
  ip = (ip || '').trim();
  if (!ip) return;
  try {
    const res = await api('/security/blocked-ips/unblock', {
      method: 'POST',
      body: JSON.stringify({ ip: ip })
    });
    const d = await res.json().catch(() => ({}));
    if (!res.ok || d.success === false) throw new Error(d.error || t('common.failed'));
    toast(t('security.unblocked'), 'success');
    await loadBlockedIPs();
  } catch (e) {
    toastError((e && e.message) || t('common.failed'));
  }
}

export async function openApiKeyIPsModal(id, name) {
  const title = $('apiKeyIPsModalTitle');
  if (title) {
    title.textContent = t('apiKeys.ipsTitle') + (name ? ': ' + name : '');
  }
  const body = $('apiKeyIPsBody');
  if (body) body.innerHTML = '<div class="empty-state">' + escapeHtml(t('api.loading') || '...') + '</div>';
  openDialog('apiKeyIPsModal');
  try {
    const res = await api('/api-keys/' + encodeURIComponent(id) + '/ips');
    const d = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(d.error || t('common.failed'));
    const ips = Array.isArray(d.ips) ? d.ips : [];
    if (!ips.length) {
      body.innerHTML = '<div class="empty-state">' + escapeHtml(t('apiKeys.ipsEmpty')) + '</div>';
      return;
    }
    const keyRpm = d.rpm || 0;
    const fmtTs = (sec) => {
      if (!sec) return '-';
      try {
        return new Date(sec * 1000).toLocaleString(undefined, {
          year: 'numeric', month: '2-digit', day: '2-digit',
          hour: '2-digit', minute: '2-digit', second: '2-digit'
        });
      } catch (_) {
        return new Date(sec * 1000).toLocaleString();
      }
    };
    const rows = ips.map(item => {
      const ip = item.ip || '';
      const last = fmtTs(item.lastSeen);
      const first = fmtTs(item.firstSeen);
      return '<tr>' +
        '<td class="api-key-ips-ip">' + escapeHtml(ip) + '</td>' +
        '<td class="num">' + escapeHtml(formatNumber(item.requests || 0)) + '</td>' +
        '<td class="num" title="' + escapeAttr(t('apiKeys.rpmHint')) + '">' + escapeHtml(formatNumber(item.rpm || 0)) + '</td>' +
        '<td class="api-key-ips-time">' + escapeHtml(first) + '</td>' +
        '<td class="api-key-ips-time">' + escapeHtml(last) + '</td>' +
        '<td class="api-key-ips-actions"><button type="button" class="btn btn-danger btn-sm" data-ban-ip="' + escapeAttr(ip) + '">' +
          escapeHtml(t('apiKeys.banIP')) + '</button></td>' +
      '</tr>';
    }).join('');
    body.innerHTML =
      '<div class="api-key-ips-summary">' +
        '<span>' + escapeHtml(t('apiKeys.rpm')) + ': <strong>' + escapeHtml(formatNumber(keyRpm)) + '</strong></span>' +
        '<span class="api-key-ips-summary-hint">' + escapeHtml(t('apiKeys.rpmHint')) + '</span>' +
        '<span class="api-key-ips-summary-hint">· ' + escapeHtml(t('apiKeys.multiIP', String(ips.length))) + '</span>' +
      '</div>' +
      '<div class="api-key-ips-table-wrap"><table class="api-key-ips-table">' +
        '<thead><tr>' +
          '<th>IP</th>' +
          '<th class="num">' + escapeHtml(t('apiKeys.ipsRequests')) + '</th>' +
          '<th class="num" title="' + escapeAttr(t('apiKeys.rpmHint')) + '">' + escapeHtml(t('apiKeys.colRpm')) + '</th>' +
          '<th>' + escapeHtml(t('apiKeys.ipsFirstSeen')) + '</th>' +
          '<th>' + escapeHtml(t('apiKeys.ipsLastSeen')) + '</th>' +
          '<th class="api-key-ips-actions"></th>' +
        '</tr></thead><tbody>' + rows + '</tbody></table></div>';
  } catch (e) {
    if (body) body.innerHTML = '<div class="empty-state">' + escapeHtml((e && e.message) || t('common.failed')) + '</div>';
  }
}

export function closeApiKeyIPsModal() {
  closeDialog('apiKeyIPsModal');
}

// Prompt filter rules
export async function loadPromptFilter() {
  const res = await api('/prompt-filter');
  const d = await res.json();
  $('filterClaudeCode').checked = !!d.filterClaudeCode;
  $('filterEnvNoise').checked = !!d.filterEnvNoise;
  $('filterStripBoundaries').checked = !!d.filterStripBoundaries;
  state.promptRules = d.rules || [];
  renderPromptRules();
}
export async function savePromptFilter() {
  const res = await api('/prompt-filter', {
    method: 'POST', body: JSON.stringify({
      filterClaudeCode: $('filterClaudeCode').checked,
      filterEnvNoise: $('filterEnvNoise').checked,
      filterStripBoundaries: $('filterStripBoundaries').checked,
      rules: state.promptRules
    })
  });
  const d = await res.json();
  if (d.success) toast(t('settings.promptFilterSaved'), 'success');
  else toast(t('common.saveFailed') + ': ' + (d.error || ''), 'error');
}
export function renderPromptRules() {
  const c = $('promptFilterRules');
  if (!c) return;
  if (!state.promptRules.length) {
    c.innerHTML = '<small class="text-xs muted-text">' + escapeHtml(t('promptFilter.noRules')) + '</small>';
    return;
  }
  c.innerHTML = state.promptRules.map((r, i) => {
    const isContains = r.type === 'lines-containing';
    const typeLabel = isContains ? t('promptFilter.typeContains') : t('promptFilter.typeRegex');
    const matchPh = isContains ? t('promptFilter.matchPlaceholderContains') : t('promptFilter.matchPlaceholderRegex');
    const replaceRow = !isContains
      ? '<div class="rule-field"><label>' + escapeHtml(t('promptFilter.replace')) + '</label>' +
      '<input value="' + escapeAttr(r.replace || '') + '" data-rule-idx="' + i + '" data-rule-field="replace" placeholder="' + escapeAttr(t('promptFilter.emptyRemove')) + '" />' +
      '</div>'
      : '';
    return '<div class="rule-card' + (r.enabled ? '' : ' disabled') + '">' +
      '<div class="rule-header">' +
      '<label class="switch"><input type="checkbox" ' + (r.enabled ? 'checked' : '') + ' data-rule-toggle="' + i + '" /><span class="slider"></span></label>' +
      '<div class="rule-meta">' +
      '<input class="rule-name-input" value="' + escapeAttr(r.name || '') + '" data-rule-idx="' + i + '" data-rule-field="name" placeholder="' + escapeAttr(t('promptFilter.unnamed')) + '" />' +
      '<span class="rule-type">' + escapeHtml(typeLabel) + '</span>' +
      '</div>' +
      '<button class="rule-remove" data-rule-remove="' + i + '" type="button" aria-label="' + escapeAttr(t('common.remove')) + '">&times;</button>' +
      '</div>' +
      '<div class="rule-body">' +
      '<div class="rule-field"><label>' + escapeHtml(t('promptFilter.match')) + '</label>' +
      '<input value="' + escapeAttr(r.match || '') + '" data-rule-idx="' + i + '" data-rule-field="match" placeholder="' + escapeAttr(matchPh) + '" />' +
      '</div>' +
      replaceRow +
      '</div>' +
      '</div>';
  }).join('');
}
export function addPromptRule(type) {
  state.promptRules.push({ id: 'rule-' + Date.now(), name: '', type, match: '', replace: '', enabled: true });
  renderPromptRules();
}

// Billing (token multiplier + model credit rates)
export async function loadBillingConfig() {
  try {
    const res = await api('/billing');
    const d = await res.json();
    const mult = d.tokenUsageMultiplier != null ? d.tokenUsageMultiplier : 1;
    const multEl = $('tokenUsageMultiplier');
    if (multEl) multEl.value = String(mult);
    state.builtinDefaultRate = d.builtinDefaultRate != null ? d.builtinDefaultRate : 0.003;
    const rates = d.modelCreditRates || {};
    const rows = Object.keys(rates).sort((a, b) => {
      if (a.toLowerCase() === 'default') return -1;
      if (b.toLowerCase() === 'default') return 1;
      return a.localeCompare(b);
    }).map(model => ({ model, rate: rates[model] }));
    if (!rows.some(r => String(r.model || '').toLowerCase() === 'default')) {
      rows.unshift({ model: 'default', rate: state.builtinDefaultRate });
    }
    state.creditRates = rows;
    renderCreditRateRows();
  } catch (e) {
    state.creditRates = [{ model: 'default', rate: state.builtinDefaultRate || 0.003 }];
    renderCreditRateRows();
  }
}

export function renderCreditRateRows() {
  const c = $('creditRateRows');
  if (!c) return;
  if (!state.creditRates.length) {
    c.innerHTML = '<small class="text-xs muted-text">' + escapeHtml(t('settings.creditRatesEmpty')) + '</small>';
    return;
  }
  c.innerHTML = state.creditRates.map((r, i) => {
    return '<div class="rule-card">' +
      '<div class="rule-header">' +
      '<div class="rule-meta" style="flex:1">' +
      '<span class="rule-type">' + escapeHtml(t('settings.creditRateModel')) + '</span>' +
      '</div>' +
      '<button class="rule-remove" data-credit-rate-remove="' + i + '" type="button" aria-label="' + escapeAttr(t('common.remove')) + '">&times;</button>' +
      '</div>' +
      '<div class="rule-body">' +
      '<div class="rule-field"><label>' + escapeHtml(t('settings.creditRateModel')) + '</label>' +
      '<input value="' + escapeAttr(r.model || '') + '" data-credit-rate-idx="' + i + '" data-credit-rate-field="model" placeholder="default / claude-opus" spellcheck="false" />' +
      '</div>' +
      '<div class="rule-field"><label>' + escapeHtml(t('settings.creditRateValue')) + '</label>' +
      '<input type="number" min="0" step="0.001" value="' + escapeAttr(String(r.rate != null ? r.rate : 0)) + '" data-credit-rate-idx="' + i + '" data-credit-rate-field="rate" />' +
      '</div>' +
      '</div>' +
      '</div>';
  }).join('');
}

export function addCreditRateRow() {
  state.creditRates.push({ model: '', rate: state.builtinDefaultRate || 0.003 });
  renderCreditRateRows();
}

export async function saveBillingConfig() {
  const multRaw = parseFloat(($('tokenUsageMultiplier') && $('tokenUsageMultiplier').value) || '');
  if (isNaN(multRaw) || multRaw <= 0) {
    toast(t('settings.billingInvalidMultiplier'), 'warning');
    return;
  }
  const rates = {};
  for (const row of (state.creditRates || [])) {
    const model = String(row.model || '').trim();
    if (!model) continue;
    const rate = parseFloat(row.rate);
    if (isNaN(rate) || rate < 0) {
      toast(t('settings.billingInvalidRate'), 'warning');
      return;
    }
    rates[model] = rate;
  }
  try {
    const res = await api('/billing', {
      method: 'POST',
      body: JSON.stringify({ tokenUsageMultiplier: multRaw, modelCreditRates: rates })
    });
    const d = await res.json().catch(() => ({}));
    if (!res.ok || d.success === false) throw new Error(d.error || t('common.saveFailed'));
    toast(t('settings.billingSaved'), 'success');
    await loadBillingConfig();
  } catch (e) {
    toast((e && e.message) || t('common.saveFailed'), 'error');
  }
}

export function bindBillingEvents() {
  const host = $('creditRateRows');
  if (!host || host.dataset.bound === '1') return;
  host.dataset.bound = '1';
  host.addEventListener('input', (e) => {
    const tEl = e.target;
    if (!tEl || !tEl.dataset) return;
    const idx = parseInt(tEl.dataset.creditRateIdx, 10);
    const field = tEl.dataset.creditRateField;
    if (isNaN(idx) || !field || !state.creditRates[idx]) return;
    if (field === 'rate') state.creditRates[idx].rate = tEl.value;
    else state.creditRates[idx][field] = tEl.value;
  });
  host.addEventListener('click', (e) => {
    const btn = e.target && e.target.closest ? e.target.closest('[data-credit-rate-remove]') : null;
    if (!btn) return;
    const idx = parseInt(btn.getAttribute('data-credit-rate-remove'), 10);
    if (isNaN(idx)) return;
    state.creditRates.splice(idx, 1);
    renderCreditRateRows();
  });
}

// Telegram notifications
export async function loadTelegramConfig() {
  try {
    const res = await api('/telegram');
    const d = await res.json();
    const en = $('telegramEnabled');
    if (en) en.checked = !!d.enabled;
    const chat = $('telegramChatId');
    if (chat) chat.value = d.chatId || '';
    const tok = $('telegramBotToken');
    if (tok) {
      tok.value = '';
      if (d.botTokenSet && d.botTokenMasked) {
        tok.placeholder = d.botTokenMasked;
      } else {
        tok.placeholder = '';
      }
    }
  } catch (e) {
    // leave defaults
  }
}

export async function saveTelegramConfig() {
  const enabled = !!($('telegramEnabled') && $('telegramEnabled').checked);
  const chatId = (($('telegramChatId') && $('telegramChatId').value) || '').trim();
  const body = { enabled, chatId };
  const tokVal = (($('telegramBotToken') && $('telegramBotToken').value) || '').trim();
  if (tokVal) body.botToken = tokVal;
  try {
    const res = await api('/telegram', { method: 'POST', body: JSON.stringify(body) });
    const d = await res.json().catch(() => ({}));
    if (!res.ok || d.success === false) throw new Error(d.error || t('common.saveFailed'));
    toast(t('settings.telegramSaved'), 'success');
    await loadTelegramConfig();
  } catch (e) {
    toast((e && e.message) || t('common.saveFailed'), 'error');
  }
}

export async function testTelegramConfig() {
  try {
    const res = await api('/telegram/test', { method: 'POST', body: '{}' });
    const d = await res.json().catch(() => ({}));
    if (!res.ok || d.success === false) throw new Error(d.error || t('settings.telegramTestFailed'));
    toast(t('settings.telegramTestOk'), 'success');
  } catch (e) {
    toast((e && e.message) || t('settings.telegramTestFailed'), 'error');
  }
}
