// accounts.js — account list, filters, batch actions, detail modal, test modal.
//
// Depends on state.js (shared state) and core.js (DOM/i18n/dialog/api helpers).
// Calls a few main-module functions at runtime (loadStats, renderProviderNav,
// renderUsageView); that import is circular but safe because every such call is
// inside a function body (evaluated after all modules finish loading), not at
// top level.
import { state, selectedAccounts, agQuotaExpanded } from './state.js';
import {
  $, qsa, escapeHtml, escapeAttr, copyText, t, api, loaderHtml,
  toast, toastPrimary, toastError,
  openDialog, closeDialog, closeAllCustomSelects, confirmAction,
  enhanceCustomSelects, getDisplayEmail,
} from './core.js';
import { loadStats, renderProviderNav, renderUsageView } from '../app.js';

export async function loadAccounts() {
  const res = await api('/accounts');
  state.accountsData = await res.json();
  renderAccounts();
  renderProviderNav();
  if (state.currentView === 'usage') renderUsageView();
}

// Account list
export function getFilteredAccounts() {
  return state.accountsData.filter(a => {
    if (state.currentProviderFilter && accountProviderKey(a) !== state.currentProviderFilter) return false;
    if (state.filterStatus === 'enabled' && !a.enabled) return false;
    if (state.filterStatus === 'disabled' && (a.enabled || (a.banStatus && a.banStatus !== 'ACTIVE'))) return false;
    if (state.filterStatus === 'banned' && (!a.banStatus || a.banStatus === 'ACTIVE')) return false;
    if (state.filterKeyword) {
      const kw = state.filterKeyword.toLowerCase();
      if (!(a.email || '').toLowerCase().includes(kw)) return false;
    }
    return true;
  });
}
export function onFilterChange() {
  state.filterKeyword = $('filterSearch').value;
  state.filterStatus = $('filterStatusSelect').value;
  renderAccounts();
}
export function toggleSelectAll(checked) {
  const filtered = getFilteredAccounts();
  if (checked) filtered.forEach(a => selectedAccounts.add(a.id));
  else selectedAccounts.clear();
  renderAccounts();
  updateBatchBar();
}
export function toggleSelectAccount(id) {
  if (selectedAccounts.has(id)) selectedAccounts.delete(id);
  else selectedAccounts.add(id);
  updateBatchBar();
}
export function updateBatchBar() {
  const bar = $('batchBar');
  const count = selectedAccounts.size;
  const cb = $('selectAllCheckbox');
  if (cb) {
    const filtered = getFilteredAccounts();
    const selectedFiltered = filtered.filter(a => selectedAccounts.has(a.id)).length;
    cb.checked = filtered.length > 0 && selectedFiltered === filtered.length;
    cb.indeterminate = selectedFiltered > 0 && selectedFiltered < filtered.length;
  }
  if (count > 0) {
    bar.classList.remove('hidden');
    $('batchCount').textContent = String(count);
  } else {
    bar.classList.add('hidden');
  }
}

export function formatSubscriptionLabel(type) {
  const s = (type || '').toUpperCase();
  if (s.includes('POWER')) return t('subscription.power');
  if (s.includes('PRO_PLUS') || s.includes('PROPLUS')) return t('subscription.proPlus');
  if (s.includes('PRO')) return t('subscription.pro');
  if (s.includes('FREE')) return t('subscription.free');
  return type || t('subscription.free');
}
export function getSubBadge(type) {
  const s = (type || '').toUpperCase();
  if (s.includes('POWER')) return '<span class="badge badge-power">' + escapeHtml(formatSubscriptionLabel(type)) + '</span>';
  if (s.includes('PRO_PLUS') || s.includes('PROPLUS')) return '<span class="badge badge-proplus">' + escapeHtml(formatSubscriptionLabel(type)) + '</span>';
  if (s.includes('PRO')) return '<span class="badge badge-pro">' + escapeHtml(formatSubscriptionLabel(type)) + '</span>';
  return '<span class="badge badge-free">' + escapeHtml(formatSubscriptionLabel(type)) + '</span>';
}
export function getTrialBadge(a) {
  if (a.trialStatus === 'ACTIVE' && a.trialUsageLimit > 0) {
    return '<span class="badge badge-trial">' + escapeHtml(t('accounts.trial')) + '</span>';
  }
  return '';
}
export function formatTrialExpiry(ts) {
  if (!ts) return '';
  const date = new Date(ts * 1000);
  const diffDays = Math.ceil((date - new Date()) / (1000 * 60 * 60 * 24));
  if (diffDays < 0) return '(' + t('accounts.trialExpired') + ')';
  if (diffDays === 0) return '(' + t('accounts.trialToday') + ')';
  if (diffDays <= 7) return '(' + diffDays + t('accounts.trialDays') + ')';
  return '';
}
export function formatAuthMethod(method) {
  if (!method) return '-';
  const normalized = String(method).toLowerCase();
  if (normalized === 'idc') return t('auth.enterprise');
  if (normalized === 'social') return t('auth.social');
  if (normalized === 'builderid') return 'BuilderID';
  if (normalized === 'github') return t('local.providerGithub');
  if (normalized === 'google') return t('local.providerGoogle');
  if (normalized === 'grok' || normalized === 'xai') return t('provider.grok') || 'Grok / xAI';
  return method;
}
// accountProviderKey buckets an account into one of the sidebar provider
// groups. Mirrors the Go-side detection (isGrokAccount / isAntigravityAccount):
// anything not Grok/Antigravity is treated as a Kiro (AWS) account.
export function accountProviderKey(a) {
  const p = String(a.provider || '').toLowerCase();
  const m = String(a.authMethod || '').toLowerCase();
  if (p === 'grok' || p === 'xai' || m === 'grok' || a.grokApiKey) return 'grok';
  if (p === 'antigravity' || m === 'antigravity') return 'antigravity';
  return 'kiro';
}
// Display label + icon for each provider bucket, used by the sidebar nav.
export const PROVIDER_NAV = [
  { key: 'kiro', labelKey: 'provider.kiro', icon: 'fa-solid fa-robot' },
  { key: 'antigravity', labelKey: 'provider.antigravity', icon: 'fa-brands fa-google' },
  { key: 'grok', labelKey: 'provider.grok', icon: 'fa-solid fa-bolt' }
];
export function getStatusBadge(a) {
  const out = [];
  const isBanned = a.banStatus && a.banStatus !== 'ACTIVE';
  if (isBanned) {
    if (a.banStatus === 'BANNED') out.push('<span class="badge badge-banned">' + escapeHtml(t('accounts.banned')) + '</span>');
    else if (a.banStatus === 'SUSPENDED') out.push('<span class="badge badge-suspended">' + escapeHtml(t('accounts.suspended')) + '</span>');
    out.push('<span class="badge badge-warning">' + escapeHtml(t('accounts.disabled')) + '</span>');
  } else {
    if (!a.hasToken)
      out.push('<span class="badge badge-error">' + escapeHtml(t('accounts.noToken')) + '</span>');
    else if (a.expiresAt && a.expiresAt < Date.now() / 1000)
      out.push('<span class="badge badge-warning">' + escapeHtml(t('accounts.expired')) + '</span>');
    else
      out.push('<span class="badge badge-success">' + escapeHtml(t('accounts.normal')) + '</span>');
    out.push(a.enabled
      ? '<span class="badge badge-info">' + escapeHtml(t('accounts.enabled')) + '</span>'
      : '<span class="badge badge-warning">' + escapeHtml(t('accounts.disabled')) + '</span>');
  }
  return out.join('');
}
export function formatTokenExpiry(ts) {
  if (!ts) return '-';
  const diff = ts - Date.now() / 1000;
  if (diff <= 0) return t('time.expired');
  if (diff < 3600) return Math.floor(diff / 60) + t('time.minutes');
  if (diff < 86400) return Math.floor(diff / 3600) + t('time.hours');
  return Math.floor(diff / 86400) + t('time.days');
}
export function formatNum(n) {
  if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M';
  if (n >= 1e3) return (n / 1e3).toFixed(1) + 'K';
  return n.toString();
}
export function applyUsageBars(root) {
  qsa('.usage-fill[data-usage-pct]', root).forEach(el => {
    const pct = Math.max(0, Math.min(100, parseFloat(el.dataset.usagePct) || 0));
    el.style.width = pct + '%';
  });
}

export function formatAgResetTime(ts) {
  if (!ts) return '';
  // resetTime is an RFC3339 timestamp string from Gemini Code Assist.
  const date = new Date(ts);
  if (isNaN(date.getTime())) return '';
  const diffMs = date - new Date();
  if (diffMs <= 0) return '';
  const diffH = Math.floor(diffMs / (1000 * 60 * 60));
  if (diffH < 24) return diffH + t('time.hours');
  return Math.floor(diffH / 24) + t('time.days');
}

// renderAntigravityQuota renders the per-model quota returned by the Antigravity
// Cloud Code :fetchAvailableModels endpoint. Each entry carries a remaining
// fraction (0.0-1.0) that drives a small bar per model.
export function renderAntigravityQuota(a) {
  const quota = Array.isArray(a.agQuota) ? a.agQuota : [];
  if (quota.length === 0) return '';
  let minRemain = 100;
  const items = quota.map(b => {
    const remainPct = Math.max(0, Math.min(100, (b.remainingFraction || 0) * 100));
    const usedPct = 100 - remainPct;
    const barClass = remainPct < 10 ? 'critical' : remainPct < 30 ? 'high' : '';
    if (remainPct < minRemain) minRemain = remainPct;
    const name = b.displayName || b.modelId || '-';
    const reset = formatAgResetTime(b.resetTime);
    const resetLabel = reset ? ' · ' + t('antigravity.quotaReset', reset) : '';
    const tooltip = name + resetLabel;
    return '' +
      '<div class="ag-quota-item" title="' + escapeAttr(tooltip) + '">' +
      '<div class="ag-quota-name">' + escapeHtml(name) + '</div>' +
      '<div class="usage-bar"><div class="usage-fill ' + barClass + '" data-usage-pct="' + escapeAttr(usedPct) + '"></div></div>' +
      '<div class="ag-quota-pct">' + remainPct.toFixed(0) + '%</div>' +
      '</div>';
  }).join('');
  const summary = t('antigravity.quotaSummary', quota.length, minRemain.toFixed(0));
  const open = agQuotaExpanded.has(a.id) ? ' open' : '';
  return '<details class="ag-quota" data-id="' + escapeAttr(a.id) + '"' + open + '>' +
    '<summary class="ag-quota-summary">' +
    '<span class="ag-quota-title">' + escapeHtml(t('antigravity.quotaTitle')) + '</span>' +
    '<span class="ag-quota-meta">' + escapeHtml(summary) + '</span>' +
    '<i class="fa-solid fa-chevron-down ag-quota-chevron" aria-hidden="true"></i>' +
    '</summary>' +
    '<div class="ag-quota-grid">' + items + '</div>' +
    '</details>';
}

// renderGrokInfo shows a small info row for Grok accounts (OAuth or API key).
export function renderGrokInfo(a) {
  const isGrok = (a.provider && (a.provider.toLowerCase() === 'grok' || a.provider.toLowerCase() === 'xai')) ||
                 (a.authMethod && a.authMethod.toLowerCase() === 'grok') ||
                 a.grokApiKey;
  if (!isGrok) return '';

  const authType = a.grokAuthType || (a.grokApiKey ? 'apikey' : 'oauth');
  let info = '';

  if (authType === 'apikey' || a.grokApiKey) {
    const masked = a.grokApiKey ? (a.grokApiKey.slice(0, 6) + '••••' + a.grokApiKey.slice(-4)) : '••••••••';
    info = '<span class="badge badge-info">xAI Key: ' + escapeHtml(masked) + '</span>';
  } else if (authType === 'oauth' || authType === 'grok-oauth') {
    info = '<span class="badge badge-info">Grok Build OAuth</span>';
  } else {
    info = '<span class="badge badge-info">Grok / xAI</span>';
  }

  return '<div class="account-grok-info" style="margin: 4px 0 8px; font-size: 12px;">' + info + '</div>';
}

export function isGrokAccountDetail(a) {
  if (!a) return false;
  const p = String(a.provider || '').toLowerCase();
  const m = String(a.authMethod || '').toLowerCase();
  return p === 'grok' || p === 'xai' || m === 'grok' || !!a.grokApiKey;
}

export function renderGrokDetailSection(a, idAttr) {
  const authType = a.grokAuthType || (a.grokApiKey ? 'apikey' : 'oauth');
  let credsHtml = '';

  if (a.grokApiKey) {
    const masked = a.grokApiKey.slice(0, 8) + '••••••••' + a.grokApiKey.slice(-4);
    credsHtml += detailItem(t('grok.apiKey') || 'xAI API Key', masked);
  }

  const typeLabel = (authType === 'oauth' || authType === 'grok-oauth') ? 'Grok Build OAuth' : authType;
  return '' +
    '<div class="detail-section"><h4>' + escapeHtml(t('provider.grok') || 'Grok / xAI') + '</h4><div class="detail-grid">' +
    detailItem(t('grok.authType') || 'Auth Type', typeLabel) +
    credsHtml +
    '</div>' +
    '<p class="help-block" style="margin-top:6px;font-size:12px;">' + escapeHtml(t('grok.detailHint') || 'Grok credentials are stored securely. Use the Test button to verify connectivity.') + '</p>' +
    '</div>';
}

export function renderAccounts() {
  const container = $('accountsList');
  if (!container) return;
  const filtered = getFilteredAccounts();
  if (filtered.length === 0) {
    container.innerHTML = '<div class="empty-state">' + escapeHtml(t('accounts.empty')) + '</div>';
    return;
  }
  container.innerHTML = filtered.map(a => {
    const usagePct = (a.usagePercent || 0) * 100;
    const usageClass = usagePct > 90 ? 'critical' : usagePct > 70 ? 'high' : '';
    const trialPct = (a.trialUsagePercent || 0) * 100;
    const trialClass = trialPct > 90 ? 'critical' : trialPct > 70 ? 'high' : '';
    const isSelected = selectedAccounts.has(a.id);
    const weight = a.weight || 0;
    const weightBadge = weight >= 2 ? '<span class="badge badge-warning">' + escapeHtml(t('accounts.weightShort')) + ':' + weight + '</span>' : '';
    const overageBadge = renderOverageBadge(a);
    const banned = a.banStatus && a.banStatus !== 'ACTIVE';
    const idAttr = escapeAttr(a.id);
    const displayEmail = getDisplayEmail(a.email, a.id);
    const selectLabel = t('accounts.selectAccount', displayEmail);

    const refreshSvg = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M23 4v6h-6M1 20v-6h6"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/></svg>';
    const userSvg = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>';
    const copySvg = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>';

    return '' +
      '<div class="account-card' + (isSelected ? ' selected' : '') + '" data-id="' + idAttr + '">' +
      '<div class="account-header">' +
      '<div class="account-info">' +
      '<input type="checkbox" class="account-checkbox" ' + (isSelected ? 'checked' : '') + ' data-id="' + idAttr + '" aria-label="' + escapeAttr(selectLabel) + '" />' +
      '<div class="account-info-text">' +
      '<div class="account-email">' + escapeHtml(displayEmail) + '</div>' +
      '<div class="account-meta">' +
      getSubBadge(a.subscriptionType) +
      getTrialBadge(a) +
      weightBadge +
      overageBadge +
      '<span class="badge badge-info">' + escapeHtml(formatAuthMethod(a.provider || a.authMethod)) + '</span>' +
      getStatusBadge(a) +
      '</div>' +
      '</div>' +
      '</div>' +
      '<div class="account-actions">' +
      '<button class="btn btn-icon btn-sm btn-ghost" data-action="refresh" data-id="' + idAttr + '" title="' + escapeAttr(t('accounts.refresh')) + '">' + refreshSvg + '</button>' +
      '<button class="btn btn-icon btn-sm btn-ghost" data-action="detail" data-id="' + idAttr + '" title="' + escapeAttr(t('accounts.detail')) + '">' + userSvg + '</button>' +
      '<button class="btn btn-icon btn-sm btn-ghost" data-action="copyJSON" data-id="' + idAttr + '" title="' + escapeAttr(t('accounts.copyJSON')) + '">' + copySvg + '</button>' +
      (banned ? '' :
        '<button class="btn btn-sm ' + (a.enabled ? 'btn-outline' : 'btn-primary') + '" data-action="toggle" data-id="' + idAttr + '" data-enabled="' + (!a.enabled) + '">' +
        escapeHtml(a.enabled ? t('accounts.disable') : t('accounts.enable')) +
        '</button>') +
      '<button class="btn btn-sm btn-secondary" data-action="test" data-id="' + idAttr + '" id="test-' + idAttr + '">' + escapeHtml(t('accounts.test')) + '</button>' +
      '<button class="btn btn-sm btn-danger" data-action="delete" data-id="' + idAttr + '">' + escapeHtml(t('accounts.delete')) + '</button>' +
      '</div>' +
      '</div>' +
      (a.usageLimit > 0 ?
        '<div class="account-usage">' +
        '<div class="usage-label">' + escapeHtml(t('accounts.mainQuota')) + '</div>' +
        '<div class="usage-bar"><div class="usage-fill ' + usageClass + '" data-usage-pct="' + escapeAttr(usagePct) + '"></div></div>' +
        '<div class="usage-text"><span>' + (a.usageCurrent != null ? a.usageCurrent.toFixed(1) : 0) + ' / ' + (a.usageLimit != null ? a.usageLimit.toFixed(0) : 0) + '</span><span>' + usagePct.toFixed(1) + '%</span></div>' +
        '</div>' : '') +
      (a.trialUsageLimit > 0 ?
        '<div class="account-usage">' +
        '<div class="usage-label">' + escapeHtml(t('accounts.trialQuota')) + ' ' + escapeHtml(formatTrialExpiry(a.trialExpiresAt)) + '</div>' +
        '<div class="usage-bar"><div class="usage-fill ' + trialClass + '" data-usage-pct="' + escapeAttr(trialPct) + '"></div></div>' +
        '<div class="usage-text"><span>' + (a.trialUsageCurrent != null ? a.trialUsageCurrent.toFixed(1) : 0) + ' / ' + (a.trialUsageLimit != null ? a.trialUsageLimit.toFixed(0) : 0) + '</span><span>' + trialPct.toFixed(1) + '%</span></div>' +
        '</div>' : '') +
      renderAntigravityQuota(a) +
      renderGrokInfo(a) +
      '<div class="account-stats">' +
      '<div class="account-stat"><div class="account-stat-value">' + (a.requestCount || 0) + '</div><div class="account-stat-label">' + escapeHtml(t('accounts.requests')) + '</div></div>' +
      '<div class="account-stat"><div class="account-stat-value">' + formatNum(a.totalTokens || 0) + '</div><div class="account-stat-label">' + escapeHtml(t('accounts.tokens')) + '</div></div>' +
      '<div class="account-stat"><div class="account-stat-value">' + (a.totalCredits || 0).toFixed(1) + '</div><div class="account-stat-label">' + escapeHtml(t('accounts.credits')) + '</div></div>' +
      '<div class="account-stat"><div class="account-stat-value">' + escapeHtml(formatTokenExpiry(a.expiresAt)) + '</div><div class="account-stat-label">' + escapeHtml(t('accounts.expiry')) + '</div></div>' +
      '</div>' +
      '</div>';
  }).join('');
  applyUsageBars(container);
  enhanceCustomSelects(container);
}

// Account actions
export async function refreshAccount(id, card) {
  if (card) card.classList.add('loading');
  try {
    const res = await api('/accounts/' + id + '/refresh', { method: 'POST' });
    const d = await res.json();
    if (d.success) loadAccounts();
    else toastError(t('accounts.refreshFailed') + ': ' + (d.error || ''));
  } catch (e) {
    toastError(t('accounts.refreshFailed'));
  }
  if (card) card.classList.remove('loading');
}
export async function toggleAccount(id, enabled) {
  await api('/accounts/' + id, { method: 'PUT', body: JSON.stringify({ enabled }) });
  loadAccounts();
}
export async function deleteAccount(id) {
  const ok = await confirmAction(t('accounts.confirmDelete'), {
    title: t('accounts.delete'),
    confirmText: t('accounts.delete'),
    variant: 'danger'
  });
  if (!ok) return;
  try {
    const res = await api('/accounts/' + id, { method: 'DELETE' });
    const d = await res.json().catch(() => ({}));
    if (!res.ok || d.success === false) throw new Error(d.error || t('common.failed'));
    toast(t('accounts.deleteSuccess'), 'danger', { icon: 'fa-solid fa-trash' });
    loadAccounts(); loadStats();
  } catch (e) {
    toast((e && e.message) || t('common.failed'), 'error');
  }
}
export async function copyAccountJSON(id, btn) {
  try {
    const jsonPromise = api('/accounts/' + id + '/full').then(async res => {
      if (!res.ok) throw new Error('Failed');
      const a = await res.json();
      const { clientId, clientSecret, accessToken, refreshToken } = a;
      return JSON.stringify({ clientId, clientSecret, accessToken, refreshToken }, null, 2);
    });
    await copyText(jsonPromise);
    flashCopySuccess(btn);
    toastPrimary(t('accounts.copyJSONSuccess'));
  } catch (e) {
    toastError(t('common.failed'));
  }
}
export function flashCopySuccess(btn) {
  if (!btn) return;
  const html = btn.innerHTML, cls = btn.className;
  btn.disabled = true;
  btn.className = 'btn btn-icon btn-sm btn-success';
  btn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>';
  setTimeout(() => { btn.disabled = false; btn.className = cls; btn.innerHTML = html; }, 800);
}

// Batch actions
export async function batchAction(action) {
  const ids = Array.from(selectedAccounts);
  if (!ids.length) return;
  const confirmKey = 'batch.confirm' + action.charAt(0).toUpperCase() + action.slice(1);
  const ok = await confirmAction(t(confirmKey, ids.length), {
    title: t('common.confirm'),
    confirmText: t('common.confirm'),
    variant: action === 'disable' ? 'danger' : 'primary'
  });
  if (!ok) return;
  const dismiss = toast(t('batch.processing'), 'info', { duration: 0 });
  try {
    const res = await api('/accounts/batch', { method: 'POST', body: JSON.stringify({ ids, action }) });
    const d = await res.json();
    if (!res.ok || !d.success) throw new Error(d.error || t('common.failed'));
    dismiss();
    if (action === 'refresh') {
      toast(t('batch.refreshResult', d.refreshed || 0, d.failed || 0), d.failed ? 'warning' : 'success');
    } else if (action === 'enable') {
      toast(t('batch.enableResult', d.count || ids.length), 'success');
    } else if (action === 'disable') {
      toast(t('batch.disableResult', d.count || ids.length), 'success');
    } else {
      toast(t('batch.done'), 'success');
    }
    selectedAccounts.clear();
    updateBatchBar();
    loadAccounts(); loadStats();
  } catch (e) {
    dismiss();
    toast((e && e.message) || t('common.failed'), 'error');
  }
}
export async function batchRefreshModels() {
  const ids = Array.from(selectedAccounts);
  if (!ids.length) return;
  const confirmed = await confirmAction(t('batch.confirmRefreshModels', ids.length), {
    title: t('models.refreshAll'),
    confirmText: t('common.confirm')
  });
  if (!confirmed) return;
  const dismiss = toast(t('detail.refreshModelCache') + '…', 'info', { duration: 0 });
  let ok = 0, fail = 0;
  for (const id of ids) {
    try {
      const res = await api('/accounts/' + id + '/models/refresh', { method: 'POST' });
      const d = await res.json();
      if (d.success) ok++; else fail++;
    } catch { fail++; }
  }
  dismiss();
  toast(t('batch.refreshModelsResult', ok, fail), fail ? 'warning' : 'success');
  selectedAccounts.clear();
  updateBatchBar();
  loadAccounts();
}
export async function batchDelete() {
  const ids = Array.from(selectedAccounts);
  if (!ids.length) return;
  const confirmed = await confirmAction(t('batch.confirmDelete', ids.length), {
    title: t('accounts.delete'),
    confirmText: t('accounts.delete'),
    variant: 'danger'
  });
  if (!confirmed) return;
  const dismiss = toast(t('batch.deleting'), 'info', { duration: 0 });
  let ok = 0, fail = 0;
  for (const id of ids) {
    try {
      const res = await api('/accounts/' + id, { method: 'DELETE' });
      const d = await res.json().catch(() => ({}));
      if (res.ok && d.success !== false) ok++; else fail++;
    } catch { fail++; }
  }
  dismiss();
  toast(t('batch.deleteResult', ok, fail), fail ? 'warning' : 'success', { icon: 'fa-solid fa-trash' });
  selectedAccounts.clear();
  updateBatchBar();
  loadAccounts(); loadStats();
}
export async function refreshAllModels() {
  const ok = await confirmAction(t('models.confirmRefreshAll'), {
    title: t('models.refreshAll'),
    confirmText: t('models.refreshAll')
  });
  if (!ok) return;
  const dismiss = toast(t('detail.refreshModelCache') + '…', 'info', { duration: 0 });
  try {
    const res = await api('/accounts/models/refresh', { method: 'POST' });
    const d = await res.json();
    dismiss();
    toast(t('models.refreshAllDone', d.refreshed || 0), 'success');
  } catch (e) {
    dismiss();
    toast(t('common.failed'), 'error');
  }
}
export async function refreshAccountModels(id) {
  const dismiss = toast(t('detail.refreshModelCache') + '…', 'info', { duration: 0 });
  try {
    const res = await api('/accounts/' + id + '/models/refresh', { method: 'POST' });
    const d = await res.json();
    dismiss();
    if (d.success) toast(t('detail.refreshModelCache') + ' · ' + (d.count || 0), 'success');
    else toast(t('common.failed') + (d.error ? ': ' + d.error : ''), 'error');
  } catch (e) {
    dismiss();
    toast(t('common.failed'), 'error');
  }
}

// Detail modal
export function detailItem(label, value) {
  return '<div class="detail-item"><div class="detail-label">' + escapeHtml(label) + '</div><div class="detail-value">' + escapeHtml(value) + '</div></div>';
}
export function showDetail(id) {
  const a = state.accountsData.find(x => x.id === id);
  if (!a) return;
  const idAttr = escapeAttr(id);
  $('detailBody').innerHTML =
    '<div class="detail-section"><h4>' + escapeHtml(t('detail.basicInfo')) + '</h4><div class="detail-grid">' +
    detailItem(t('detail.email'), getDisplayEmail(a.email, null)) +
    detailItem(t('detail.userId'), a.userId || '-') +
    detailItem(t('detail.authMethod'), formatAuthMethod(a.provider || a.authMethod)) +
    detailItem(t('detail.region'), a.region || 'us-east-1') +
    '</div></div>' +

    (isGrokAccountDetail(a) ? renderGrokDetailSection(a, idAttr) : '') +

    '<div class="detail-section"><h4>' + escapeHtml(t('detail.machineId')) + '</h4><div class="machine-id-row">' +
    '<input type="text" id="machineIdInput" value="' + escapeAttr(a.machineId || '') + '" placeholder="UUID" />' +
    '<button class="btn btn-sm btn-outline" id="generateMachineIdBtn" type="button">' + escapeHtml(t('detail.generate')) + '</button>' +
    '<button class="btn btn-sm btn-primary" data-detail-action="saveMachineId" data-id="' + idAttr + '" type="button">' + escapeHtml(t('detail.save')) + '</button>' +
    '</div></div>' +

    '<div class="detail-section"><h4>' + escapeHtml(t('detail.weight')) + '</h4>' +
    '<div class="form-group">' +
    '<input type="number" id="weightInput" value="' + (a.weight || 0) + '" min="0" max="10" />' +
    '<small>' + escapeHtml(t('detail.weightHint')) + '</small>' +
    '</div>' +
    '<button class="btn btn-sm btn-primary" data-detail-action="saveWeight" data-id="' + idAttr + '" type="button">' + escapeHtml(t('detail.save')) + '</button>' +
    '</div>' +

    '<div class="detail-section">' +
    '<h4>' + escapeHtml(t('detail.overage')) +
    ' <button class="btn btn-sm btn-outline" data-detail-action="refreshOverage" data-id="' + idAttr + '" type="button">' + escapeHtml(t('detail.overageRefresh')) + '</button>' +
    '</h4>' +
    '<p class="help-block">' + escapeHtml(t('detail.overageHint')) + '</p>' +
    renderOverageBlock(a, idAttr) +
    '</div>' +

    '<div class="detail-section"><h4>' + escapeHtml(t('detail.proxyURL')) + '</h4><div class="machine-id-row">' +
    '<input type="text" id="proxyURLInput" value="' + escapeAttr(a.proxyURL || '') + '" placeholder="socks5://host:port" />' +
    '<button class="btn btn-sm btn-primary" data-detail-action="saveProxyURL" data-id="' + idAttr + '" type="button">' + escapeHtml(t('detail.save')) + '</button>' +
    '</div><p class="help-block">' + escapeHtml(t('detail.proxyHint')) + '</p></div>' +

    '<div class="detail-section"><h4>' + escapeHtml(t('detail.subscription')) + '</h4><div class="detail-grid">' +
    detailItem(t('detail.subscriptionType'), a.subscriptionTitle || (a.subscriptionType ? formatSubscriptionLabel(a.subscriptionType) : '-')) +
    detailItem(t('detail.tokenExpiry'), a.expiresAt ? new Date(a.expiresAt * 1000).toLocaleString() : '-') +
    detailItem(t('detail.mainQuota'), (a.usageCurrent != null ? a.usageCurrent.toFixed(1) : 0) + ' / ' + (a.usageLimit != null ? a.usageLimit.toFixed(0) : 0)) +
    detailItem(t('detail.resetDate'), a.nextResetDate || '-') +
    (a.trialUsageLimit > 0 ?
      detailItem(t('detail.trialQuota'), (a.trialUsageCurrent != null ? a.trialUsageCurrent.toFixed(1) : 0) + ' / ' + a.trialUsageLimit.toFixed(0)) +
      detailItem(t('detail.trialStatus'), a.trialStatus || '-') +
      detailItem(t('detail.trialExpiry'), a.trialExpiresAt ? new Date(a.trialExpiresAt * 1000).toLocaleString() : '-')
      : '') +
    '</div></div>' +

    '<div class="detail-section"><h4>' + escapeHtml(t('detail.statistics')) + '</h4><div class="detail-grid">' +
    detailItem(t('detail.requestCount'), a.requestCount || 0) +
    detailItem(t('detail.errorCount'), a.errorCount || 0) +
    detailItem(t('detail.totalTokens'), formatNum(a.totalTokens || 0)) +
    detailItem(t('detail.totalCredits'), (a.totalCredits || 0).toFixed(2)) +
    '</div></div>' +

    '<div class="detail-section">' +
    '<h4>' + escapeHtml(t('detail.models')) +
    ' <button class="btn btn-sm btn-outline" data-detail-action="loadModels" data-id="' + idAttr + '" type="button">' + escapeHtml(t('detail.loadModels')) + '</button>' +
    ' <button class="btn btn-sm btn-outline" data-detail-action="refreshModels" data-id="' + idAttr + '" type="button">' + escapeHtml(t('detail.refreshModelCache')) + '</button>' +
    '</h4>' +
    '<div id="modelsList" class="model-list"></div>' +
    '</div>';

  openDialog('detailModal');
}
export async function loadModels(id) {
  const c = $('modelsList');
  c.innerHTML = loaderHtml(t('detail.loading'));
  try {
    const res = await api('/accounts/' + id + '/models');
    const d = await res.json();
    if (d.success && d.models) {
      const sorted = d.models.slice().sort((a, b) => {
        if (a.modelId === 'auto') return -1;
        if (b.modelId === 'auto') return 1;
        return (a.rateMultiplier || 1) - (b.rateMultiplier || 1);
      });
      c.innerHTML = sorted.map(m => {
        const ratio = m.rateMultiplier || 1;
        return '<div class="model-item">' +
          '<div class="model-name">' + escapeHtml(m.modelId) + '</div>' +
          '<div class="model-credit"><span class="credit-ratio">' + escapeHtml(t('detail.creditMultiplier', ratio)) + '</span></div>' +
          '<div class="model-info">' + escapeHtml(m.description || '') + '</div>' +
          '</div>';
      }).join('') || '<p class="empty-state">' + escapeHtml(t('detail.noModels')) + '</p>';
    } else {
      c.innerHTML = '<p class="message message-error">' + escapeHtml(t('detail.loadFailed')) + ': ' + escapeHtml(d.error || '') + '</p>';
      toast(t('detail.loadFailed') + (d.error ? ': ' + d.error : ''), 'error');
    }
  } catch (e) {
    c.innerHTML = '<p class="message message-error">' + escapeHtml(t('detail.loadFailed')) + '</p>';
    toast(t('detail.loadFailed'), 'error');
  }
}
export async function generateMachineId() {
  try {
    const res = await api('/generate-machine-id');
    const d = await res.json();
    if (d.machineId) $('machineIdInput').value = d.machineId;
  } catch (e) {
    toast(t('detail.generateFailed'), 'error');
  }
}
export async function putAccount(id, body, successMsg) {
  try {
    const res = await api('/accounts/' + id, { method: 'PUT', body: JSON.stringify(body) });
    const d = await res.json();
    if (d.success) {
      toast(successMsg, 'success');
      loadAccounts();
    } else {
      toast(t('detail.saveFailed') + (d.error ? ': ' + d.error : ''), 'error');
    }
  } catch (e) {
    toast(t('detail.saveFailed'), 'error');
  }
}
export async function saveMachineId(id) {
  const m = $('machineIdInput').value.trim();
  if (m && !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(m) && !/^[0-9a-f]{32}$/i.test(m)) {
    toast(t('detail.machineIdError'), 'warning'); return;
  }
  await putAccount(id, { machineId: m }, t('detail.saved'));
}
export async function saveWeight(id) {
  const weight = parseInt($('weightInput').value, 10) || 0;
  await putAccount(id, { weight }, t('detail.saved'));
}
export function renderOverageBadge(a) {
  const status = (a.overageStatus || '').toUpperCase();
  if (status === 'ENABLED') {
    return '<span class="badge badge-warning">' + escapeHtml(t('accounts.overageOn')) + '</span>';
  }
  if (status === 'DISABLED') {
    return '<span class="badge badge-muted">' + escapeHtml(t('accounts.overageOff')) + '</span>';
  }
  return '';
}
export function renderOverageBlock(a, idAttr) {
  const status = (a.overageStatus || '').toUpperCase();
  const capable = !a.overageCapability || a.overageCapability === 'OVERAGE_CAPABLE';
  const checked = status === 'ENABLED';
  const checkedAt = a.overageCheckedAt ? new Date(a.overageCheckedAt * 1000).toLocaleString() : '-';
  const statusText = status === 'ENABLED' ? t('detail.overageEnabled')
    : status === 'DISABLED' ? t('detail.overageDisabled')
    : t('detail.overageUnknown');
  const disabledAttr = capable ? '' : ' disabled';
  return '<div class="form-group flex items-center gap-2">' +
    '<label class="switch"><input type="checkbox" id="overageSwitchInput-' + idAttr + '" data-detail-action="toggleOverage" data-id="' + idAttr + '" ' + (checked ? 'checked' : '') + disabledAttr + ' /><span class="slider"></span></label>' +
    '<span id="overageSwitchLabel-' + idAttr + '">' + escapeHtml(statusText) + '</span>' +
    '</div>' +
    (capable ? '' : '<p class="help-block" style="color:#ef4444">' + escapeHtml(t('detail.overageNotCapable')) + '</p>') +
    '<div class="detail-grid">' +
    detailItem(t('detail.overageStatus'), status || '-') +
    detailItem(t('detail.overageCap'), a.overageCap ? '$' + Number(a.overageCap).toFixed(2) : '-') +
    detailItem(t('detail.overageRate'), a.overageRate ? '$' + Number(a.overageRate).toFixed(4) : '-') +
    detailItem(t('detail.overageCurrent'), a.currentOverages ? '$' + Number(a.currentOverages).toFixed(4) : '$0') +
    detailItem(t('detail.overageCheckedAt'), checkedAt) +
    '</div>';
}
export async function toggleOverageSwitch(id, inputEl) {
  const desired = inputEl.checked;
  const labelEl = $('overageSwitchLabel-' + id);
  const oldLabel = labelEl ? labelEl.textContent : '';
  inputEl.disabled = true;
  if (labelEl) labelEl.textContent = t('detail.overageSwitching');
  try {
    const res = await api('/accounts/' + encodeURIComponent(id) + '/overage', {
      method: 'POST',
      body: JSON.stringify({ enabled: desired }),
    });
    const d = await res.json().catch(() => ({}));
    if (!res.ok || d.success === false) {
      throw new Error(d.error || t('accounts.overageSwitchFailed'));
    }
    if (labelEl) {
      labelEl.textContent = d.overageStatus === 'ENABLED' ? t('detail.overageEnabled')
        : d.overageStatus === 'DISABLED' ? t('detail.overageDisabled')
        : t('detail.overageUnknown');
    }
    inputEl.checked = d.overageStatus === 'ENABLED';
    await loadAccounts();
  } catch (e) {
    inputEl.checked = !desired;
    if (labelEl) labelEl.textContent = oldLabel;
    toast(t('accounts.overageSwitchFailed') + ': ' + (e.message || e), 'warning');
  } finally {
    inputEl.disabled = false;
  }
}
export async function refreshAccountOverage(id) {
  try {
    const res = await api('/accounts/' + encodeURIComponent(id) + '/overage', { method: 'GET' });
    const d = await res.json().catch(() => ({}));
    if (!res.ok || d.success === false) {
      throw new Error(d.error || t('accounts.overageSwitchFailed'));
    }
    await loadAccounts();
    showDetail(id);
  } catch (e) {
    toast(t('accounts.overageSwitchFailed') + ': ' + (e.message || e), 'warning');
  }
}
export async function saveProxyURL(id) {
  const url = $('proxyURLInput').value.trim();
  if (url && !/^(socks5|socks5h|http|https):\/\//.test(url)) {
    toast(t('detail.proxyFormatError'), 'warning'); return;
  }
  await putAccount(id, { proxyURL: url }, t('detail.proxySaved'));
}
export function closeDetailModal() { closeDialog('detailModal'); }

// Test flow
export function getTestAccount(id) {
  return state.accountsData.find(a => a.id === id) || null;
}
export function getTestModelValue() {
  const choice = $('testModelChoice');
  return (choice && choice.value.trim()) || 'claude-sonnet-4';
}
export function renderTestLog() {
  const c = $('testModalLog');
  if (!c) return;
  if (!state.testLogs.length) {
    c.innerHTML = '<div class="test-log-empty">' + escapeHtml(t('accounts.testLog.empty')) + '</div>';
    return;
  }
  c.innerHTML = state.testLogs.map(log =>
    '<div class="test-log-line ' + escapeAttr(log.type || 'info') + '">' +
    '<span class="test-log-time">' + escapeHtml(log.time) + '</span>' +
    '<span class="test-log-message">' + escapeHtml(log.msg) + '</span>' +
    '</div>'
  ).join('');
  c.scrollTop = c.scrollHeight;
}
export function addTestLog(msg, type) {
  const time = new Date().toLocaleTimeString();
  state.testLogs.push({ time, msg, type });
  if (state.testLogs.length > 100) state.testLogs.shift();
  renderTestLog();
}
export function clearTestLog() {
  state.testLogs = [];
  renderTestLog();
}
export function renderTestModal() {
  const body = $('testBody');
  if (!body) return;
  const acc = getTestAccount(state.testModalAccountId);
  const idAttr = escapeAttr(state.testModalAccountId);
  const email = acc ? getDisplayEmail(acc.email, acc.id) : state.testModalAccountId;
  const proxy = acc ? (acc.proxyURL || t('accounts.testLog.globalProxy')) : '?';
  const statusText = state.testModalLoadingModels
    ? t('accounts.testModelsLoading')
    : state.testModalModelError
      ? t('accounts.testModelsFallback')
      : t('accounts.testModelsReady', state.testModalModels.length);
  const modelField = state.testModalLoadingModels
    ? '<div class="test-model-loading">' + escapeHtml(t('accounts.testModelsLoading')) + '</div>'
    : state.testModalModels.length
      ? '<select id="testModelChoice">' +
      state.testModalModels.map(m => '<option value="' + escapeAttr(m) + '">' + escapeHtml(m) + '</option>').join('') +
      '</select>'
      : '<input type="text" id="testModelChoice" placeholder="claude-sonnet-4" value="claude-sonnet-4" />';

  body.innerHTML =
    '<div class="test-modal-account">' +
    '<div class="test-modal-account-main">' +
    '<div class="test-modal-email">' + escapeHtml(email) + '</div>' +
    '<div class="test-modal-meta">' +
    '<span>' + escapeHtml(formatAuthMethod(acc && (acc.provider || acc.authMethod))) + '</span>' +
    '<span>' + escapeHtml(proxy) + '</span>' +
    '</div>' +
    '</div>' +
    '<span class="test-modal-status">' + escapeHtml(statusText) + '</span>' +
    '</div>' +
    '<div class="test-modal-grid">' +
    '<div class="form-group test-model-field">' +
    '<label for="testModelChoice">' + escapeHtml(t('accounts.selectModel')) + '</label>' +
    modelField +
    '</div>' +
    '<div class="test-log-card">' +
    '<div class="test-log-header">' +
    '<span class="test-log-title">' + escapeHtml(t('accounts.testLog.title')) + '</span>' +
    '<button class="btn btn-xs btn-outline test-log-clear" id="testLogClear" type="button">' + escapeHtml(t('accounts.testLog.clear')) + '</button>' +
    '</div>' +
    '<div class="test-log-content" id="testModalLog"></div>' +
    '</div>' +
    '</div>' +
    '<div class="modal-footer">' +
    '<button class="btn btn-secondary" id="testModalCancelBtn" type="button">' + escapeHtml(t('common.close')) + '</button>' +
    '<button class="btn btn-primary" id="testRunBtn" data-id="' + idAttr + '" type="button" ' + (state.testModalLoadingModels ? 'disabled' : '') + '>' + escapeHtml(t('accounts.test')) + '</button>' +
    '</div>';

  if (!state.testModalLoadingModels) enhanceCustomSelects(body);
  renderTestLog();
}
export async function testAccount(id) {
  state.testModalAccountId = id;
  state.testModalModels = [];
  state.testModalLoadingModels = true;
  state.testModalModelError = false;
  state.testModalRunning = false;
  state.testLogs = [];
  renderTestModal();
  openDialog('testModal');
  try {
    const res = await api('/accounts/' + id + '/models/cached');
    const d = await res.json();
    state.testModalModels = Array.isArray(d.models) ? d.models.slice().sort() : [];
  } catch (e) {
    state.testModalModelError = true;
  } finally {
    state.testModalLoadingModels = false;
    renderTestModal();
  }
}
export function closeTestModal() {
  closeAllCustomSelects();
  closeDialog('testModal');
}
export async function runTestAccount(id, model) {
  if (state.testModalRunning) return;
  state.testModalRunning = true;
  const modalBtn = $('testRunBtn');
  if (modalBtn) modalBtn.setAttribute('aria-busy', 'true');
  const acc = state.accountsData.find(a => a.id === id);
  const email = acc ? getDisplayEmail(acc.email, acc.id) : id;
  const proxy = acc ? (acc.proxyURL || t('accounts.testLog.globalProxy')) : '?';
  addTestLog(t('accounts.testLog.start', email, model, proxy), 'info');
  try {
    const startTime = Date.now();
    const res = await api('/accounts/' + id + '/test', { method: 'POST', body: JSON.stringify({ model }) });
    const elapsed = ((Date.now() - startTime) / 1000).toFixed(1);
    const d = await res.json();
    if (d.success) {
      addTestLog(t('accounts.testLog.success', email, elapsed, d.reply), 'ok');
    } else {
      addTestLog(t('accounts.testLog.failed', email, elapsed, d.error || t('common.unknownError')), 'err');
    }
  } catch (e) {
    addTestLog(t('accounts.testLog.error', email, e.message), 'err');
  }
  state.testModalRunning = false;
  if (modalBtn) modalBtn.removeAttribute('aria-busy');
}
