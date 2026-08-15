import { Bot } from 'lucide-react'
import {
  Conversation,
  ConversationContent,
  ConversationEmptyState,
  ConversationScrollButton,
} from '@/components/ai-elements/conversation'
import type { ChatMessage } from '@/types/chat'
import type { ChatStreamState } from '../chatLogic'
import { ChatMessageItem } from './ChatMessageItem'

interface ImageGenerationTurn {
  prompt: string
  provider: string
  model: string
  size: string
  quality: string
  user: ChatMessage
  assistant: ChatMessage
}

interface ChatTranscriptProps {
  messages: ChatMessage[]
  streamState: ChatStreamState | null
  imageGeneration: ImageGenerationTurn | null
  loading: boolean
  error: boolean
  onRetry: (prompt: string, image: boolean) => void
  onRetryImage: (turn: ImageGenerationTurn) => void
}

export function ChatTranscript({
  messages,
  streamState,
  imageGeneration,
  loading,
  error,
  onRetry,
  onRetryImage,
}: ChatTranscriptProps) {
  const empty = !messages.length && !imageGeneration
  const streamingAssistantId = streamState?.message.id

  return (
    <Conversation className="min-h-0 bg-background">
      <ConversationContent className="mx-auto min-h-full w-full max-w-4xl gap-7 px-4 py-8 sm:px-8">
        {loading && empty ? (
          <ConversationEmptyState
            icon={<Bot className="size-9 animate-pulse" />}
            title="Loading conversation"
            description="Retrieving messages from your chat history."
          />
        ) : error ? (
          <ConversationEmptyState
            icon={<Bot className="size-9" />}
            title="Messages could not be loaded"
            description="Refresh the page or choose another conversation."
          />
        ) : empty ? (
          <ConversationEmptyState
            icon={(
              <div className="grid size-14 place-items-center rounded-2xl bg-primary/10 text-primary ring-1 ring-primary/20">
                <Bot className="size-7" />
              </div>
            )}
            title="Start a new signal"
            description="Choose a provider and model, then ask a question or create an image."
          />
        ) : (
          <>
            {messages.map((message, index) => (
              <ChatMessageItem
                key={message.id}
                message={message}
                reasoning={
                  message.id === streamingAssistantId
                    ? streamState?.reasoning
                    : undefined
                }
                userPrompt={
                  message.role === 'assistant'
                    ? messages
                      .slice(0, index)
                      .findLast((candidate) => candidate.role === 'user')
                      ?.content
                    : undefined
                }
                onRetry={onRetry}
              />
            ))}
            {imageGeneration ? (
              <>
                <ChatMessageItem message={imageGeneration.user} />
                <ChatMessageItem
                  message={imageGeneration.assistant}
                  userPrompt={imageGeneration.prompt}
                  onRetry={() => onRetryImage(imageGeneration)}
                />
              </>
            ) : null}
          </>
        )}
      </ConversationContent>
      <ConversationScrollButton className="bottom-5 z-10 shadow-md" />
    </Conversation>
  )
}
