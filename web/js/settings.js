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
  await Promise.all([loadThinkingConfig(), loadEndpointConfig(), loadProxyConfig(), loadPromptFilter(), loadApiKeys()]);
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

export async function loadApiKeys() {
  const list = $('apiKeysList');
  if (!list) return;
  try {
    const res = await api('/api-keys');
    if (!res.ok) throw new Error('http ' + res.status);
    const d = await res.json();
    state.apiKeysCache = Array.isArray(d.apiKeys) ? d.apiKeys : [];
    renderApiKeys();
  } catch (e) {
    state.apiKeysCache = [];
    list.innerHTML = '<div class="muted-text" style="padding:0.5rem 0;">' + escapeHtml(t('apiKeys.loadFailed')) + '</div>';
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
  let color = '#3b82f6';
  if (ratio >= 0.95) color = '#ef4444';
  else if (ratio >= 0.8) color = '#f59e0b';
  return '<div style="height:6px;background:rgba(127,127,127,0.2);border-radius:3px;overflow:hidden;margin-top:4px;">' +
    '<div style="height:100%;width:' + pct + '%;background:' + color + ';transition:width 0.3s;"></div>' +
    '</div>';
}

export function usageLine(label, used, limit, options) {
  options = options || {};
  const fmt = options.fmt || formatNumber;
  if (!limit || limit <= 0) {
    return '<div class="text-xs muted-text">' + escapeHtml(label) + ': ' + escapeHtml(fmt(used)) + ' / ' + escapeHtml(t('apiKeys.unlimited')) + '</div>';
  }
  return '<div class="text-xs muted-text">' + escapeHtml(label) + ': ' + escapeHtml(fmt(used)) + ' / ' + escapeHtml(fmt(limit)) + '</div>' + usageBar(used, limit);
}

export function renderApiKeys() {
  const list = $('apiKeysList');
  if (!list) return;
  if (!state.apiKeysCache.length) {
    list.innerHTML = '<div class="muted-text" style="padding:0.5rem 0;">' + escapeHtml(t('apiKeys.empty')) + '</div>';
    return;
  }
  const html = state.apiKeysCache.map(item => {
    const id = escapeAttr(item.id || '');
    const name = item.name ? escapeHtml(item.name) : '<span class="muted-text">' + escapeHtml(t('apiKeys.unnamed')) + '</span>';
    const masked = escapeHtml(item.keyMasked || '');
    const migrated = item.migrated
      ? '<span class="text-xs" style="background:rgba(59,130,246,0.15);color:#3b82f6;padding:1px 6px;border-radius:4px;">' + escapeHtml(t('apiKeys.migrated')) + '</span>'
      : '';
    const disabled = !item.enabled
      ? '<span class="text-xs" style="background:rgba(239,68,68,0.15);color:#ef4444;padding:1px 6px;border-radius:4px;">' + escapeHtml(t('apiKeys.disabled')) + '</span>'
      : '';
    const expired = item.expired
      ? '<span class="text-xs" style="background:rgba(239,68,68,0.15);color:#ef4444;padding:1px 6px;border-radius:4px;">' + escapeHtml(t('apiKeys.expired')) + '</span>'
      : '';
    const tokensLine = usageLine(t('apiKeys.tokens'), item.tokensUsed || 0, item.tokenLimit || 0);
    const creditsLine = usageLine(t('apiKeys.credits'), item.creditsUsed || 0, item.creditLimit || 0);
    const requestsLine = '<div class="text-xs muted-text">' + escapeHtml(t('apiKeys.requests')) + ': ' + escapeHtml(formatNumber(item.requestsCount || 0)) + '</div>';
    const expiryText = item.expiresAt
      ? new Date(item.expiresAt * 1000 - 1000).toLocaleDateString()
      : t('apiKeys.neverExpires');
    const expiryLine = '<div class="text-xs muted-text">' + escapeHtml(t('apiKeys.expiry')) + ': ' + escapeHtml(expiryText) + '</div>';
    return '<div class="card" data-apikey-id="' + id + '" style="margin-top:0.5rem;padding:0.75rem;">' +
      '<div class="flex items-center gap-2" style="flex-wrap:wrap;justify-content:space-between;">' +
        '<div class="flex items-center gap-2" style="flex-wrap:wrap;">' +
          '<span class="font-semibold">' + name + '</span>' +
          migrated +
          disabled +
          expired +
          '<span class="text-xs muted-text font-mono">' + masked + '</span>' +
        '</div>' +
        '<div class="flex items-center gap-2">' +
          '<label class="switch" title="' + escapeAttr(item.enabled ? t('accounts.disable') : t('accounts.enable')) + '">' +
            '<input type="checkbox" data-apikey-action="toggle" data-id="' + id + '"' + (item.enabled ? ' checked' : '') + ' />' +
            '<span class="slider"></span>' +
          '</label>' +
          '<button class="btn btn-outline btn-sm" type="button" data-apikey-action="edit" data-id="' + id + '">' + escapeHtml(t('apiKeys.actionEdit')) + '</button>' +
          '<button class="btn btn-outline btn-sm" type="button" data-apikey-action="reset" data-id="' + id + '">' + escapeHtml(t('apiKeys.actionReset')) + '</button>' +
          '<button class="btn btn-danger btn-sm" type="button" data-apikey-action="delete" data-id="' + id + '">' + escapeHtml(t('apiKeys.actionDelete')) + '</button>' +
        '</div>' +
      '</div>' +
      '<div style="margin-top:0.5rem;display:grid;gap:0.35rem;">' +
        tokensLine +
        creditsLine +
        requestsLine +
        expiryLine +
      '</div>' +
    '</div>';
  }).join('');
  list.innerHTML = html;
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
      const btn = e.target.closest('[data-apikey-action]');
      if (!btn) return;
      const action = btn.dataset.apikeyAction;
      const id = btn.dataset.id;
      if (!id) return;
      const entry = state.apiKeysCache.find(x => x.id === id);
      const name = entry ? entry.name : '';
      if (action === 'edit') openApiKeyModal(entry);
      else if (action === 'delete') deleteApiKeyEntry(id, name);
      else if (action === 'reset') resetApiKeyUsageEntry(id, name);
    });
    list.addEventListener('change', e => {
      const cb = e.target.closest('input[data-apikey-action="toggle"]');
      if (!cb) return;
      const id = cb.dataset.id;
      if (!id) return;
      toggleApiKeyEntry(id, cb.checked);
    });
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
