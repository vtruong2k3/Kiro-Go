// LogsTable — 9router-style request log table: mono font, sticky header,
// provider-colored chips (same palette on Model + Provider columns).
// Fed by the live SSE buffer (newest-first).
import { useTranslation } from 'react-i18next'
import type { LiveLog } from '@/hooks/queries/useLogStream'
import { Card } from '@/components/ui/card'
import { EmptyState } from '@/components/shared/EmptyState'
import { BrandChip, brandFor } from '@/components/shared/ModelBrand'
import {
  formatDuration,
  formatNumber,
  formatUnixSeconds,
} from '@/lib/format'
import { cn } from '@/lib/utils'

interface LogsTableProps {
  logs: LiveLog[]
  /** apiKeyId → "Name · sk-***" */
  keyNames: Map<string, string>
  /** accountId → email / nickname label */
  accountNames: Map<string, string>
  /** Total unfiltered buffer size — shown as "filtered / total". */
  totalCount?: number
}

function dash(v: string | undefined | null): string {
  return v && v.trim() ? v : '—'
}

/** Prefer resolved label; fall back to a short id, never a raw long UUID wall. */
function resolveLabel(
  id: string | undefined,
  names: Map<string, string>,
): { label: string; title?: string } {
  if (!id) return { label: '—' }
  const name = names.get(id)
  if (name) return { label: name, title: id }
  if (id.length > 14) return { label: `${id.slice(0, 8)}…${id.slice(-4)}`, title: id }
  return { label: id, title: id }
}

/** Endpoint = client protocol surface, not upstream brand. */
function EndpointChip({ endpoint }: { endpoint?: string }) {
  if (!endpoint) return <span className="text-muted-foreground">—</span>
  const e = endpoint.toLowerCase()
  const style =
    e.includes('claude') || e.includes('anthropic') || e.includes('messages')
      ? 'border-orange-500/25 bg-orange-500/10 text-orange-800 dark:text-orange-300'
      : e.includes('response')
        ? 'border-indigo-500/25 bg-indigo-500/10 text-indigo-800 dark:text-indigo-300'
        : e.includes('openai') || e.includes('chat') || e.includes('completions')
          ? 'border-emerald-500/25 bg-emerald-500/10 text-emerald-800 dark:text-emerald-300'
          : 'border-border bg-muted text-muted-foreground'

  return (
    <span
      className={cn(
        'inline-flex rounded-md border px-1.5 py-0.5 text-[11px] font-medium',
        style,
      )}
    >
      {endpoint}
    </span>
  )
}

/** Token breakdown cell: shows input/output/cache categories in one vertical
 *  column with an all-category display total, plus a CACHE badge for
 *  response-cache hits. Falls back to the flat `tokens` total for older log
 *  rows that predate the breakdown fields. */
function TokensCell({ log }: { log: LiveLog }) {
  const { t } = useTranslation()
  const hasBreakdown =
    (log.inputTokens ?? 0) > 0 ||
    (log.outputTokens ?? 0) > 0 ||
    (log.cacheReadTokens ?? 0) > 0 ||
    (log.cacheCreationTokens ?? 0) > 0
  const input = log.inputTokens ?? 0
  const output = log.outputTokens ?? 0
  const cacheRead = log.cacheReadTokens ?? 0
  const cacheCreation = log.cacheCreationTokens ?? 0
  const total = input + output + cacheRead + cacheCreation

  const titleParts: string[] = []
  if (hasBreakdown) {
    titleParts.push(`${t('logs.tokIn')}: ${formatNumber(input)}`)
    titleParts.push(`${t('logs.cacheRead')}: ${formatNumber(cacheRead)}`)
    titleParts.push(`${t('logs.cacheCreation')}: ${formatNumber(cacheCreation)}`)
    titleParts.push(`${t('logs.tokOut')}: ${formatNumber(output)}`)
    titleParts.push(`${t('logs.tokens')}: ${formatNumber(total)}`)
  }

  if (!hasBreakdown) {
    return (
      <span title={log.tokens ? 'Legacy total — breakdown unavailable' : undefined}>
        {log.tokens ? `${formatNumber(log.tokens)} total` : '—'}
      </span>
    )
  }

  return (
    <span className="inline-flex items-start justify-end gap-1" title={titleParts.join(' · ')}>
      {log.cached && (
        <span className="rounded-sm bg-violet-500/15 px-1 text-[9px] font-bold uppercase tracking-wide text-violet-700 dark:text-violet-300">
          {t('logs.cacheHit')}
        </span>
      )}
      <span className="inline-flex flex-col items-end gap-0.5 text-[10px] leading-tight">
        <span className="inline-flex items-center gap-2">
          <span className="text-sky-600 dark:text-sky-400">in {formatNumber(input)}</span>
          <span className="text-violet-600 dark:text-violet-400">out {formatNumber(output)}</span>
        </span>
        <span className="text-emerald-600 dark:text-emerald-400">cache-r {formatNumber(cacheRead)}</span>
        <span className="text-amber-600 dark:text-amber-400">cache-w {formatNumber(cacheCreation)}</span>
        <span className="border-t border-border/60 pt-0.5 font-semibold">total {formatNumber(total)}</span>
      </span>
    </span>
  )
}

export function LogsTable({ logs, keyNames, accountNames, totalCount }: LogsTableProps) {
  const { t } = useTranslation()
  const total = totalCount ?? logs.length

  return (
    <Card className="min-w-0 overflow-hidden bg-muted/20 py-0 dark:bg-black/20">
      <div className="flex items-center justify-between gap-2 border-b border-border/60 px-3 py-2 text-xs text-muted-foreground">
        <span>
          {logs.length === total
            ? t('logs.totalCount', { n: total })
            : t('logs.filteredCount', { shown: logs.length, total })}
        </span>
        <span className="hidden sm:inline">{t('logs.newestFirst')}</span>
      </div>

      <div className="max-h-[calc(100dvh-16rem)] overflow-auto">
        {logs.length === 0 ? (
          <div className="p-8">
            <EmptyState message={t('logs.empty')} />
          </div>
        ) : (
          <table className="w-full border-collapse text-left font-mono text-xs whitespace-nowrap">
            <thead className="sticky top-0 z-10 border-b border-border bg-muted/95 backdrop-blur supports-backdrop-filter:bg-muted/80">
              <tr className="text-[10px] uppercase tracking-wide text-muted-foreground">
                <th className="border-r border-border/50 px-3 py-2 font-semibold">
                  {t('logs.time')}
                </th>
                <th className="border-r border-border/50 px-3 py-2 font-semibold">
                  {t('logs.status')}
                </th>
                <th className="border-r border-border/50 px-3 py-2 font-semibold">
                  {t('logs.model')}
                </th>
                <th className="border-r border-border/50 px-3 py-2 font-semibold">
                  {t('logs.endpoint')}
                </th>
                <th className="border-r border-border/50 px-3 py-2 font-semibold">
                  {t('logs.provider')}
                </th>
                <th className="border-r border-border/50 px-3 py-2 font-semibold">
                  {t('logs.account')}
                </th>
                <th className="border-r border-border/50 px-3 py-2 font-semibold">
                  {t('logs.apiKey')}
                </th>
                <th className="border-r border-border/50 px-3 py-2 text-right font-semibold">
                  {t('logs.tokens')}
                </th>
                <th className="border-r border-border/50 px-3 py-2 text-right font-semibold">
                  {t('logs.credits')}
                </th>
                <th className="border-r border-border/50 px-3 py-2 text-right font-semibold">
                  {t('logs.duration')}
                </th>
                <th className="border-r border-border/50 px-3 py-2 font-semibold">
                  {t('logs.ip')}
                </th>
                <th className="px-3 py-2 font-semibold">{t('logs.detail')}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border/40">
              {logs.map((log) => {
                const ok = log.status === 'success'
                const account = resolveLabel(log.accountId, accountNames)
                const apiKey = resolveLabel(log.apiKeyId, keyNames)
                const errText = !ok
                  ? [log.errorType, log.error].filter(Boolean).join(' — ')
                  : ''
                // Same brand for Model + Provider columns.
                const brand = brandFor(log.provider, log.model, t)

                return (
                  <tr
                    key={log._key}
                    className={cn(
                      'transition-colors hover:bg-primary/5',
                      ok ? undefined : 'bg-destructive/5',
                    )}
                  >
                    <td className="border-r border-border/30 px-3 py-1.5 text-muted-foreground">
                      {formatUnixSeconds(log.time, {
                        month: 'short',
                        day: 'numeric',
                        hour: '2-digit',
                        minute: '2-digit',
                        second: '2-digit',
                        hour12: false,
                      })}
                    </td>
                    <td className="border-r border-border/30 px-3 py-1.5">
                      <span
                        className={cn(
                          'inline-flex items-center rounded-full px-2 py-0.5 text-[11px] font-bold',
                          ok
                            ? 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-400'
                            : 'bg-destructive/15 text-destructive',
                        )}
                      >
                        {ok ? t('logs.statusSuccess') : t('logs.statusError')}
                      </span>
                    </td>
                    <td className="border-r border-border/30 px-3 py-1.5">
                      {log.model ? (
                        brand ? (
                          <BrandChip brand={brand} text={log.model} title={log.model} />
                        ) : (
                          <span className="font-medium">{log.model}</span>
                        )
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className="border-r border-border/30 px-3 py-1.5">
                      <EndpointChip endpoint={log.endpoint} />
                    </td>
                    <td className="border-r border-border/30 px-3 py-1.5">
                      {brand ? (
                        <BrandChip
                          brand={brand}
                          text={brand.label}
                          title={log.provider || brand.label}
                        />
                      ) : log.provider ? (
                        <span className="rounded-full border border-border bg-muted px-2 py-0.5 text-[11px] font-semibold uppercase">
                          {log.provider}
                        </span>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                    <td
                      className="max-w-[12rem] truncate border-r border-border/30 px-3 py-1.5 font-sans text-[12px]"
                      title={account.title}
                    >
                      {account.label}
                    </td>
                    <td
                      className="max-w-[12rem] truncate border-r border-border/30 px-3 py-1.5 font-sans text-[12px] text-muted-foreground"
                      title={apiKey.title || (log.apiKeyId ? undefined : t('logs.noApiKey'))}
                    >
                      {log.apiKeyId ? apiKey.label : t('logs.noApiKey')}
                    </td>
                    <td className="border-r border-border/30 px-3 py-1.5 text-right tabular-nums font-medium text-sky-600 dark:text-sky-400">
                      <TokensCell log={log} />
                    </td>
                    <td className="border-r border-border/30 px-3 py-1.5 text-right tabular-nums text-amber-600 dark:text-amber-400">
                      {log.credits ? formatNumber(log.credits) : '—'}
                    </td>
                    <td
                      className={cn(
                        'border-r border-border/30 px-3 py-1.5 text-right tabular-nums font-medium',
                        log.duration >= 30_000
                          ? 'text-destructive'
                          : log.duration >= 10_000
                            ? 'text-amber-600 dark:text-amber-400'
                            : 'text-foreground/80',
                      )}
                    >
                      {formatDuration(log.duration)}
                    </td>
                    <td className="border-r border-border/30 px-3 py-1.5 text-muted-foreground">
                      {dash(log.clientIp)}
                    </td>
                    <td
                      className={cn(
                        'max-w-[14rem] truncate px-3 py-1.5',
                        !ok && 'font-medium text-destructive',
                      )}
                      title={errText || undefined}
                    >
                      {errText || '—'}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </div>
    </Card>
  )
}
