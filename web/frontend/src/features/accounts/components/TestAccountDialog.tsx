// TestAccountDialog — pick a model and run a live test against one account. The
// test hits POST /accounts/{id}/test; a small log area shows start → success /
// failed. Models come from the account's cached model list (useAccountModels).
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { CheckCircle2, XCircle } from 'lucide-react'
import type { AccountListItem } from '@/types/account'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { AccountModelPicker } from '@/components/shared/AccountModelPicker'
import { Label } from '@/components/ui/label'
import { HamsterWheel } from '@/components/shared/HamsterLoader'
import { tp } from '@/lib/t'
import { useAccountModels } from '@/hooks/queries/useProviderModels'
import { testAccount, type TestAccountResult } from '@/services/accounts.service'

interface Props {
  account: AccountListItem | null
  onClose: () => void
}

interface LogLine {
  tone: 'info' | 'success' | 'error'
  text: string
}

export function TestAccountDialog({ account, onClose }: Props) {
  const { t } = useTranslation()
  const models = useAccountModels(account?.id ?? '', !!account)
  const [model, setModel] = useState('')
  const [running, setRunning] = useState(false)
  const [log, setLog] = useState<LogLine[]>([])

  useEffect(() => {
    setLog([])
    setModel('')
  }, [account?.id])

  const modelList = models.data ?? []

  async function runTest() {
    if (!account) return
    setRunning(true)
    setLog([{ tone: 'info', text: tp(t, 'accounts.testLog.start', account.email, model || t('accounts.selectModel'), account.provider) }])
    try {
      const res: TestAccountResult = await testAccount(account.id, model || undefined)
      if (res.success) {
        setLog((l) => [
          ...l,
          { tone: 'success', text: `${t('accounts.testLog.success')}${res.model ? ` · ${res.model}` : ''}${res.latency ? ` · ${res.latency}ms` : ''}` },
        ])
      } else {
        setLog((l) => [...l, { tone: 'error', text: res.error || res.message || t('accounts.testLog.failed') }])
      }
    } catch (err) {
      setLog((l) => [...l, { tone: 'error', text: err instanceof Error ? err.message : t('accounts.testLog.error') }])
    } finally {
      setRunning(false)
    }
  }

  return (
    <Dialog open={!!account} onOpenChange={(o) => !o && onClose()}>
      <DialogContent
        className="w-[min(720px,calc(100vw-2rem))] max-w-none gap-5 p-6 sm:max-w-none"
        style={{ minHeight: '320px' }}
      >
        <DialogHeader>
          <DialogTitle>{t('accounts.testModalTitle')}</DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-2">
            <Label>{t('accounts.selectModel')}</Label>
            <AccountModelPicker
              models={modelList}
              value={model}
              onChange={setModel}
              isLoading={models.isPending}
              disabled={running}
            />
          </div>

          {log.length > 0 && (
            <div className="min-h-24 max-h-60 w-full min-w-0 space-y-1 overflow-x-hidden overflow-y-auto rounded-lg border bg-muted/40 p-3 font-mono text-xs">
              {log.map((line, i) => (
                <div
                  key={i}
                  className={
                    line.tone === 'success'
                      ? 'flex min-w-0 items-start gap-1.5 break-all text-emerald-600 dark:text-emerald-400'
                      : line.tone === 'error'
                        ? 'flex min-w-0 items-start gap-1.5 break-all text-destructive'
                        : 'flex min-w-0 items-start gap-1.5 break-all text-muted-foreground'
                  }
                >
                  {line.tone === 'success' && <CheckCircle2 className="size-3.5" />}
                  {line.tone === 'error' && <XCircle className="size-3.5" />}
                  <span className="min-w-0 whitespace-pre-wrap break-all">{line.text}</span>
                </div>
              ))}
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common.close')}
          </Button>
          <Button onClick={runTest} disabled={running}>
            {running ? (
              <span className="flex items-center gap-2">
                <HamsterWheel size="sm" />
                {t('accounts.testing')}
              </span>
            ) : (
              t('accounts.test')
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
