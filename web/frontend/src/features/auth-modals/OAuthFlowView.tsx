// OAuthFlowView — the shared UI for a session-based OAuth flow driven by
// useOAuthFlow. Renders the sign-in link + user code, a live phase indicator,
// the manual-paste callback fallback (headless/domain deploys), and the done /
// error terminal states. Provider-specific flows just wire their start/poll/
// complete/cancel into useOAuthFlow and render this.
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { CheckCircle2, ExternalLink, XCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { HamsterLoader } from '@/components/shared/HamsterLoader'
import { CopyButton } from '@/components/shared/CopyButton'
import type { OAuthFlowState } from '@/hooks/useOAuthFlow'

interface Props {
  // Only the parts OAuthFlowView renders — keeps it agnostic to each flow's StartArgs.
  flow: {
    state: OAuthFlowState
    complete: (callbackUrl: string) => Promise<void>
  }
  /** Show the manual-paste callback field (loopback fallback). */
  allowManual?: boolean
  onDone?: () => void
}

export function OAuthFlowView({ flow, allowManual, onDone }: Props) {
  const { t } = useTranslation()
  const { state, complete: onComplete } = flow
  const [callback, setCallback] = useState('')

  if (state.phase === 'done') {
    return (
      <div className="flex flex-col items-center gap-3 py-8 text-center">
        <CheckCircle2 className="size-12 text-emerald-500" />
        <p className="font-medium">{t('accounts.testSuccess')}</p>
        {state.account?.email && (
          <p className="text-sm text-muted-foreground">{state.account.email}</p>
        )}
        <Button onClick={onDone}>{t('common.close')}</Button>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {(state.phase === 'starting' || state.phase === 'polling') && (
        <HamsterLoader size="sm" label={t('builderid.waiting')} />
      )}

      {state.phase === 'redirect' && (
        <div className="rounded-lg border border-sky-500/30 bg-sky-500/10 p-3 text-sm text-sky-700 dark:text-sky-300">
          {t('kirosso.pasteSecond')}
        </div>
      )}

      {state.callbackHint && (
        <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-700 dark:text-amber-300">
          {state.callbackHint}
        </div>
      )}

      {state.signInUrl && (
        <div className="space-y-2">
          <Label>{t('iam.loginUrl')}</Label>
          <div className="flex min-w-0 items-center gap-2">
            <a
              href={state.signInUrl}
              target="_blank"
              rel="noreferrer"
              className="flex min-w-0 flex-1 items-center gap-2 rounded-lg border bg-muted/50 px-3 py-2 text-sm hover:bg-muted"
            >
              <ExternalLink className="size-4 shrink-0" />
              <span className="truncate">{state.signInUrl}</span>
            </a>
            <CopyButton value={state.signInUrl} />
          </div>
        </div>
      )}

      {state.userCode && (
        <div className="space-y-1">
          <div className="flex items-center justify-center gap-2 rounded-lg border bg-muted/50 py-3">
            <span className="font-mono text-xl font-semibold tracking-widest">{state.userCode}</span>
            <CopyButton value={state.userCode} />
          </div>
          <p className="text-center text-xs text-muted-foreground">{t('builderid.verifyCode')}</p>
        </div>
      )}

      {allowManual && onComplete && (
        <div className="space-y-2">
          <Label htmlFor="callback">{t('iam.callbackUrl')}</Label>
          <div className="flex items-center gap-2">
            <Input
              id="callback"
              value={callback}
              onChange={(e) => setCallback(e.target.value)}
              placeholder="http://127.0.0.1:.../?code=..."
            />
            <Button
              onClick={() => {
                const value = callback.trim()
                setCallback('')
                void onComplete(value)
              }}
              disabled={!callback.trim() || state.phase === 'polling'}
            >
              {t('iam.complete')}
            </Button>
          </div>
        </div>
      )}

      {state.phase === 'error' && (
        <div className="flex items-start gap-2 rounded-lg border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">
          <XCircle className="mt-0.5 size-4 shrink-0" />
          <span>{state.error || t('common.failed')}</span>
        </div>
      )}
    </div>
  )
}
