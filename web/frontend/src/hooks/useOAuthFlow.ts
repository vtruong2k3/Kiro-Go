// useOAuthFlow — the ONE shared driver for every session-based add-account flow
// (Grok, Antigravity, Codex, Kiro-SSO, BuilderID). Parametrized by the provider's
// start/poll/complete/cancel functions so no flow re-implements polling/cleanup.
//
// Lifecycle:
//   start(args) → server returns { sessionId, signInUrl, interval } and we begin
//   polling poll(sessionId) on that interval. A poll that resolves completed:true
//   ends the flow with the account. complete(callbackUrl) is the manual-paste
//   fallback for headless/domain deploys where the loopback callback can't reach
//   the server. cancel() / unmount stops polling and tells the server to drop the
//   session. On success we invalidate the accounts query.
import { useCallback, useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { qk } from '@/config/queryKeys'
import { ApiError } from '@/services/httpClient'
import type {
  StartResponse,
  PollResponse,
  CompleteResponse,
  FlowAccount,
} from '@/types/auth'

export type FlowPhase = 'idle' | 'starting' | 'awaiting' | 'polling' | 'redirect' | 'done' | 'error'

interface OAuthFlowConfig<StartArgs> {
  start: (args: StartArgs) => Promise<StartResponse>
  poll?: (sessionId: string) => Promise<PollResponse>
  complete?: (sessionId: string, callbackUrl: string) => Promise<CompleteResponse>
  cancel?: (sessionId: string) => Promise<unknown>
}

export interface OAuthFlowState {
  phase: FlowPhase
  signInUrl: string
  userCode: string
  verificationUri: string
  account: FlowAccount | null
  error: string
  callbackMode: 'automatic' | 'manual' | ''
  callbackHint: string
}

const INITIAL: OAuthFlowState = {
  phase: 'idle',
  signInUrl: '',
  userCode: '',
  verificationUri: '',
  account: null,
  error: '',
  callbackMode: '',
  callbackHint: '',
}

export function useOAuthFlow<StartArgs = void>(config: OAuthFlowConfig<StartArgs>) {
  const qc = useQueryClient()
  const [state, setState] = useState<OAuthFlowState>(INITIAL)
  const sessionRef = useRef<string>('')
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const stoppedRef = useRef(false)
  const generationRef = useRef(0)
  const retryRef = useRef(0)

  const stopPolling = useCallback(() => {
    if (timerRef.current) {
      clearTimeout(timerRef.current)
      timerRef.current = null
    }
  }, [])

  const finishSuccess = useCallback(
    (account: FlowAccount | null) => {
      stopPolling()
      stoppedRef.current = true
      void qc.invalidateQueries({ queryKey: qk.accounts })
      setState((s) => ({ ...s, phase: 'done', account: account ?? s.account }))
    },
    [qc, stopPolling],
  )

  const failWith = useCallback(
    (message: string) => {
      stopPolling()
      setState((s) => ({ ...s, phase: 'error', error: message }))
    },
    [stopPolling],
  )

  const schedulePoll = useCallback(
    (intervalMs: number, generation = generationRef.current) => {
      if (!config.poll) return
      stopPolling()
      timerRef.current = setTimeout(async () => {
        if (stoppedRef.current || generation !== generationRef.current) return
        const sid = sessionRef.current
        if (!sid) return
        try {
          const res = await config.poll!(sid)
          if (stoppedRef.current || generation !== generationRef.current) return
          retryRef.current = 0
          if (res.completed) {
            finishSuccess(res.account ?? null)
            return
          }
          const nextMs = (res.interval ?? intervalMs / 1000) * 1000
          setState((s) => ({ ...s, phase: 'polling', error: '' }))
          schedulePoll(nextMs, generation)
        } catch (err) {
          if (stoppedRef.current || generation !== generationRef.current) return
          const apiErr = err instanceof ApiError ? err : null
          // Network failures and 5xx responses are transient; authenticated
          // session/OAuth errors remain terminal and are surfaced immediately.
          if (!apiErr || apiErr.status === 0 || apiErr.status >= 500) {
            const retry = Math.min(retryRef.current, 5)
            retryRef.current += 1
            const backoff = Math.min(15000, Math.max(intervalMs, 1000) * 2 ** retry)
            setState((s) => ({ ...s, phase: 'polling', error: 'Connection interrupted; reconnecting…' }))
            schedulePoll(backoff, generation)
            return
          }
          failWith(apiErr.message)
        }
      }, intervalMs)
    },
    [config, finishSuccess, failWith, stopPolling],
  )

  const openSignIn = useCallback((url: string) => {
    if (!url) return
    window.open(url, '_blank', 'noopener')
  }, [])

  const start = useCallback(
    async (args: StartArgs, popup?: Window | null) => {
      stoppedRef.current = false
      generationRef.current += 1
      retryRef.current = 0
      const generation = generationRef.current
      setState({ ...INITIAL, phase: 'starting' })
      try {
        const res = await config.start(args)
        if (stoppedRef.current || generation !== generationRef.current) return
        sessionRef.current = res.sessionId
        const url = res.signInUrl ?? res.authorizeUrl ?? ''
        setState({
          ...INITIAL,
          phase: 'awaiting',
          signInUrl: url,
          userCode: res.userCode ?? '',
          verificationUri: res.verificationUri ?? '',
          account: null,
          error: '',
          callbackMode: res.callbackMode ?? '',
          callbackHint: res.callbackHint ?? '',
        })
        // Reuse a tab opened synchronously by the click handler. If no tab was
        // opened (effect-driven providers or popup blockers), retain the link
        // as the reliable fallback.
        if (popup && !popup.closed) {
          popup.location.href = url
        } else {
          openSignIn(url)
        }
        if (config.poll) schedulePoll((res.interval ?? 2) * 1000, generation)
      } catch (err) {
        failWith(err instanceof ApiError ? err.message : String(err))
      }
    },
    [config, schedulePoll, failWith, openSignIn],
  )

  const complete = useCallback(
    async (callbackUrl: string) => {
      if (!config.complete) return
      const sid = sessionRef.current
      if (!sid) return
      stopPolling()
      setState((s) => ({ ...s, phase: 'polling', error: '' }))
      try {
        const res = await config.complete(sid, callbackUrl)
        // M365 hosted SSO is a 2-leg flow: leg 1 returns a redirect URL (the
        // Microsoft login) that the operator opens, signs into, then pastes the
        // second callback URL to finish. Not completed + no redirect = failure.
        if (res.completed || (res.success && res.account)) {
          finishSuccess(res.account ?? null)
        } else if (res.status === 'redirect' && res.redirectUrl) {
          const url = res.redirectUrl
          window.open(url, '_blank', 'noopener')
          setState((s) => ({ ...s, phase: 'redirect', signInUrl: url, error: '' }))
        } else {
          failWith('failed')
        }
      } catch (err) {
        failWith(err instanceof ApiError ? err.message : String(err))
      }
    },
    [config, finishSuccess, failWith, stopPolling],
  )

  const cancel = useCallback(() => {
    stoppedRef.current = true
    stopPolling()
    const sid = sessionRef.current
    if (sid && config.cancel) void config.cancel(sid).catch(() => null)
    sessionRef.current = ''
    setState(INITIAL)
  }, [config, stopPolling])

  // Stop polling + drop the server session on unmount.
  useEffect(() => {
    return () => {
      stoppedRef.current = true
      stopPolling()
      const sid = sessionRef.current
      if (sid && config.cancel) void config.cancel(sid).catch(() => null)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return { state, start, complete, cancel }
}
