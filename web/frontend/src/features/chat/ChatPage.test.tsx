// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import en from '@/../locales/en.json'
import i18n from '@/lib/i18n'
import type { ChatConversation, ChatModel } from '@/types/chat'
import { ConfirmDialogHost } from '@/components/shared/ConfirmDialog'
import { TooltipProvider } from '@/components/ui/tooltip'
import ChatPage from './ChatPage'

const { conversation, model } = vi.hoisted(() => {
  const conversation: ChatConversation = {
    id: 'conversation-1',
    title: 'My conversation',
    provider: 'kiro',
    model: 'claude',
    mode: 'chat',
    status: 'active',
    pinned: false,
    projectId: '',
    createdAt: 1,
    updatedAt: 1,
    archivedAt: null,
  }

  const model: ChatModel = {
    id: 'kiro:claude',
    provider: 'kiro',
    model: 'claude',
    displayName: 'Claude',
    capabilities: { vision: true, imageGeneration: false },
  } as ChatModel

  return { conversation, model }
})

vi.mock('@/hooks/queries/useChat', () => ({
  useChatModels: () => ({ data: [model], isLoading: false }),
  useChatConversations: () => ({ data: { data: [conversation] }, isLoading: false }),
  useChatMessages: () => ({ data: { data: [] }, isLoading: false, isError: false }),
}))

vi.mock('@/services/chat.service', () => ({
  chatService: {
    deleteConversation: vi.fn().mockResolvedValue(undefined),
    updateConversation: vi.fn().mockResolvedValue(conversation),
    createConversation: vi.fn().mockResolvedValue(conversation),
  },
}))

class ResizeObserverMock {
  observe() {}
  unobserve() {}
  disconnect() {}
}

vi.stubGlobal('ResizeObserver', ResizeObserverMock)
window.HTMLElement.prototype.scrollIntoView = () => {}

beforeAll(async () => {
  await i18n.changeLanguage('en')
})

afterEach(cleanup)

function renderChatPage() {
  const queryClient = new QueryClient()
  return render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <ChatPage />
        <ConfirmDialogHost />
      </TooltipProvider>
    </QueryClientProvider>,
  )
}

describe('ChatPage delete confirmation', () => {
  it('shows the localized delete-confirmation dialog title and description', () => {
    renderChatPage()

    fireEvent.click(screen.getByRole('button', { name: en['chat.sidebar.delete'] }))

    expect(screen.getByText(en['chat.page.deleteTitle'])).toBeInTheDocument()
    expect(screen.getByText(en['chat.page.deleteDescription'])).toBeInTheDocument()
  })
})
