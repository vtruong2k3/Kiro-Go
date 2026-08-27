// OAuth / import flow payloads. All start/poll/complete/cancel are POST.
// Flows are stateful via a server-side sessionId.

import type { ProviderKey } from './provider'

// Minimal account stub returned by a completed flow.
export interface FlowAccount {
  id: string
  email: string
  authMethod?: string
  provider?: string
  projectId?: string
  plan?: string
  region?: string
  subscription?: string
  remoteBaseURL?: string
  remoteCheckKeyURL?: string
  modelCount?: number
  nickname?: string
}

// start responses vary slightly per provider.
export interface StartResponse {
  sessionId: string
  // Different flows name the URL differently.
  signInUrl?: string
  authorizeUrl?: string
  verificationUri?: string
  userCode?: string
  interval?: number
  expiresIn?: number
  callbackMode?: 'automatic' | 'manual'
  callbackHint?: string
}

export type PollStatus = 'pending' | 'slow_down' | 'redirect'

export interface PollResponse {
  success: boolean
  completed: boolean
  status?: PollStatus
  interval?: number
  redirectUrl?: string
  account?: FlowAccount
}

export interface CompleteResponse {
  success: boolean
  completed?: boolean
  // M365 (Kiro-SSO) completes in two legs: the first paste returns
  // status:'redirect' + redirectUrl (the Microsoft login) to open, then the
  // operator pastes a second callback URL.
  status?: PollStatus
  redirectUrl?: string
  account?: FlowAccount
  error?: string
}

// Batch import (SSO token) can return multiple accounts + per-line errors.
export interface BatchImportResponse {
  success: boolean
  accounts: Array<{ id: string; email: string }>
  errors?: string[]
}

// Which provider a given add-account flow targets.
export type FlowProvider = ProviderKey | 'builderid' | 'iam-sso' | 'kiro-sso'
