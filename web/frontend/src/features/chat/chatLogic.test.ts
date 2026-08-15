import { describe, expect, it } from 'vitest'
import type { ChatMessage } from '@/types/chat'
import { createOptimisticChatTurn, failChatStream, pendingChatMessages, reconcileChatMessages, reduceChatStream, setChatTurnConversation, validateChatUploads } from './chatLogic'

function file(name: string, type: string, size = 1) {
  return new File([new Uint8Array(size)], name, { type })
}

const user: ChatMessage = {
  id: 'local-user', conversationId: 'conversation', parentMessageId: '', clientRequestId: '', role: 'user', content: 'hello',
  provider: '', model: '', status: 'complete', errorCode: '', errorMessage: '', requestId: '', inputTokens: 0,
  outputTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0, createdAt: 1, updatedAt: 1,
}
const message: ChatMessage = {
  id: 'local-assistant', conversationId: 'conversation', parentMessageId: user.id, clientRequestId: '', role: 'assistant', content: '',
  provider: '', model: '', status: 'streaming', errorCode: '', errorMessage: '', requestId: '', inputTokens: 0,
  outputTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0, createdAt: 1, updatedAt: 1,
}
const initialState = () => ({ user, message, reasoning: '', done: false, persistedIds: false })

describe('validateChatUploads', () => {
  it('accepts supported images within the four-file limit', () => {
    const result = validateChatUploads([file('one.png', 'image/png')], [file('two.jpg', 'image/jpeg'), file('three.webp', 'image/webp')])
    expect(result.rejected).toBeNull()
    expect(result.accepted).toHaveLength(3)
  })

  it.each([
    [file('empty.png', 'image/png', 0), 'empty'],
    [file('bad.gif', 'image/gif'), 'unsupported'],
    [file('large.png', 'image/png', 10 * 1024 * 1024 + 1), 'oversized'],
  ])('rejects %s images', (invalid) => {
    expect(validateChatUploads([], [invalid]).rejected).toBe('invalid_image')
  })

  it('rejects the complete batch instead of silently dropping overflow', () => {
    const current = Array.from({ length: 3 }, (_, index) => file(`${index}.png`, 'image/png'))
    expect(validateChatUploads(current, [file('four.png', 'image/png'), file('five.png', 'image/png')])).toEqual({ accepted: [], rejected: 'too_many' })
  })
})

describe('chat stream state', () => {
  it('creates an ordered optimistic turn before persistence and updates its conversation id', () => {
    const ids = ['local-user', 'local-assistant']
    const state = createOptimisticChatTurn({
      conversationId: 'local-conversation',
      content: 'hello now',
      provider: 'kiro',
      model: 'claude',
      now: 42,
      createId: () => ids.shift()!,
    })

    expect([state.user, state.message]).toMatchObject([
      { id: 'local-user', role: 'user', content: 'hello now', status: 'complete', conversationId: 'local-conversation' },
      { id: 'local-assistant', role: 'assistant', parentMessageId: 'local-user', status: 'streaming', conversationId: 'local-conversation' },
    ])

    const persistedConversation = setChatTurnConversation(state, 'conversation')
    expect(persistedConversation.user).toMatchObject({ id: 'local-user', content: 'hello now', conversationId: 'conversation' })
    expect(persistedConversation.message).toMatchObject({ id: 'local-assistant', parentMessageId: 'local-user', conversationId: 'conversation' })
  })

  it('reconciles both ids and reduces reasoning, deltas, usage, and done', () => {
    let state = initialState()
    state = reduceChatStream(state, { event: 'generation.created', data: { generationId: 'g', userMessageId: 'user', assistantMessageId: 'assistant' } })
    state = reduceChatStream(state, { event: 'response.reasoning_summary.delta', data: { delta: 'think' } })
    state = reduceChatStream(state, { event: 'response.delta', data: { delta: 'hello' } })
    state = reduceChatStream(state, { event: 'response.completed', data: { finishReason: 'stop', provider: 'kiro', model: 'claude', usage: { inputTokens: 2, outputTokens: 1, cacheReadTokens: 1, cacheCreationTokens: 0 } } })
    state = reduceChatStream(state, { event: 'done', data: {} })
    expect(state.user.id).toBe('user')
    expect(state.message).toMatchObject({ id: 'assistant', parentMessageId: 'user', content: 'hello', status: 'complete', provider: 'kiro', inputTokens: 2 })
    expect(state.reasoning).toBe('think')
    expect(state.done).toBe(true)
    expect(state.persistedIds).toBe(true)
  })

  it('deduplicates each pending message independently', () => {
    const state = reduceChatStream(initialState(), { event: 'generation.created', data: { generationId: 'g', userMessageId: 'user', assistantMessageId: 'assistant' } })
    expect(pendingChatMessages([{ ...state.user }], state).map((item) => item.id)).toEqual(['assistant'])
    expect(pendingChatMessages([{ ...state.user }, { ...state.message }], state)).toEqual([])
  })

  it('keeps the active user turn before its assistant while persistence catches up', () => {
    const state = reduceChatStream(initialState(), { event: 'generation.created', data: { generationId: 'g', userMessageId: 'user', assistantMessageId: 'assistant' } })
    const previous = { ...user, id: 'previous', content: 'previous turn' }

    expect(reconcileChatMessages([], state)).toEqual([
      state.user,
      state.message,
    ])
    expect(reconcileChatMessages([previous, { ...state.message, content: 'reply' }], state))
      .toEqual([previous, state.user, { ...state.message, content: 'reply' }])
    expect(reconcileChatMessages([previous, state.user], state))
      .toEqual([previous, state.user, state.message])
  })

  it('reconciles persisted turn records without duplicating them', () => {
    const state = reduceChatStream(initialState(), { event: 'generation.created', data: { generationId: 'g', userMessageId: 'user', assistantMessageId: 'assistant' } })
    const persistedUser = { ...state.user, content: 'persisted user' }
    const persistedAssistant = { ...state.message, content: 'persisted assistant' }

    expect(reconcileChatMessages([persistedAssistant, persistedUser], state))
      .toEqual([persistedUser, persistedAssistant])
  })

  it('preserves stopped and interrupted terminal states', () => {
    expect(failChatStream(initialState(), true, 'ignored').message).toMatchObject({ status: 'stopped', errorCode: 'generation_cancelled' })
    expect(failChatStream(initialState(), false, 'Disconnected').message).toMatchObject({ status: 'error', errorCode: 'stream_interrupted', errorMessage: 'Disconnected' })
  })
})
