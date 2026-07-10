// auth-modals.js — the add-account picker and every provider sign-in flow.
//
// Covers the method picker (showModal/methodCard/modalAdd + PROVIDER_METHODS/
// METHOD_CARDS/ALL_METHODS), each provider modal + its OAuth start/poll/complete/
// cancel flow (kiroSso, antigravity, grok, builderId, iam), the credential/cookie/
// sso-token/local imports, and the export-accounts modal.
//
// Imports loadStats from ../app.js at runtime (circular but safe: only called
// inside handlers, never at module top level).

import { state } from './state.js';
import {
  $, qsa, escapeHtml, escapeAttr, copyText, getDisplayEmail,
  openDialog, closeDialog, t, toast, toastPrimary, toastWarning, toastError,
  enhanceCustomSelects, api,
} from './core.js';
import { formatAuthMethod, formatSubscriptionLabel, loadAccounts } from './accounts.js';
import { loadStats } from '../app.js';

export var METHOD_ICONS = {
  builderid: 'fa-solid fa-id-card',
  iam: 'fa-solid fa-key',
  sso: 'fa-solid fa-shield-halved',
  local: 'fa-solid fa-folder-open',
  credentials: 'fa-solid fa-code',
  cookie: 'fa-solid fa-cookie-bite',
  enterprisesso: 'fa-brands fa-microsoft',
  grok: 'fa-solid fa-robot',
  antigravity: 'fa-brands fa-google',
  codex: 'fa-solid fa-code'
};
export function methodCard(type, title, desc) {
  var icon = METHOD_ICONS[type] || 'fa-solid fa-circle-plus';
  return '<button type="button" class="method-card" data-method="' + escapeAttr(type) + '">' +
    '<span class="method-icon"><i class="' + icon + '" aria-hidden="true"></i></span>' +
    '<span class="method-body">' +
    '<span class="method-title">' + escapeHtml(title) + '</span>' +
    '<span class="method-desc">' + escapeHtml(desc) + '</span>' +
    '</span>' +
    '<span class="method-arrow" aria-hidden="true"><i class="fa-solid fa-chevron-right"></i></span>' +
    '</button>';
}
export function showModal(type) {
  const modal = $('addModal');
  const title = $('modalTitle');
  const body = $('modalBody');
  if (type === 'add') modalAdd(title, body);
  else if (type === 'builderid') modalBuilderId(title, body);
  else if (type === 'iam') modalIam(title, body);
  else if (type === 'sso') modalSso(title, body);
  else if (type === 'local') modalLocal(title, body);
  else if (type === 'credentials') modalCredentials(title, body);
  else if (type === 'cookie') modalCookie(title, body);
  else if (type === 'enterprisesso') modalEnterpriseSso(title, body);
  else if (type === 'antigravity') modalAntigravity(title, body);
  else if (type === 'grok') modalGrok(title, body);
  else if (type === 'codex') modalCodex(title, body);
  if (!modal.classList.contains('active')) openDialog('addModal');
  enhanceCustomSelects(body);
}
export function closeModal() {
  closeDialog('addModal');
  state.iamSession = '';
  if (state.kiroSsoPollTimer) { clearTimeout(state.kiroSsoPollTimer); state.kiroSsoPollTimer = null; }
  // Best-effort cancel to free the loopback callback port immediately if the
  // operator closes the modal mid-login. If polling already completed, the
  // poller clears state.kiroSsoSession first, so this no-ops then.
  if (state.kiroSsoSession) {
    api('/auth/kiro-sso/cancel', { method: 'POST', body: JSON.stringify({ sessionId: state.kiroSsoSession }) }).catch(() => {});
  }
  state.kiroSsoSession = '';
  if (state.antigravityPollTimer) { clearTimeout(state.antigravityPollTimer); state.antigravityPollTimer = null; }
  if (state.antigravitySession) {
    api('/auth/antigravity/cancel', { method: 'POST', body: JSON.stringify({ sessionId: state.antigravitySession }) }).catch(() => {});
  }
  state.antigravitySession = '';
  if (state.builderIdPollTimer) { clearTimeout(state.builderIdPollTimer); state.builderIdPollTimer = null; }
  state.builderIdSession = '';
  // Grok add flow is direct (API key); no need to cancel server session for now
  if (state.grokPollTimer) { clearTimeout(state.grokPollTimer); state.grokPollTimer = null; }
  state.grokSession = '';
  if (state.codexPollTimer) { clearTimeout(state.codexPollTimer); state.codexPollTimer = null; }
  if (state.codexSession) {
    api('/auth/codex/cancel', { method: 'POST', body: JSON.stringify({ sessionId: state.codexSession }) }).catch(() => {});
  }
  state.codexSession = '';
}
// Which sign-in method cards belong to each provider bucket. When the accounts
// view is filtered to one provider, the Add dialog shows only that provider's
// methods (see modalAdd + the addAccountBtn handler).
export const PROVIDER_METHODS = {
  kiro: ['builderid', 'iam', 'sso', 'local', 'credentials', 'cookie', 'enterprisesso'],
  antigravity: ['antigravity'],
  grok: ['grok'],
  codex: ['codex']
};
export const METHOD_CARDS = {
  builderid: () => methodCard('builderid', t('modal.builderIdTitle'), t('modal.builderIdDesc')),
  iam: () => methodCard('iam', t('modal.iamTitle'), t('modal.iamDesc')),
  sso: () => methodCard('sso', t('modal.ssoTitle'), t('modal.ssoDesc')),
  local: () => methodCard('local', t('modal.localTitle'), t('modal.localDesc')),
  credentials: () => methodCard('credentials', t('modal.credentialsTitle'), t('modal.credentialsDesc')),
  cookie: () => methodCard('cookie', t('modal.cookieTitle'), t('modal.cookieDesc')),
  enterprisesso: () => methodCard('enterprisesso', t('modal.enterpriseSsoTitle'), t('modal.enterpriseSsoDesc')),
  antigravity: () => methodCard('antigravity', t('modal.antigravityTitle'), t('modal.antigravityDesc')),
  grok: () => methodCard('grok', t('modal.grokTitle') || 'Grok / xAI', t('modal.grokDesc') || 'Add xAI Grok account using API key (recommended) or OAuth'),
  codex: () => methodCard('codex', t('modal.codexTitle') || 'OpenAI Codex', t('modal.codexDesc') || 'Add a ChatGPT account via OAuth or by importing a token')
};
// Provider order used by "All Accounts": kiro methods first, then antigravity, grok, codex.
export const ALL_METHODS = ['builderid', 'iam', 'sso', 'local', 'credentials', 'cookie', 'enterprisesso', 'antigravity', 'grok', 'codex'];

export function modalAdd(title, body) {
  title.textContent = t('modal.addAccount');
  // Restrict the offered methods to the provider currently being viewed, if any.
  const methods = (state.currentProviderFilter && PROVIDER_METHODS[state.currentProviderFilter]) || ALL_METHODS;
  const cards = methods.map(m => (METHOD_CARDS[m] ? METHOD_CARDS[m]() : '')).join('');
  body.innerHTML =
    '<div class="method-list">' + cards + '</div>' +
    '<div class="modal-footer"><button class="btn btn-secondary" data-close-add="1" type="button">' + escapeHtml(t('common.cancel')) + '</button></div>';
}
export function modalBuilderId(title, body) {
  title.textContent = t('modal.builderIdTitle');
  body.innerHTML =
    '<p class="help-block">' + escapeHtml(t('modal.builderIdDesc')) + '</p>' +
    '<div id="builderIdStep1">' +
    '<div class="form-group"><label>' + escapeHtml(t('detail.region')) + '</label><input type="text" id="builderIdRegion" value="us-east-1" /></div>' +
    '<div class="modal-footer">' +
    '<button class="btn btn-secondary" data-modal-goto="add" type="button">' + escapeHtml(t('common.back')) + '</button>' +
    '<button class="btn btn-primary" id="startBuilderIdBtn" type="button">' + escapeHtml(t('builderid.startLogin')) + '</button>' +
    '</div>' +
    '</div>' +
    '<div id="builderIdStep2" class="hidden">' +
    '<div class="message message-info message-center"><p class="builder-code" id="builderIdUserCode"></p><p class="text-xs mt-2">' + escapeHtml(t('builderid.verifyCode')) + '</p></div>' +
    '<div class="form-group mt-4"><label>' + escapeHtml(t('builderid.verifyUrl')) + '</label>' +
    '<div class="endpoint"><span id="builderIdVerifyUrl" class="font-mono text-xs"></span></div>' +
    '<div class="flex gap-2 mt-2">' +
    '<button class="btn btn-sm btn-outline flex-1" id="builderIdOpenBtn" type="button">' + escapeHtml(t('builderid.open')) + '</button>' +
    '<button class="btn btn-sm btn-outline flex-1" id="builderIdCopyBtn" type="button">' + escapeHtml(t('common.copy')) + '</button>' +
    '</div>' +
    '</div>' +
    '<p id="builderIdStatus" class="text-center text-sm mt-4 muted-text">' + escapeHtml(t('builderid.waiting')) + '</p>' +
    '<div class="modal-footer"><button class="btn btn-secondary" id="builderIdCancelBtn" type="button">' + escapeHtml(t('common.cancel')) + '</button></div>' +
    '</div>';
  $('startBuilderIdBtn').addEventListener('click', startBuilderIdLogin);
}
export function modalIam(title, body) {
  title.textContent = t('modal.iamTitle');
  body.innerHTML =
    '<p class="help-block">' + escapeHtml(t('modal.iamDesc')) + '</p>' +
    '<div class="form-group"><label>' + escapeHtml(t('iam.startUrl')) + '</label><input type="text" id="iamStartUrl" placeholder="https://xxx.awsapps.com/start" /></div>' +
    '<div class="form-group"><label>' + escapeHtml(t('detail.region')) + '</label><input type="text" id="iamRegion" value="us-east-1" /></div>' +
    '<div id="iamStep2" class="hidden">' +
    '<div class="form-group"><label>' + escapeHtml(t('iam.loginUrl')) + '</label>' +
    '<div class="endpoint"><span id="iamAuthUrl" class="font-mono text-xs"></span></div>' +
    '<div class="flex gap-2 mt-2">' +
    '<button class="btn btn-sm btn-outline flex-1" id="iamOpenBtn" type="button">' + escapeHtml(t('builderid.open')) + '</button>' +
    '<button class="btn btn-sm btn-outline flex-1" id="iamCopyBtn" type="button">' + escapeHtml(t('common.copy')) + '</button>' +
    '</div>' +
    '</div>' +
    '<p class="text-sm mt-3 success-text">' + escapeHtml(t('iam.completeLogin')) + '</p>' +
    '<div class="form-group"><label>' + escapeHtml(t('iam.callbackUrl')) + '</label><input type="text" id="iamCallback" placeholder="http://127.0.0.1:xxx/?code=..." /></div>' +
    '</div>' +
    '<div class="modal-footer">' +
    '<button class="btn btn-secondary" data-modal-goto="add" type="button">' + escapeHtml(t('common.back')) + '</button>' +
    '<button class="btn btn-primary" id="iamBtn" type="button">' + escapeHtml(t('builderid.startLogin')) + '</button>' +
    '</div>';
  $('iamBtn').addEventListener('click', startIamSso);
}
export function modalEnterpriseSso(title, body) {
  title.textContent = t('modal.enterpriseSsoTitle');
  body.innerHTML =
    '<p class="help-block">' + escapeHtml(t('modal.enterpriseSsoDesc')) + '</p>' +
    '<div id="kiroSsoStep1">' +
    '<div class="message message-info"><p class="text-xs">' + escapeHtml(t('kirosso.hostNote')) + '</p></div>' +
    '<div class="modal-footer">' +
    '<button class="btn btn-secondary" data-modal-goto="add" type="button">' + escapeHtml(t('common.back')) + '</button>' +
    '<button class="btn btn-primary" id="startKiroSsoBtn" type="button">' + escapeHtml(t('builderid.startLogin')) + '</button>' +
    '</div>' +
    '</div>' +
    '<div id="kiroSsoStep2" class="hidden">' +
    '<div class="message message-info"><p class="text-xs">' + escapeHtml(t('kirosso.openInstruction')) + '</p></div>' +
    '<div class="form-group mt-3"><label>' + escapeHtml(t('iam.loginUrl')) + '</label>' +
    '<div class="endpoint"><span id="kiroSsoSignInUrl" class="font-mono text-xs"></span></div>' +
    '<div class="flex gap-2 mt-2">' +
    '<button class="btn btn-sm btn-outline flex-1" id="kiroSsoOpenBtn" type="button">' + escapeHtml(t('builderid.open')) + '</button>' +
    '<button class="btn btn-sm btn-outline flex-1" id="kiroSsoCopyBtn" type="button">' + escapeHtml(t('common.copy')) + '</button>' +
    '</div>' +
    '<div class="form-group mt-3"><label>' + escapeHtml(t('kirosso.callbackUrl')) + '</label>' +
    '<input type="text" id="kiroSsoCallback" placeholder="http://localhost:3128/signin/callback?..." />' +
    '<p class="help-block text-xs mt-1">' + escapeHtml(t('kirosso.callbackHint')) + '</p></div>' +
    '</div>' +
    '<p id="kiroSsoStatus" class="text-center text-sm mt-4 muted-text">' + escapeHtml(t('builderid.waiting')) + '</p>' +
    '<div class="modal-footer">' +
    '<button class="btn btn-secondary" id="kiroSsoCancelBtn" type="button">' + escapeHtml(t('common.cancel')) + '</button>' +
    '<button class="btn btn-primary" id="kiroSsoCompleteBtn" type="button">' + escapeHtml(t('iam.complete')) + '</button>' +
    '</div>' +
    '</div>';
  $('startKiroSsoBtn').addEventListener('click', startKiroSsoLogin);
}
export async function startKiroSsoLogin() {
  const res = await api('/auth/kiro-sso/start', { method: 'POST', body: JSON.stringify({}) });
  const d = await res.json();
  if (d.sessionId && d.signInUrl) {
    state.kiroSsoSession = d.sessionId;
    $('kiroSsoSignInUrl').textContent = d.signInUrl;
    $('kiroSsoStep1').classList.add('hidden');
    $('kiroSsoStep2').classList.remove('hidden');
    $('kiroSsoOpenBtn').addEventListener('click', () => window.open($('kiroSsoSignInUrl').textContent, '_blank'));
    $('kiroSsoCopyBtn').addEventListener('click', async () => {
      await copyText($('kiroSsoSignInUrl').textContent);
      toast(t('common.copied'), 'primary');
    });
    $('kiroSsoCancelBtn').addEventListener('click', cancelKiroSsoLogin);
    $('kiroSsoCompleteBtn').addEventListener('click', completeKiroSsoManual);
    // Open the sign-in tab immediately (works when the admin panel is viewed on the proxy host).
    window.open(d.signInUrl, '_blank');
    pollKiroSso(d.interval || 2);
  } else toastError(t('common.failed') + ': ' + (d.error || ''));
}
// completeKiroSsoManual finishes a sign-in from a pasted callback URL — the
// fallback when the admin panel is opened from a different host than the proxy
// (the loopback localhost:3128 redirect can't reach the server). The M365 flow
// has two legs: the first paste returns a redirect URL (Microsoft login) that we
// open for the operator to sign in and paste the second callback URL.
export async function completeKiroSsoManual() {
  if (!state.kiroSsoSession) { toastError(t('common.failed')); return; }
  const callbackUrl = ($('kiroSsoCallback').value || '').trim();
  if (!callbackUrl) { toastError(t('common.failed') + ': ' + t('kirosso.callbackUrl')); return; }
  // Stop polling the loopback listener; we are completing manually.
  if (state.kiroSsoPollTimer) { clearTimeout(state.kiroSsoPollTimer); state.kiroSsoPollTimer = null; }
  const res = await api('/auth/kiro-sso/complete', { method: 'POST', body: JSON.stringify({ sessionId: state.kiroSsoSession, callbackUrl }) });
  const d = await res.json();
  if (d.completed) {
    state.kiroSsoSession = '';
    closeModal(); loadAccounts(); loadStats();
    toastPrimary(t('builderid.success') + ': ' + (d.account?.email || d.account?.id));
    autoRefreshNewAccount(d.account?.id);
  } else if (d.status === 'redirect' && d.redirectUrl) {
    // Leg 1 done: open the Microsoft login URL and prompt for the next callback.
    $('kiroSsoSignInUrl').textContent = d.redirectUrl;
    $('kiroSsoCallback').value = '';
    $('kiroSsoStatus').textContent = t('kirosso.pasteSecond');
    window.open(d.redirectUrl, '_blank');
  } else {
    toastError(t('common.failed') + ': ' + (d.error || ''));
  }
}
export function pollKiroSso(interval) {
  state.kiroSsoPollTimer = setTimeout(async () => {
    const res = await api('/auth/kiro-sso/poll', { method: 'POST', body: JSON.stringify({ sessionId: state.kiroSsoSession }) });
    const d = await res.json();
    if (d.completed) {
      // Session is already consumed server-side; clear it so closeModal() does
      // not fire a redundant cancel for an account that succeeded.
      state.kiroSsoSession = '';
      closeModal(); loadAccounts(); loadStats();
      toastPrimary(t('builderid.success') + ': ' + (d.account?.email || d.account?.id));
      autoRefreshNewAccount(d.account?.id);
    } else if (d.success && !d.completed) {
      $('kiroSsoStatus').textContent = t('builderid.waiting');
      pollKiroSso(interval);
    } else {
      toastError(t('common.failed') + ': ' + (d.error || ''));
      cancelKiroSsoLogin();
    }
  }, interval * 1000);
}
export function cancelKiroSsoLogin() {
  if (state.kiroSsoPollTimer) { clearTimeout(state.kiroSsoPollTimer); state.kiroSsoPollTimer = null; }
  // Tell the backend to release the loopback callback port now instead of waiting
  // for the deadline (fire-and-forget; ignore the result).
  if (state.kiroSsoSession) {
    api('/auth/kiro-sso/cancel', { method: 'POST', body: JSON.stringify({ sessionId: state.kiroSsoSession }) }).catch(() => {});
  }
  state.kiroSsoSession = '';
  showModal('add');
}
export function modalAntigravity(title, body) {
  title.textContent = t('modal.antigravityTitle');
  body.innerHTML =
    '<p class="help-block">' + escapeHtml(t('modal.antigravityDesc')) + '</p>' +
    '<div id="antigravityStep1">' +
    '<div class="message message-info"><p class="text-xs">' + escapeHtml(t('kirosso.hostNote')) + '</p></div>' +
    '<div class="modal-footer">' +
    '<button class="btn btn-secondary" data-modal-goto="add" type="button">' + escapeHtml(t('common.back')) + '</button>' +
    '<button class="btn btn-primary" id="startAntigravityBtn" type="button">' + escapeHtml(t('builderid.startLogin')) + '</button>' +
    '</div>' +
    '</div>' +
    '<div id="antigravityStep2" class="hidden">' +
    '<div class="message message-info"><p class="text-xs">' + escapeHtml(t('kirosso.openInstruction')) + '</p></div>' +
    '<div class="form-group mt-3"><label>' + escapeHtml(t('iam.loginUrl')) + '</label>' +
    '<div class="endpoint"><span id="antigravitySignInUrl" class="font-mono text-xs"></span></div>' +
    '<div class="flex gap-2 mt-2">' +
    '<button class="btn btn-sm btn-outline flex-1" id="antigravityOpenBtn" type="button">' + escapeHtml(t('builderid.open')) + '</button>' +
    '<button class="btn btn-sm btn-outline flex-1" id="antigravityCopyBtn" type="button">' + escapeHtml(t('common.copy')) + '</button>' +
    '</div>' +
    '</div>' +
    '<p id="antigravityStatus" class="text-center text-sm mt-4 muted-text">' + escapeHtml(t('builderid.waiting')) + '</p>' +
    '<div class="form-group mt-3"><label>' + escapeHtml(t('antigravity.callbackUrl')) + '</label>' +
    '<input type="text" id="antigravityCallback" placeholder="http://localhost:3129/callback?code=..." />' +
    '<p class="help-block text-xs mt-1">' + escapeHtml(t('antigravity.callbackHint')) + '</p></div>' +
    '<div class="modal-footer">' +
    '<button class="btn btn-secondary" id="antigravityCancelBtn" type="button">' + escapeHtml(t('common.cancel')) + '</button>' +
    '<button class="btn btn-primary" id="antigravityCompleteBtn" type="button">' + escapeHtml(t('iam.complete')) + '</button>' +
    '</div>' +
    '</div>';
  $('startAntigravityBtn').addEventListener('click', startAntigravityLogin);
}
export async function startAntigravityLogin() {
  const res = await api('/auth/antigravity/start', { method: 'POST', body: JSON.stringify({}) });
  const d = await res.json();
  if (d.sessionId && d.signInUrl) {
    state.antigravitySession = d.sessionId;
    $('antigravitySignInUrl').textContent = d.signInUrl;
    $('antigravityStep1').classList.add('hidden');
    $('antigravityStep2').classList.remove('hidden');
    $('antigravityOpenBtn').addEventListener('click', () => window.open($('antigravitySignInUrl').textContent, '_blank'));
    $('antigravityCopyBtn').addEventListener('click', async () => {
      await copyText($('antigravitySignInUrl').textContent);
      toast(t('common.copied'), 'primary');
    });
    $('antigravityCancelBtn').addEventListener('click', cancelAntigravityLogin);
    $('antigravityCompleteBtn').addEventListener('click', completeAntigravityManual);
    window.open(d.signInUrl, '_blank');
    pollAntigravity(d.interval || 2);
  } else toastError(t('common.failed') + ': ' + (d.error || ''));
}
export async function completeAntigravityManual() {
  const callbackUrl = ($('antigravityCallback').value || '').trim();
  if (!callbackUrl) { toastError(t('common.failed') + ': ' + t('antigravity.callbackUrl')); return; }
  // Stop polling the loopback listener; we are completing manually.
  if (state.antigravityPollTimer) { clearTimeout(state.antigravityPollTimer); state.antigravityPollTimer = null; }
  $('antigravityStatus').textContent = t('builderid.waiting');
  const res = await api('/auth/antigravity/complete', { method: 'POST', body: JSON.stringify({ callbackUrl }) });
  const d = await res.json();
  if (d.completed) {
    state.antigravitySession = '';
    closeModal(); loadAccounts(); loadStats();
    toastPrimary(t('builderid.success') + ': ' + (d.account?.email || d.account?.id));
    autoRefreshNewAccount(d.account?.id);
  } else {
    toastError(t('common.failed') + ': ' + (d.error || ''));
    // Keep the session/poll alive so the operator can retry.
    pollAntigravity(2);
  }
}
export function pollAntigravity(interval) {
  state.antigravityPollTimer = setTimeout(async () => {
    const res = await api('/auth/antigravity/poll', { method: 'POST', body: JSON.stringify({ sessionId: state.antigravitySession }) });
    const d = await res.json();
    if (d.completed) {
      state.antigravitySession = '';
      closeModal(); loadAccounts(); loadStats();
      toastPrimary(t('builderid.success') + ': ' + (d.account?.email || d.account?.id));
      autoRefreshNewAccount(d.account?.id);
    } else if (d.success && !d.completed) {
      $('antigravityStatus').textContent = t('builderid.waiting');
      pollAntigravity(interval);
    } else {
      toastError(t('common.failed') + ': ' + (d.error || ''));
      cancelAntigravityLogin();
    }
  }, interval * 1000);
}
export function cancelAntigravityLogin() {
  if (state.antigravityPollTimer) { clearTimeout(state.antigravityPollTimer); state.antigravityPollTimer = null; }
  if (state.antigravitySession) {
    api('/auth/antigravity/cancel', { method: 'POST', body: JSON.stringify({ sessionId: state.antigravitySession }) }).catch(() => {});
  }
  state.antigravitySession = '';
  showModal('add');
}
export function modalSso(title, body) {
  title.textContent = t('modal.ssoTitle');
  body.innerHTML =
    '<div class="help-block">' +
    '<b>' + escapeHtml(t('sso.howToGet')) + '</b>' +
    '<ol class="steps-list">' +
    '<li>' + escapeHtml(t('sso.step1')) + ' <code class="code-inline">view.awsapps.com/start</code></li>' +
    '<li>' + escapeHtml(t('sso.step2')) + '</li>' +
    '<li>' + escapeHtml(t('sso.step3')) + ' <code class="code-inline">x-amz-sso_authn</code></li>' +
    '</ol>' +
    '</div>' +
    '<div class="form-group"><label>' + escapeHtml(t('sso.tokenLabel')) + ' <small>' + escapeHtml(t('sso.tokenHint')) + '</small></label>' +
    '<textarea id="ssoToken" placeholder="' + escapeAttr(t('sso.tokenPlaceholder')) + '"></textarea></div>' +
    '<div class="form-group"><label>' + escapeHtml(t('detail.region')) + '</label><input type="text" id="ssoRegion" value="us-east-1" /></div>' +
    '<div class="modal-footer">' +
    '<button class="btn btn-secondary" data-modal-goto="add" type="button">' + escapeHtml(t('common.back')) + '</button>' +
    '<button class="btn btn-primary" id="importSsoBtn" type="button">' + escapeHtml(t('common.add')) + '</button>' +
    '</div>';
  $('importSsoBtn').addEventListener('click', importSsoToken);
}

export function modalLocal(title, body) {
  title.textContent = t('modal.localTitle');
  body.innerHTML =
    '<p class="help-block">' + escapeHtml(t('modal.localDesc')) + '</p>' +
    '<div class="help-block">' +
    '<p><b>' + escapeHtml(t('local.fileLocation')) + '</b></p>' +
    '<p>' + escapeHtml(t('local.windows')) + ': <code class="code-inline">%USERPROFILE%\\.aws\\sso\\cache\\</code></p>' +
    '<p>' + escapeHtml(t('local.macosLinux')) + ': <code class="code-inline">~/.aws/sso/cache/</code></p>' +
    '</div>' +
    '<div class="form-group"><label>' + escapeHtml(t('local.loginChannel')) + '</label>' +
    '<select id="localProvider">' +
    '<option value="BuilderId">' + escapeHtml(t('local.providerBuilderId')) + '</option>' +
    '<option value="Enterprise">' + escapeHtml(t('local.providerEnterprise')) + '</option>' +
    '<option value="Google">' + escapeHtml(t('local.providerGoogle')) + '</option>' +
    '<option value="Github">' + escapeHtml(t('local.providerGithub')) + '</option>' +
    '</select>' +
    '</div>' +
    '<div class="form-group">' +
    '<label>' + escapeHtml(t('local.tokenFile')) + ' <small>' + escapeHtml(t('local.tokenRequired')) + '</small></label>' +
    '<div class="input-row">' +
    '<textarea id="localTokenJson" placeholder="' + escapeAttr(t('local.pasteOrUpload')) + '" class="font-mono"></textarea>' +
    '<label class="btn btn-outline btn-sm">' + escapeHtml(t('local.upload')) +
    '<input type="file" accept=".json" id="localTokenFile" class="file-input-hidden" />' +
    '</label>' +
    '</div>' +
    '</div>' +
    '<div id="localClientGroup" class="form-group">' +
    '<label>' + escapeHtml(t('local.clientFile')) + ' <small>' + escapeHtml(t('local.clientRequired')) + '</small></label>' +
    '<div class="input-row">' +
    '<textarea id="localClientJson" placeholder="' + escapeAttr(t('local.pasteOrUpload')) + '" class="font-mono"></textarea>' +
    '<label class="btn btn-outline btn-sm">' + escapeHtml(t('local.upload')) +
    '<input type="file" accept=".json" id="localClientFile" class="file-input-hidden" />' +
    '</label>' +
    '</div>' +
    '</div>' +
    '<div class="modal-footer">' +
    '<button class="btn btn-secondary" data-modal-goto="add" type="button">' + escapeHtml(t('common.back')) + '</button>' +
    '<button class="btn btn-primary" id="importLocalBtn" type="button">' + escapeHtml(t('common.add')) + '</button>' +
    '</div>';
  $('localProvider').addEventListener('change', updateLocalFields);
  $('localTokenFile').addEventListener('change', e => loadLocalFile(e.target, 'localTokenJson'));
  $('localClientFile').addEventListener('change', e => loadLocalFile(e.target, 'localClientJson'));
  $('importLocalBtn').addEventListener('click', importLocalKiro);
}
export function modalCredentials(title, body) {
  title.textContent = t('modal.credentialsTitle');
  body.innerHTML =
    '<p class="help-block">' + escapeHtml(t('modal.credentialsDesc')) + '</p>' +
    '<p class="help-block">' + escapeHtml(t('credentials.batchHint')) + '</p>' +
    '<div class="form-group"><label>' + escapeHtml(t('credentials.label')) + '</label>' +
    '<textarea id="credJson" class="font-mono" placeholder=\'[{"refreshToken":"xxx","provider":"BuilderID"}]&#10;or&#10;email----state.password----refreshToken----clientId----clientSecret\'></textarea>' +
    '</div>' +
    '<div class="modal-footer">' +
    '<button class="btn btn-secondary" data-modal-goto="add" type="button">' + escapeHtml(t('common.back')) + '</button>' +
    '<button class="btn btn-primary" id="importCredBtn" type="button">' + escapeHtml(t('common.add')) + '</button>' +
    '</div>';
  $('importCredBtn').addEventListener('click', importCredentials);
}
export function modalCookie(title, body) {
  title.textContent = t('modal.cookieTitle');
  body.innerHTML =
    '<div class="help-block">' +
    '<p><b>' + escapeHtml(t('cookie.howToGet')) + '</b></p>' +
    '<ol class="steps-list">' +
    '<li>' + escapeHtml(t('cookie.step1')) + ' <a href="' + escapeAttr(t('cookie.link')) + '" target="_blank">' + escapeHtml(t('cookie.link')) + '</a></li>' +
    '<li>' + escapeHtml(t('cookie.step2')) + '</li>' +
    '<li>' + escapeHtml(t('cookie.step3')) + '</li>' +
    '</ol>' +
    '</div>' +
    '<div class="form-group"><label>' + escapeHtml(t('cookie.provider')) + '</label>' +
    '<select id="cookieProvider">' +
    '<option value="Google">' + escapeHtml(t('cookie.google')) + '</option>' +
    '<option value="Github">' + escapeHtml(t('cookie.github')) + '</option>' +
    '</select>' +
    '</div>' +
    '<div class="form-group"><label>' + escapeHtml(t('cookie.refreshToken')) + '</label>' +
    '<textarea id="cookieRefreshToken" class="font-mono" placeholder="' + escapeAttr(t('cookie.refreshTokenPlaceholder')) + '"></textarea>' +
    '</div>' +
    '<div class="modal-footer">' +
    '<button class="btn btn-secondary" data-modal-goto="add" type="button">' + escapeHtml(t('common.back')) + '</button>' +
    '<button class="btn btn-primary" id="importCookieBtn" type="button">' + escapeHtml(t('common.add')) + '</button>' +
    '</div>';
  $('importCookieBtn').addEventListener('click', importFromCookie);
}

// ==================== Grok / xAI account modal ====================
// Two sign-in modes (mirrors 9router): Grok Build OAuth (PKCE loopback,
// recommended) and xAI API Key. Backend fully implemented in auth/xai.go + handler.
export function modalGrok(title, body) {
  title.textContent = t('modal.grokTitle') || 'Grok / xAI';
  body.innerHTML =
    '<p class="help-block">' + escapeHtml(t('modal.grokDesc') || 'Add an xAI Grok account. API Key mode is ready.') + '</p>' +
    '<div class="seg-tabs" id="grokModeTabs">' +
    '<button type="button" class="seg-tab active" data-grok-mode="oauth">' + escapeHtml(t('grok.modeOauth') || 'Grok Build OAuth') + '</button>' +
    '<button type="button" class="seg-tab" data-grok-mode="apikey">' + escapeHtml(t('grok.modeApiKey') || 'xAI API Key') + '</button>' +
    '</div>' +
    // ---- OAuth pane ----
    '<div id="grokOauthPane">' +
    '<div id="grokStep1">' +
    '<div class="message message-info"><p class="text-xs">' + escapeHtml(t('kirosso.hostNote')) + '</p></div>' +
    '<div class="modal-footer">' +
    '<button class="btn btn-secondary" data-modal-goto="add" type="button">' + escapeHtml(t('common.back')) + '</button>' +
    '<button class="btn btn-primary" id="startGrokBtn" type="button">' + escapeHtml(t('builderid.startLogin')) + '</button>' +
    '</div>' +
    '</div>' +
    '<div id="grokStep2" class="hidden">' +
    '<div class="message message-info"><p class="text-xs">' + escapeHtml(t('kirosso.openInstruction')) + '</p></div>' +
    '<div class="form-group mt-3"><label>' + escapeHtml(t('iam.loginUrl')) + '</label>' +
    '<div class="endpoint"><span id="grokSignInUrl" class="font-mono text-xs"></span></div>' +
    '<div class="flex gap-2 mt-2">' +
    '<button class="btn btn-sm btn-outline flex-1" id="grokOpenBtn" type="button">' + escapeHtml(t('builderid.open')) + '</button>' +
    '<button class="btn btn-sm btn-outline flex-1" id="grokCopyBtn" type="button">' + escapeHtml(t('common.copy')) + '</button>' +
    '</div>' +
    '</div>' +
    '<p id="grokStatus" class="text-center text-sm mt-4 muted-text">' + escapeHtml(t('builderid.waiting')) + '</p>' +
    '<div class="form-group mt-3"><label>' + escapeHtml(t('grok.callbackUrl') || t('antigravity.callbackUrl')) + '</label>' +
    '<input type="text" id="grokCallback" placeholder="http://127.0.0.1:56121/callback?code=..." />' +
    '<p class="help-block text-xs mt-1">' + escapeHtml(t('grok.callbackHint') || t('antigravity.callbackHint')) + '</p></div>' +
    '<div class="modal-footer">' +
    '<button class="btn btn-secondary" id="grokCancelBtn" type="button">' + escapeHtml(t('common.cancel')) + '</button>' +
    '<button class="btn btn-primary" id="grokCompleteBtn" type="button">' + escapeHtml(t('iam.complete')) + '</button>' +
    '</div>' +
    '</div>' +
    '</div>' +
    // ---- API key pane ----
    '<div id="grokApiPane" class="hidden">' +
    '<div class="form-group">' +
    '<label>' + escapeHtml(t('grok.apiKey') || 'xAI API Key') + '</label>' +
    '<input type="text" id="grokApiKey" class="font-mono" placeholder="xai-..." />' +
    '<p class="help-block text-xs mt-1">' + escapeHtml(t('grok.apiKeyHint') || 'Create key at console.x.ai. Stored securely.') + '</p>' +
    '</div>' +
    '<div class="form-group">' +
    '<label>' + escapeHtml(t('detail.weight') || 'Weight') + '</label>' +
    '<input type="number" id="grokWeight" value="1" min="1" style="width:100px" />' +
    '</div>' +
    '<div class="modal-footer">' +
    '<button class="btn btn-secondary" data-modal-goto="add" type="button">' + escapeHtml(t('common.back')) + '</button>' +
    '<button class="btn btn-primary" id="addGrokBtn" type="button">' + escapeHtml(t('common.add')) + '</button>' +
    '</div>' +
    '</div>';

  // Both Grok Build OAuth and xAI API Key are supported (mirrors 9router dual auth)
  // Default to OAuth (recommended)
  $('grokOauthPane').classList.remove('hidden');
  $('grokApiPane').classList.add('hidden');

  const oauthTab = qsa('#grokModeTabs .seg-tab[data-grok-mode="oauth"]')[0];
  const apiTab = qsa('#grokModeTabs .seg-tab[data-grok-mode="apikey"]')[0];
  if (oauthTab) oauthTab.classList.add('active');
  if (apiTab) apiTab.classList.remove('active');

  qsa('#grokModeTabs .seg-tab').forEach(tab => {
    tab.addEventListener('click', () => {
      const mode = tab.dataset.grokMode;
      qsa('#grokModeTabs .seg-tab').forEach(el => el.classList.toggle('active', el === tab));
      $('grokOauthPane').classList.toggle('hidden', mode !== 'oauth');
      $('grokApiPane').classList.toggle('hidden', mode !== 'apikey');
    });
    // No more dimming — both modes fully implemented on backend
  });

  $('startGrokBtn').addEventListener('click', startGrokLogin);
  $('addGrokBtn').addEventListener('click', importGrokAccount);
}

export async function startGrokLogin() {
  const res = await api('/auth/grok/start', { method: 'POST', body: JSON.stringify({}) });
  const d = await res.json();
  if (d.sessionId && d.signInUrl) {
    state.grokSession = d.sessionId;
    $('grokSignInUrl').textContent = d.signInUrl;
    $('grokStep1').classList.add('hidden');
    $('grokStep2').classList.remove('hidden');
    $('grokOpenBtn').addEventListener('click', () => window.open($('grokSignInUrl').textContent, '_blank'));
    $('grokCopyBtn').addEventListener('click', async () => {
      await copyText($('grokSignInUrl').textContent);
      toast(t('common.copied'), 'primary');
    });
    $('grokCancelBtn').addEventListener('click', cancelGrokLogin);
    $('grokCompleteBtn').addEventListener('click', completeGrokManual);
    window.open(d.signInUrl, '_blank');
    pollGrok(d.interval || 2);
  } else toastError(t('common.failed') + ': ' + (d.error || ''));
}
export async function completeGrokManual() {
  const callbackUrl = ($('grokCallback').value || '').trim();
  if (!callbackUrl) { toastError(t('common.failed') + ': ' + (t('grok.callbackUrl') || t('antigravity.callbackUrl'))); return; }
  if (state.grokPollTimer) { clearTimeout(state.grokPollTimer); state.grokPollTimer = null; }
  $('grokStatus').textContent = t('builderid.waiting');
  const res = await api('/auth/grok/complete', { method: 'POST', body: JSON.stringify({ sessionId: state.grokSession, callbackUrl }) });
  const d = await res.json();
  if (d.completed) {
    state.grokSession = '';
    closeModal(); loadAccounts(); loadStats();
    toastPrimary(t('builderid.success') + ': ' + (d.account?.email || d.account?.id));
    autoRefreshNewAccount(d.account?.id);
  } else {
    toastError(t('common.failed') + ': ' + (d.error || ''));
    pollGrok(2);
  }
}
export function pollGrok(interval) {
  state.grokPollTimer = setTimeout(async () => {
    const res = await api('/auth/grok/poll', { method: 'POST', body: JSON.stringify({ sessionId: state.grokSession }) });
    const d = await res.json();
    if (d.completed) {
      state.grokSession = '';
      closeModal(); loadAccounts(); loadStats();
      toastPrimary(t('builderid.success') + ': ' + (d.account?.email || d.account?.id));
      autoRefreshNewAccount(d.account?.id);
    } else if (d.success && !d.completed) {
      $('grokStatus').textContent = t('builderid.waiting');
      pollGrok(interval);
    } else {
      toastError(t('common.failed') + ': ' + (d.error || ''));
      cancelGrokLogin();
    }
  }, interval * 1000);
}
export function cancelGrokLogin() {
  if (state.grokPollTimer) { clearTimeout(state.grokPollTimer); state.grokPollTimer = null; }
  if (state.grokSession) {
    api('/auth/grok/cancel', { method: 'POST', body: JSON.stringify({ sessionId: state.grokSession }) }).catch(() => {});
  }
  state.grokSession = '';
  showModal('add');
}

export async function importGrokAccount() {
  const apiKey = ($('grokApiKey').value || '').trim();
  const weight = parseInt($('grokWeight').value || '1', 10) || 1;

  if (!apiKey) {
    return toastError((t('grok.apiKey') || 'xAI API Key') + ' is required');
  }

  const account = {
    id: '', // server will generate
    provider: 'grok',
    authMethod: 'grok',
    grokAuthType: 'apikey',
    grokApiKey: apiKey,
    weight: weight,
    enabled: true
  };

  try {
    const res = await api('/accounts', { method: 'POST', body: JSON.stringify(account) });
    if (!res.ok) {
      const err = await res.text();
      throw new Error(err || 'Failed to add Grok account');
    }
    closeModal();
    await loadAccounts();
    await loadStats();
    toastPrimary(t('builderid.success') || 'Account added');

    // Auto register Grok models for routing
    try {
      const res = await api('/accounts', { method: 'GET' });
      const data = await res.json();
      const newAcc = (data.accounts || []).find(x => x.provider === 'grok' || x.grokApiKey);
      if (newAcc && newAcc.id) {
        await api('/accounts/' + newAcc.id + '/models/refresh', { method: 'POST' }).catch(() => {});
      }
    } catch (_) {}
  } catch (e) {
    toastError('Failed to add Grok account: ' + (e.message || e));
  }
}

// ==================== Codex (OpenAI ChatGPT) ====================

export function modalCodex(title, body) {
  title.textContent = t('modal.codexTitle') || 'OpenAI Codex';
  body.innerHTML =
    '<p class="help-block">' + escapeHtml(t('modal.codexDesc') || 'Add a ChatGPT account via OAuth or by importing a token.') + '</p>' +
    '<div class="seg-tabs" id="codexModeTabs">' +
    '<button type="button" class="seg-tab active" data-codex-mode="oauth">' + escapeHtml(t('codex.modeOauth') || 'ChatGPT OAuth') + '</button>' +
    '<button type="button" class="seg-tab" data-codex-mode="import">' + escapeHtml(t('codex.modeImport') || 'Import Token') + '</button>' +
    '</div>' +
    // ---- OAuth pane ----
    '<div id="codexOauthPane">' +
    '<div id="codexStep1">' +
    '<div class="message message-info"><p class="text-xs">' + escapeHtml(t('kirosso.hostNote')) + '</p></div>' +
    '<div class="modal-footer">' +
    '<button class="btn btn-secondary" data-modal-goto="add" type="button">' + escapeHtml(t('common.back')) + '</button>' +
    '<button class="btn btn-primary" id="startCodexBtn" type="button">' + escapeHtml(t('builderid.startLogin')) + '</button>' +
    '</div>' +
    '</div>' +
    '<div id="codexStep2" class="hidden">' +
    '<div class="message message-info"><p class="text-xs">' + escapeHtml(t('kirosso.openInstruction')) + '</p></div>' +
    '<div class="form-group mt-3"><label>' + escapeHtml(t('iam.loginUrl')) + '</label>' +
    '<div class="endpoint"><span id="codexSignInUrl" class="font-mono text-xs"></span></div>' +
    '<div class="flex gap-2 mt-2">' +
    '<button class="btn btn-sm btn-outline flex-1" id="codexOpenBtn" type="button">' + escapeHtml(t('builderid.open')) + '</button>' +
    '<button class="btn btn-sm btn-outline flex-1" id="codexCopyBtn" type="button">' + escapeHtml(t('common.copy')) + '</button>' +
    '</div>' +
    '</div>' +
    '<p id="codexStatus" class="text-center text-sm mt-4 muted-text">' + escapeHtml(t('builderid.waiting')) + '</p>' +
    '<div class="form-group mt-3"><label>' + escapeHtml(t('codex.callbackUrl') || t('antigravity.callbackUrl')) + '</label>' +
    '<input type="text" id="codexCallback" placeholder="http://localhost:1455/auth/callback?code=..." />' +
    '<p class="help-block text-xs mt-1">' + escapeHtml(t('codex.callbackHint') || t('antigravity.callbackHint')) + '</p></div>' +
    '<div class="modal-footer">' +
    '<button class="btn btn-secondary" id="codexCancelBtn" type="button">' + escapeHtml(t('common.cancel')) + '</button>' +
    '<button class="btn btn-primary" id="codexCompleteBtn" type="button">' + escapeHtml(t('iam.complete')) + '</button>' +
    '</div>' +
    '</div>' +
    '</div>' +
    // ---- Import token pane ----
    '<div id="codexImportPane" class="hidden">' +
    '<div class="form-group">' +
    '<label>' + escapeHtml(t('codex.token') || 'Token / auth.json') + '</label>' +
    '<textarea id="codexToken" class="font-mono" rows="5" placeholder=\'{"accessToken":"...","refreshToken":"...","idToken":"..."} or a single ChatGPT access token\'></textarea>' +
    '<p class="help-block text-xs mt-1">' + escapeHtml(t('codex.tokenHint') || 'Paste a full auth.json (with refresh token) or a single ChatGPT access token.') + '</p>' +
    '</div>' +
    '<div class="form-group">' +
    '<label>' + escapeHtml(t('detail.weight') || 'Weight') + '</label>' +
    '<input type="number" id="codexWeight" value="1" min="1" style="width:100px" />' +
    '</div>' +
    '<div class="modal-footer">' +
    '<button class="btn btn-secondary" data-modal-goto="add" type="button">' + escapeHtml(t('common.back')) + '</button>' +
    '<button class="btn btn-primary" id="addCodexBtn" type="button">' + escapeHtml(t('common.add')) + '</button>' +
    '</div>' +
    '</div>';

  $('codexOauthPane').classList.remove('hidden');
  $('codexImportPane').classList.add('hidden');

  qsa('#codexModeTabs .seg-tab').forEach(tab => {
    tab.addEventListener('click', () => {
      const mode = tab.dataset.codexMode;
      qsa('#codexModeTabs .seg-tab').forEach(el => el.classList.toggle('active', el === tab));
      $('codexOauthPane').classList.toggle('hidden', mode !== 'oauth');
      $('codexImportPane').classList.toggle('hidden', mode !== 'import');
    });
  });

  $('startCodexBtn').addEventListener('click', startCodexLogin);
  $('addCodexBtn').addEventListener('click', importCodexAccount);
}

export async function startCodexLogin() {
  const res = await api('/auth/codex/start', { method: 'POST', body: JSON.stringify({}) });
  const d = await res.json();
  if (d.sessionId && d.signInUrl) {
    state.codexSession = d.sessionId;
    $('codexSignInUrl').textContent = d.signInUrl;
    $('codexStep1').classList.add('hidden');
    $('codexStep2').classList.remove('hidden');
    $('codexOpenBtn').addEventListener('click', () => window.open($('codexSignInUrl').textContent, '_blank'));
    $('codexCopyBtn').addEventListener('click', async () => {
      await copyText($('codexSignInUrl').textContent);
      toast(t('common.copied'), 'primary');
    });
    $('codexCancelBtn').addEventListener('click', cancelCodexLogin);
    $('codexCompleteBtn').addEventListener('click', completeCodexManual);
    window.open(d.signInUrl, '_blank');
    pollCodex(d.interval || 2);
  } else toastError(t('common.failed') + ': ' + (d.error || ''));
}

export async function completeCodexManual() {
  const callbackUrl = ($('codexCallback').value || '').trim();
  if (!callbackUrl) { toastError(t('common.failed') + ': ' + (t('codex.callbackUrl') || t('antigravity.callbackUrl'))); return; }
  if (state.codexPollTimer) { clearTimeout(state.codexPollTimer); state.codexPollTimer = null; }
  $('codexStatus').textContent = t('builderid.waiting');
  const res = await api('/auth/codex/complete', { method: 'POST', body: JSON.stringify({ sessionId: state.codexSession, callbackUrl }) });
  const d = await res.json();
  if (d.completed) {
    state.codexSession = '';
    closeModal(); loadAccounts(); loadStats();
    toastPrimary(t('builderid.success') + ': ' + (d.account?.email || d.account?.id));
    autoRefreshNewAccount(d.account?.id);
  } else {
    toastError(t('common.failed') + ': ' + (d.error || ''));
    pollCodex(2);
  }
}

export function pollCodex(interval) {
  state.codexPollTimer = setTimeout(async () => {
    const res = await api('/auth/codex/poll', { method: 'POST', body: JSON.stringify({ sessionId: state.codexSession }) });
    const d = await res.json();
    if (d.completed) {
      state.codexSession = '';
      closeModal(); loadAccounts(); loadStats();
      toastPrimary(t('builderid.success') + ': ' + (d.account?.email || d.account?.id));
      autoRefreshNewAccount(d.account?.id);
    } else if (d.success && !d.completed) {
      $('codexStatus').textContent = t('builderid.waiting');
      pollCodex(interval);
    } else {
      toastError(t('common.failed') + ': ' + (d.error || ''));
      cancelCodexLogin();
    }
  }, interval * 1000);
}

export function cancelCodexLogin() {
  if (state.codexPollTimer) { clearTimeout(state.codexPollTimer); state.codexPollTimer = null; }
  if (state.codexSession) {
    api('/auth/codex/cancel', { method: 'POST', body: JSON.stringify({ sessionId: state.codexSession }) }).catch(() => {});
  }
  state.codexSession = '';
  showModal('add');
}

export async function importCodexAccount() {
  const raw = ($('codexToken').value || '').trim();
  const weight = parseInt($('codexWeight').value || '1', 10) || 1;
  if (!raw) {
    return toastError((t('codex.token') || 'Token') + ' is required');
  }

  // Accept either a full auth.json object or a single access token string.
  const payload = { weight };
  if (raw[0] === '{') {
    try {
      const parsed = JSON.parse(raw);
      payload.accessToken = parsed.accessToken || parsed.access_token || '';
      payload.refreshToken = parsed.refreshToken || parsed.refresh_token || '';
      payload.idToken = parsed.idToken || parsed.id_token || '';
      payload.expiresIn = parsed.expiresIn || parsed.expires_in || 0;
    } catch (e) {
      return toastError('Invalid auth.json: ' + (e.message || e));
    }
  } else {
    payload.accessToken = raw;
  }
  if (!payload.accessToken) {
    return toastError('accessToken is required');
  }

  try {
    const res = await api('/accounts/codex/import', { method: 'POST', body: JSON.stringify(payload) });
    const d = await res.json();
    if (!res.ok || !d.success) {
      throw new Error(d.error || 'Failed to add Codex account');
    }
    closeModal();
    await loadAccounts();
    await loadStats();
    toastPrimary(t('builderid.success') + ': ' + (d.account?.email || d.account?.id));
    autoRefreshNewAccount(d.account?.id);
  } catch (e) {
    toastError('Failed to add Codex account: ' + (e.message || e));
  }
}

export function updateLocalFields() {
  const p = $('localProvider').value;
  $('localClientGroup').classList.toggle('hidden', p === 'Google' || p === 'Github');
}
export function loadLocalFile(input, targetId) {
  const file = input.files[0];
  if (!file) return;
  const r = new FileReader();
  r.onload = e => { $(targetId).value = e.target.result; };
  r.readAsText(file);
}

// Import handlers
export async function importLocalKiro() {
  const provider = $('localProvider').value;
  const tokenJson = $('localTokenJson').value.trim();
  const clientJson = $('localClientJson').value.trim();
  const isSocial = provider === 'Google' || provider === 'Github';
  if (!tokenJson) return toastWarning(t('local.tokenMissing'));
  let tokenData, clientData;
  try { tokenData = JSON.parse(tokenJson); } catch { return toastWarning(t('local.tokenInvalid')); }
  if (!tokenData.refreshToken) return toastWarning(t('local.refreshTokenMissing'));
  if (!isSocial) {
    if (!clientJson) return toastWarning(t('local.clientMissing'));
    try { clientData = JSON.parse(clientJson); } catch { return toastWarning(t('local.clientInvalid')); }
    if (!clientData.clientId || !clientData.clientSecret) return toastWarning(t('local.clientSecretMissing'));
  }
  const authMethod = clientData ? 'idc' : 'social';
  const payload = {
    refreshToken: tokenData.refreshToken,
    accessToken: tokenData.accessToken || '',
    clientId: clientData?.clientId || '',
    clientSecret: clientData?.clientSecret || '',
    region: tokenData.region || '',
    authMethod, provider
  };
  const res = await api('/auth/credentials', { method: 'POST', body: JSON.stringify(payload) });
  const d = await res.json();
  if (d.success) {
    closeModal(); loadAccounts(); loadStats();
    toastPrimary(t('local.importSuccess') + ': ' + (d.account?.email || d.account?.id));
    autoRefreshNewAccount(d.account?.id);
  } else toastError(t('common.failed') + ': ' + (d.error || ''));
}
export async function importCredentials() {
  const raw = $('credJson').value.trim();
  if (!raw) { toastWarning(t('credentials.jsonError')); return; }
  let items;
  let skipped = 0;
  try {
    const json = JSON.parse(raw);
    if (json.accounts && Array.isArray(json.accounts)) {
      items = json.accounts.map(a => {
        const c = a.credentials || {};
        return {
          refreshToken: c.refreshToken || a.refreshToken,
          clientId: c.clientId || a.clientId,
          clientSecret: c.clientSecret || a.clientSecret,
          region: c.region || a.region,
          authMethod: c.authMethod || a.authMethod,
          provider: c.provider || a.provider || a.idp
        };
      });
    } else {
      items = Array.isArray(json) ? json : [json];
    }
  } catch {
    const parsed = parseLineCredentials(raw);
    items = parsed.items;
    skipped = parsed.skipped;
    if (items.length === 0 && skipped === 0) {
      toastWarning(t('credentials.jsonError'));
      return;
    }
    if (items.length === 0) {
      toastWarning(t('credentials.lineParseAllSkipped', skipped));
      return;
    }
  }
  let ok = 0, fail = 0, newIds = [];
  for (const item of items) {
    if (!item.refreshToken) { fail++; continue; }
    let authMethod = item.authMethod || '';
    if (item.clientId && item.clientSecret) authMethod = 'idc';
    else if (!authMethod || authMethod === 'social') authMethod = 'social';
    else authMethod = authMethod.toLowerCase() === 'idc' ? 'idc' : 'social';
    let provider = item.provider || '';
    if (!provider && authMethod === 'social') provider = 'Google';
    if (!provider && authMethod === 'idc') provider = 'BuilderId';
    const payload = {
      refreshToken: item.refreshToken,
      accessToken: item.accessToken || '',
      clientId: item.clientId || '',
      clientSecret: item.clientSecret || '',
      authMethod, provider,
      region: item.region || 'us-east-1'
    };
    try {
      const res = await api('/auth/credentials', { method: 'POST', body: JSON.stringify(payload) });
      const d = await res.json();
      if (d.success) { ok++; if (d.account?.id) newIds.push(d.account.id); }
      else fail++;
    } catch { fail++; }
  }
  closeModal(); loadAccounts(); loadStats();
  let msg = t('sso.importSuccess', ok);
  if (fail > 0) msg += t('sso.importPartial', fail);
  if (skipped > 0) msg += t('credentials.lineParseSkipped', skipped);
  toastPrimary(msg, { duration: 5200 });
  newIds.forEach(autoRefreshNewAccount);
}
export function parseLineCredentials(text) {
  const items = [];
  let skipped = 0;
  for (const line of text.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    let parts;
    if (trimmed.includes('----')) {
      parts = trimmed.split('----').map(s => s.trim());
    } else if (trimmed.includes('\t')) {
      parts = trimmed.split(/\t+/).map(s => s.trim());
    } else {
      parts = trimmed.split(/\s+/).map(s => s.trim());
    }
    if (parts.length < 5) { skipped++; continue; }
    const refreshToken = parts[2];
    if (!refreshToken) { skipped++; continue; }
    items.push({
      refreshToken,
      clientId: parts[3],
      clientSecret: parts[4],
    });
  }
  return { items, skipped };
}
export async function importFromCookie() {
  const refreshToken = $('cookieRefreshToken').value.trim();
  if (!refreshToken) return toastWarning(t('cookie.refreshTokenMissing'));
  const provider = $('cookieProvider').value;
  const payload = { refreshToken, accessToken: '', clientId: '', clientSecret: '', authMethod: 'social', provider };
  const res = await api('/auth/credentials', { method: 'POST', body: JSON.stringify(payload) });
  const d = await res.json();
  if (d.success) {
    closeModal(); loadAccounts(); loadStats();
    toastPrimary(t('cookie.importSuccess') + ': ' + (d.account?.email || d.account?.id));
    autoRefreshNewAccount(d.account?.id);
  } else toastError(t('common.failed') + ': ' + (d.error || ''));
}
export async function importSsoToken() {
  const res = await api('/auth/sso-token', {
    method: 'POST', body: JSON.stringify({
      bearerToken: $('ssoToken').value,
      region: $('ssoRegion').value
    })
  });
  const d = await res.json();
  if (d.success) {
    closeModal(); loadAccounts(); loadStats();
    const count = d.accounts?.length || 0;
    const errs = d.errors?.length || 0;
    let msg = t('sso.importSuccess', count);
    if (errs > 0) msg += t('sso.importPartial', errs);
    toastPrimary(msg, { duration: 5200 });
    if (d.accounts) d.accounts.forEach(a => autoRefreshNewAccount(a.id));
  } else toastError(t('common.failed') + ': ' + (d.error || ''));
}
export async function startBuilderIdLogin() {
  const region = $('builderIdRegion').value || 'us-east-1';
  const res = await api('/auth/builderid/start', { method: 'POST', body: JSON.stringify({ region }) });
  const d = await res.json();
  if (d.sessionId) {
    state.builderIdSession = d.sessionId;
    $('builderIdUserCode').textContent = d.userCode;
    $('builderIdVerifyUrl').textContent = d.verificationUri;
    $('builderIdStep1').classList.add('hidden');
    $('builderIdStep2').classList.remove('hidden');
    $('builderIdOpenBtn').addEventListener('click', () => window.open($('builderIdVerifyUrl').textContent, '_blank'));
    $('builderIdCopyBtn').addEventListener('click', async () => {
      await copyText($('builderIdVerifyUrl').textContent);
      toast(t('common.copied'), 'primary');
    });
    $('builderIdCancelBtn').addEventListener('click', cancelBuilderIdLogin);
    pollBuilderIdAuth(d.interval || 5);
  } else toastError(t('common.failed') + ': ' + (d.error || ''));
}
export function pollBuilderIdAuth(interval) {
  state.builderIdPollTimer = setTimeout(async () => {
    const res = await api('/auth/builderid/poll', { method: 'POST', body: JSON.stringify({ sessionId: state.builderIdSession }) });
    const d = await res.json();
    if (d.completed) {
      closeModal(); loadAccounts(); loadStats();
      toastPrimary(t('builderid.success') + ': ' + (d.account?.email || d.account?.id));
      autoRefreshNewAccount(d.account?.id);
    } else if (d.success && !d.completed) {
      $('builderIdStatus').textContent = t('builderid.waiting');
      pollBuilderIdAuth(d.interval || interval);
    } else {
      toastError(t('common.failed') + ': ' + (d.error || ''));
      cancelBuilderIdLogin();
    }
  }, interval * 1000);
}
export function cancelBuilderIdLogin() {
  if (state.builderIdPollTimer) { clearTimeout(state.builderIdPollTimer); state.builderIdPollTimer = null; }
  state.builderIdSession = '';
  showModal('add');
}
export async function startIamSso() {
  if (state.iamSession) {
    const res = await api('/auth/iam-sso/complete', {
      method: 'POST', body: JSON.stringify({
        sessionId: state.iamSession, callbackUrl: $('iamCallback').value
      })
    });
    const d = await res.json();
    if (d.success) {
      closeModal(); loadAccounts(); loadStats();
      toastPrimary(t('builderid.success') + ': ' + (d.account?.email || d.account?.id));
      autoRefreshNewAccount(d.account?.id);
    } else toastError(t('common.failed') + ': ' + (d.error || ''));
  } else {
    const res = await api('/auth/iam-sso/start', {
      method: 'POST', body: JSON.stringify({
        startUrl: $('iamStartUrl').value, region: $('iamRegion').value
      })
    });
    const d = await res.json();
    if (d.authorizeUrl) {
      state.iamSession = d.sessionId;
      $('iamAuthUrl').textContent = d.authorizeUrl;
      $('iamStep2').classList.remove('hidden');
      $('iamBtn').textContent = t('iam.complete');
      $('iamOpenBtn').addEventListener('click', () => window.open($('iamAuthUrl').textContent, '_blank'));
      $('iamCopyBtn').addEventListener('click', async () => {
        await copyText($('iamAuthUrl').textContent);
        toast(t('common.copied'), 'primary');
      });
    } else toastError(t('common.failed') + ': ' + (d.error || ''));
  }
}
export async function autoRefreshNewAccount(id) {
  if (!id) return;
  try { await api('/accounts/' + id + '/refresh', { method: 'POST' }); } catch (e) { }
  loadAccounts();
}

// Export modal
export function showExportModal() {
  if (!state.accountsData.length) return toastWarning(t('accounts.empty'));
  state.exportSelectedIds = new Set(state.accountsData.map(a => a.id));
  renderExportModal();
  openDialog('exportModal');
}
export function closeExportModal() { closeDialog('exportModal'); }
export function renderExportModal() {
  const body = $('exportBody');
  const all = state.exportSelectedIds.size === state.accountsData.length;
  body.innerHTML =
    '<div class="flex items-center justify-between mb-3">' +
    '<span class="text-sm muted-text">' + escapeHtml(t('export.selected', state.exportSelectedIds.size)) + '</span>' +
    '<button class="btn btn-sm btn-outline" id="exportToggleAllBtn" type="button">' + escapeHtml(all ? t('export.deselectAll') : t('export.selectAll')) + '</button>' +
    '</div>' +
    '<div class="export-list">' +
    state.accountsData.map(a => {
      const checked = state.exportSelectedIds.has(a.id);
      return '<label class="export-row' + (checked ? ' selected' : '') + '">' +
        '<input type="checkbox" ' + (checked ? 'checked' : '') + ' data-export-toggle="' + escapeAttr(a.id) + '" />' +
        '<div class="export-row-text">' +
        '<div class="export-row-email">' + escapeHtml(getDisplayEmail(a.email, a.id)) + '</div>' +
        '<div class="export-row-meta">' + escapeHtml(formatAuthMethod(a.provider || a.authMethod)) + ' · ' + escapeHtml(formatSubscriptionLabel(a.subscriptionType)) + '</div>' +
        '</div>' +
        '</label>';
    }).join('') +
    '</div>' +
    '<div id="exportJsonPreview" class="hidden mb-3"><textarea id="exportJsonText" readonly class="font-mono"></textarea></div>' +
    '<div class="modal-footer">' +
    '<button class="btn btn-secondary" id="exportCloseBtn" type="button">' + escapeHtml(t('common.cancel')) + '</button>' +
    '<button class="btn btn-outline" id="exportShowJsonBtn" type="button">' + escapeHtml(t('export.showJson')) + '</button>' +
    '<button class="btn btn-outline" id="exportCopyJsonBtn" type="button">' + escapeHtml(t('export.copyJson')) + '</button>' +
    '<button class="btn btn-primary" id="exportDownloadBtn" type="button">' + escapeHtml(t('export.downloadJson')) + '</button>' +
    '</div>';
  $('exportToggleAllBtn').addEventListener('click', () => {
    if (state.exportSelectedIds.size === state.accountsData.length) state.exportSelectedIds.clear();
    else state.exportSelectedIds = new Set(state.accountsData.map(a => a.id));
    renderExportModal();
  });
  $('exportCloseBtn').addEventListener('click', closeExportModal);
  $('exportShowJsonBtn').addEventListener('click', exportShowJson);
  $('exportCopyJsonBtn').addEventListener('click', exportCopyJson);
  $('exportDownloadBtn').addEventListener('click', exportDownloadJson);
  qsa('[data-export-toggle]', body).forEach(cb => cb.addEventListener('change', e => {
    const id = e.target.dataset.exportToggle;
    if (state.exportSelectedIds.has(id)) state.exportSelectedIds.delete(id);
    else state.exportSelectedIds.add(id);
    renderExportModal();
  }));
}
export async function getExportData() {
  if (state.exportSelectedIds.size === 0) { toastWarning(t('export.noSelection')); return null; }
  const res = await api('/export', { method: 'POST', body: JSON.stringify({ ids: Array.from(state.exportSelectedIds) }) });
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    toastError(t('common.failed') + ': ' + (err.error || t('common.unknownError')));
    return null;
  }
  return res.json();
}
export async function exportShowJson() {
  const data = await getExportData();
  if (!data) return;
  $('exportJsonPreview').classList.remove('hidden');
  $('exportJsonText').value = JSON.stringify(data, null, 2);
}
export async function exportCopyJson() {
  if (state.exportSelectedIds.size === 0) { toastWarning(t('export.noSelection')); return; }
  const jsonPromise = getExportData().then(data => {
    if (!data) throw new Error('no-data');
    const filtered = (data.accounts || []).map(a => {
      const { clientId, clientSecret, accessToken, refreshToken } = a.credentials || {};
      return { clientId, clientSecret, accessToken, refreshToken };
    });
    return JSON.stringify(filtered, null, 2);
  });
  try {
    await copyText(jsonPromise);
    toast(t('export.copied'), 'primary');
  } catch (e) {
    if (e && e.message !== 'no-data') toastError(t('common.failed'));
  }
}
export async function exportDownloadJson() {
  const data = await getExportData();
  if (!data) return;
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = 'kiro-accounts-' + new Date().toISOString().slice(0, 10) + '.json';
  a.click();
  URL.revokeObjectURL(url);
}
