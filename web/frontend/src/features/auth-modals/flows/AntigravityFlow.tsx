// Antigravity OAuth — loopback poll + manual-paste fallback. No args to start.
import { useOAuthFlow } from '@/hooks/useOAuthFlow'
import {
  startAntigravity,
  pollAntigravity,
  completeAntigravity,
  cancelAntigravity,
} from '@/services/authFlows.service'
import { Button } from '@/components/ui/button'
import { OAuthFlowView } from '../OAuthFlowView'
import type { FlowComponentProps } from './types'

export function AntigravityFlow({ onDone }: FlowComponentProps) {
  const flow = useOAuthFlow<void>({
    start: startAntigravity,
    poll: pollAntigravity,
    complete: completeAntigravity,
    cancel: cancelAntigravity,
  })

  if (flow.state.phase === 'idle') {
    return (
      <div className="space-y-3">
        <p className="text-sm text-muted-foreground">
          Open Google sign-in to connect your Antigravity account. If the callback
          cannot reach this server, the dialog will provide a manual fallback.
        </p>
        <Button
          className="w-full"
          onClick={() => {
            // Open synchronously from the user gesture so popup blockers do not
            // reject the tab while the server creates the OAuth session.
            const popup = window.open('', '_blank')
            void flow.start(undefined, popup)
          }}
        >
          Start Antigravity login
        </Button>
      </div>
    )
  }

  return <OAuthFlowView flow={flow} onDone={onDone} allowManual />
}
