import type { ChatMessage, ChatStreamEvent } from '@/types/chat'

export const chatUploadMaxFiles = 4
export const chatUploadMaxBytes = 10 * 1024 * 1024
const chatUploadMIMEs = new Set(['image/png', 'image/jpeg', 'image/webp'])

export interface ChatUploadValidation {
  accepted: File[]
  rejected: 'too_many' | 'invalid_image' | null
}

export function validateChatUploads(current: File[], incoming: File[]): ChatUploadValidation {
  if (current.length + incoming.length > chatUploadMaxFiles) {
    return { accepted: [], rejected: 'too_many' }
  }
  if (incoming.some((file) => file.size === 0 || file.size > chatUploadMaxBytes || !chatUploadMIMEs.has(file.type))) {
    return { accepted: [], rejected: 'invalid_image' }
  }
  return { accepted: [...current, ...incoming], rejected: null }
}

export interface ChatStreamState {
  user: ChatMessage
  message: ChatMessage
  reasoning: string
  done: boolean
  persistedIds: boolean
}

interface OptimisticChatTurnInput {
  conversationId: string
  content: string
  provider: string
  model: string
  now: number
  createId: () => string
}

export function createOptimisticChatTurn({
  conversationId,
  content,
  provider,
  model,
  now,
  createId,
}: OptimisticChatTurnInput): ChatStreamState {
  const userId = createId()
  const base = {
    conversationId,
    clientRequestId: '',
    provider,
    model,
    errorCode: '',
    errorMessage: '',
    requestId: '',
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
    createdAt: now,
    updatedAt: now,
  }

  return {
    user: {
      ...base,
      id: userId,
      parentMessageId: '',
      role: 'user',
      content,
      status: 'complete',
    },
    message: {
      ...base,
      id: createId(),
      parentMessageId: userId,
      role: 'assistant',
      content: '',
      status: 'streaming',
    },
    reasoning: '',
    done: false,
    persistedIds: false,
  }
}

export function setChatTurnConversation(
  state: ChatStreamState,
  conversationId: string,
): ChatStreamState {
  return {
    ...state,
    user: { ...state.user, conversationId },
    message: { ...state.message, conversationId },
  }
}

export function pendingChatMessages(transcript: ChatMessage[], state: ChatStreamState | null) {
  if (!state) return []
  const ids = new Set(transcript.map((message) => message.id))
  return [state.user, state.message].filter((message) => !ids.has(message.id))
}

export function reconcileChatMessages(
  transcript: ChatMessage[],
  state: ChatStreamState | null,
) {
  if (!state) return transcript

  const turnIds = new Set([state.user.id, state.message.id])
  const persisted = new Map(
    transcript
      .filter((message) => turnIds.has(message.id))
      .map((message) => [message.id, message]),
  )

  return [
    ...transcript.filter((message) => !turnIds.has(message.id)),
    persisted.get(state.user.id) ?? state.user,
    persisted.get(state.message.id) ?? state.message,
  ]
}

export function failChatStream(state: ChatStreamState, stopped: boolean, message: string): ChatStreamState {
  return {
    ...state,
    message: {
      ...state.message,
      status: stopped ? 'stopped' : 'error',
      errorCode: stopped ? 'generation_cancelled' : 'stream_interrupted',
      errorMessage: stopped ? 'Generation stopped' : message,
    },
  }
}

export function reduceChatStream(state: ChatStreamState, event: ChatStreamEvent): ChatStreamState {
  switch (event.event) {
    case 'generation.created':
      return {
        ...state,
        persistedIds: true,
        user: { ...state.user, id: event.data.userMessageId },
        message: { ...state.message, id: event.data.assistantMessageId, parentMessageId: event.data.userMessageId },
      }
    case 'response.delta':
      return { ...state, message: { ...state.message, content: state.message.content + event.data.delta } }
    case 'response.reasoning_summary.delta':
      return { ...state, reasoning: state.reasoning + event.data.delta }
    case 'response.completed':
      return { ...state, message: { ...state.message, status: 'complete', provider: event.data.provider, model: event.data.model, ...event.data.usage } }
    case 'response.error':
      return { ...state, message: { ...state.message, status: 'error', errorCode: event.data.code, errorMessage: event.data.message } }
    case 'done':
      return { ...state, done: true }
  }
}
