import { useState } from 'react'
import { Copy, Download, RotateCcw } from 'lucide-react'
import { toast } from 'sonner'
import {
  Attachment,
  AttachmentPreview,
  Attachments,
  type AttachmentData,
} from '@/components/ai-elements/attachments'
import {
  Message,
  MessageAction,
  MessageActions,
  MessageContent,
  MessageResponse,
} from '@/components/ai-elements/message'
import {
  Reasoning,
  ReasoningContent,
  ReasoningTrigger,
} from '@/components/ai-elements/reasoning'
import { Shimmer } from '@/components/ai-elements/shimmer'
import type { ChatAttachment, ChatMessage } from '@/types/chat'

interface ChatMessageItemProps {
  message: ChatMessage
  reasoning?: string
  userPrompt?: string
  onRetry?: (prompt: string, image: boolean) => void
}

function attachmentData(attachment: ChatAttachment): AttachmentData {
  return {
    id: attachment.id,
    type: 'file',
    filename: attachment.name,
    mediaType: attachment.mimeType,
    url: attachment.contentUrl,
  }
}

export function ChatMessageItem({
  message,
  reasoning,
  userPrompt,
  onRetry,
}: ChatMessageItemProps) {
  const [detailsOpen, setDetailsOpen] = useState(false)
  const assistant = message.role === 'assistant'
  const generatedImage = message.attachments?.some(
    (attachment) => attachment.kind === 'image_output',
  ) ?? false

  async function copyPrompt() {
    if (!userPrompt) return
    try {
      await navigator.clipboard.writeText(userPrompt)
      toast.success('Prompt copied')
    } catch {
      toast.error('Could not copy prompt')
    }
  }

  return (
    <Message from={message.role} className="mx-auto w-full max-w-3xl">
      <MessageContent
        className={assistant ? 'w-full' : undefined}
        aria-live={message.status === 'streaming' ? 'polite' : undefined}
      >
        {message.attachments?.length ? (
          <Attachments
            variant="grid"
            className={assistant ? 'mr-auto ml-0 max-w-full' : undefined}
          >
            {message.attachments.map((attachment) => (
              <Attachment
                key={attachment.id}
                data={attachmentData(attachment)}
                className="size-32 sm:size-44"
              >
                <AttachmentPreview />
                {attachment.kind === 'image_output' ? (
                  <a
                    href={attachment.contentUrl}
                    download={attachment.name}
                    className="absolute top-2 right-2 grid size-8 place-items-center rounded-lg bg-background/90 text-foreground opacity-0 shadow-sm backdrop-blur-sm transition-opacity group-hover:opacity-100 focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    aria-label={`Download ${attachment.name}`}
                  >
                    <Download className="size-4" />
                  </a>
                ) : null}
              </Attachment>
            ))}
          </Attachments>
        ) : null}

        {reasoning ? (
          <Reasoning isStreaming={message.status === 'streaming'}>
            <ReasoningTrigger />
            <ReasoningContent>{reasoning}</ReasoningContent>
          </Reasoning>
        ) : null}

        {message.content ? (
          assistant ? (
            <MessageResponse
              mode={message.status === 'streaming' ? 'streaming' : 'static'}
              isAnimating={message.status === 'streaming'}
            >
              {message.content}
            </MessageResponse>
          ) : (
            <div className="whitespace-pre-wrap text-sm">{message.content}</div>
          )
        ) : message.status === 'streaming' ? (
          <Shimmer className="text-sm">Generating…</Shimmer>
        ) : message.status === 'stopped' ? (
          <p className="text-sm text-muted-foreground">Generation stopped</p>
        ) : message.status === 'error' ? (
          <p className="text-sm text-destructive">
            {message.errorMessage || 'Generation failed'}
          </p>
        ) : null}

        {assistant && message.status === 'error' && message.content ? (
          <p className="text-sm text-destructive">
            {message.errorMessage || 'Generation failed'}
          </p>
        ) : null}
      </MessageContent>

      {assistant ? (
        <div className="space-y-1">
          <MessageActions>
            {userPrompt ? (
              <MessageAction
                tooltip="Retry"
                label="Retry response"
                onClick={() => onRetry?.(userPrompt, generatedImage)}
              >
                <RotateCcw />
              </MessageAction>
            ) : null}
            {generatedImage && userPrompt ? (
              <MessageAction tooltip="Copy prompt" onClick={copyPrompt}>
                <Copy />
              </MessageAction>
            ) : null}
            <button
              type="button"
              className="rounded-md px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              aria-expanded={detailsOpen}
              onClick={() => setDetailsOpen((open) => !open)}
            >
              Details
            </button>
          </MessageActions>
          {detailsOpen ? (
            <div className="rounded-lg border bg-muted/30 px-3 py-2 font-mono text-[11px] leading-5 text-muted-foreground">
              <div>{message.provider} · {message.model} · {message.status}</div>
              <div>Input {message.inputTokens} · Output {message.outputTokens}</div>
              <div>Cache read {message.cacheReadTokens} · Cache write {message.cacheCreationTokens}</div>
              {message.requestId ? <div className="break-all">Request {message.requestId}</div> : null}
            </div>
          ) : null}
        </div>
      ) : null}
    </Message>
  )
}
