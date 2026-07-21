// state.js — shared mutable state + leaf constants for the admin UI.
//
// ES module import bindings are read-only, so per-variable exports could not be
// reassigned. All mutable UI state therefore lives on the single exported
// `state` object; modules read/write via `state.x = ...`. The other exports
// here are immutable leaf values (no imports), so this module is dependency-free
// and safe to import from anywhere without circular-import risk.

export const baseUrl = location.origin;

// Language cache + order (dict is filled in by i18n.loadLocale at runtime).
export const dict = { en: null, zh: null, vi: null };
export const LANGS = ['zh', 'en', 'vi'];

// Long-lived Sets shared across renders (identity matters, so not on `state`).
export const selectedAccounts = new Set();
export const agQuotaExpanded = new Set();

// Drop any persisted password unless "remember me" was set. Must run before the
// state object below reads admin_password.
if (localStorage.getItem('kiro_remember') !== '1') {
  localStorage.removeItem('admin_password');
  localStorage.removeItem('admin_login_time');
}

// Mutable UI state. One object so it can be shared as a module export (import
// bindings are read-only; object properties can be reassigned).
export const state = {
  password: sessionStorage.getItem('admin_password') || localStorage.getItem('admin_password') || '',
  currentLang: localStorage.getItem('kiro_lang') || 'vi',
  accountsData: [],
  filterKeyword: '',
  filterStatus: 'all',
  currentView: 'overview',
  currentProviderFilter: '',
  privacyModeEnabled: true,
  promptRules: [],
  builderIdSession: '',
  builderIdPollTimer: null,
  iamSession: '',
  kiroSsoSession: '',
  kiroSsoPollTimer: null,
  antigravitySession: '',
  antigravityPollTimer: null,
  grokSession: '',
  grokPollTimer: null,
  codexSession: '',
  codexPollTimer: null,
  exportSelectedIds: new Set(),
  currentVersion: '',
  testLogs: [],
  testModalAccountId: '',
  testModalModels: [],
  testModalLoadingModels: false,
  testModalModelError: false,
  testModalRunning: false,
  customSelectUid: 0,
  customSelectObserver: null,
  customSelectRefreshQueued: false,
  modalScrollY: 0,
  confirmResolve: null,
  logsFilter: 'all',
  logsAutoTimer: null,
  logsCache: [],
  apiKeyRevealCache: {},
  apiKeyRevealed: {},
  apiKeysCache: [],
  apiKeysFilterKeyword: '',
  apiKeysFilterStatus: 'all',
  apiKeysPage: 1,
  apiKeysPageSize: 20,
  apiKeyEditingId: '',
  apiKeyModalSubmitting: false,
  overviewApiKeyStatsFp: '',
};
