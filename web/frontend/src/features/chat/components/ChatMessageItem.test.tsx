// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeAll, describe, expect, it } from 'vitest'
import en from '@/../locales/en.json'
import i18n from '@/lib/i18n'
import type { ChatMessage } from '@/types/chat'
import { ChatMessageItem } from './ChatMessageItem'

const assistant: ChatMessage = {
  id: 'assistant',
  conversationId: 'conversation',
  parentMessageId: 'user',
  clientRequestId: '',
  role: 'assistant',
  content: '',
  provider: 'kiro',
  model: 'claude',
  status: 'streaming',
  errorCode: '',
  errorMessage: '',
  requestId: '',
  inputTokens: 0,
  outputTokens: 0,
  cacheReadTokens: 0,
  cacheCreationTokens: 0,
  createdAt: 1,
  updatedAt: 1,
}

beforeAll(async () => {
  await i18n.changeLanguage('en')
})

afterEach(cleanup)

describe('ChatMessageItem', () => {
  it('renders an accessible AI Elements loading status before the first delta', () => {
    render(<ChatMessageItem message={assistant} />)

    expect(screen.getByRole('status', {
      name: en['chat.message.generatingAriaLabel'],
    })).toHaveTextContent(en['chat.message.generatingText'])
  })

  it('replaces the loading status when response content arrives', () => {
    render(<ChatMessageItem message={{ ...assistant, content: 'Hello' }} />)

    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.getByText('Hello')).toBeInTheDocument()
  })

  it('does not render the loading status for terminal states', () => {
    render(<ChatMessageItem message={{
      ...assistant,
      status: 'error',
      errorMessage: en['chat.message.failed'],
    }} />)

    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.getByText(en['chat.message.failed'])).toBeInTheDocument()
  })
})
