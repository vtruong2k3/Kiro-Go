import { useEffect, useMemo, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { useChatConversations, useChatMessages, useChatModels } from '@/hooks/queries/useChat'
import { chatService } from '@/services/chat.service'
import { qk } from '@/config/queryKeys'
import type { ChatMessage, ChatStreamEvent } from '@/types/chat'
import { Sheet, SheetContent } from '@/components/ui/sheet'
import { useConfirm } from '@/components/shared/ConfirmDialog'
import { ChatTranscript } from './components/ChatTranscript'
import { ChatComposer } from './components/ChatComposer'
import { ChatConversationSidebar } from './components/ChatConversationSidebar'
import { ChatHeader } from './components/ChatHeader'
import { chatExportJSON, chatExportMarkdown, downloadChatExport } from './chatExport'
import { failChatStream, pendingChatMessages, reduceChatStream, validateChatUploads, type ChatStreamState } from './chatLogic'

interface PendingImageGeneration {
  prompt: string
  provider: string
  model: string
  size: string
  quality: string
  user: ChatMessage
  assistant: ChatMessage
}

function requestId() {
  return crypto.randomUUID()
}

export default function ChatPage() {
  const queryClient = useQueryClient()
  const confirm = useConfirm()
  const models = useChatModels()
  const [conversationStatus, setConversationStatus] = useState<'active' | 'archived'>('active')
  const [search, setSearch] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const conversations = useChatConversations(conversationStatus, debouncedSearch)
  const [activeId, setActiveId] = useState('')
  const messages = useChatMessages(activeId)
  const [selectedModel, setSelectedModel] = useState('')
  const [draft, setDraft] = useState('')
  const [streamState, setStreamState] = useState<ChatStreamState | null>(null)
  const [pendingImages, setPendingImages] = useState<File[]>([])
  const [imageMode, setImageMode] = useState(false)
  const [imageSize, setImageSize] = useState('auto')
  const [imageQuality, setImageQuality] = useState('auto')
  const [uploading, setUploading] = useState(false)
  const [imageGeneration, setImageGeneration] = useState<PendingImageGeneration | null>(null)
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false)
  const controller = useRef<AbortController | null>(null)

  useEffect(() => {
    const timeout = window.setTimeout(() => setDebouncedSearch(search), 250)
    return () => window.clearTimeout(timeout)
  }, [search])

  useEffect(() => {
    if (!activeId && conversations.data?.data.length) setActiveId(conversations.data.data[0].id)
  }, [activeId, conversations.data])

  useEffect(() => {
    const conversation = conversations.data?.data.find((item) => item.id === activeId)
    if (conversation) setSelectedModel(`${conversation.provider}:${conversation.model}`)
  }, [activeId, conversations.data])

  useEffect(() => () => controller.current?.abort(), [])

  const transcript = useMemo(() => messages.data?.data ?? [], [messages.data])
  const pendingStreamMessages = useMemo(() => pendingChatMessages(transcript, streamState), [transcript, streamState])
  const activeConversation = conversations.data?.data.find((item) => item.id === activeId)

  useEffect(() => {
    if (streamState?.persistedIds && pendingStreamMessages.length === 0) setStreamState(null)
  }, [pendingStreamMessages.length, streamState])

  useEffect(() => {
    if (!imageGeneration) return
    const ids = new Set(transcript.map((message) => message.id))
    if (ids.has(imageGeneration.user.id) && ids.has(imageGeneration.assistant.id)) {
      setImageGeneration(null)
    }
  }, [imageGeneration, transcript])

  async function renameConversation() {
    if (!activeConversation) return
    const title = window.prompt('Conversation title', activeConversation.title)
    if (title === null || !title.trim() || title.trim() === activeConversation.title) return
    await chatService.updateConversation(activeConversation.id, { title: title.trim() })
    await queryClient.invalidateQueries({ queryKey: qk.chatConversations })
  }

  function exportConversation(format: 'markdown' | 'json') {
    if (!activeConversation) return
    const stem = (activeConversation.title || 'conversation').replace(/[^a-z0-9_-]+/gi, '-').replace(/^-|-$/g, '') || 'conversation'
    if (format === 'json') {
      downloadChatExport(`${stem}.json`, chatExportJSON(activeConversation, transcript), 'application/json;charset=utf-8')
      return
    }
    downloadChatExport(`${stem}.md`, chatExportMarkdown(activeConversation, transcript), 'text/markdown;charset=utf-8')
  }

  async function createConversation() {
    const model = models.data?.find((item) => item.id === selectedModel) ?? models.data?.[0]
    if (!model) return toast.error('No chat model is available')
    const created = await chatService.createConversation({ provider: model.provider, model: model.model })
    await queryClient.invalidateQueries({ queryKey: qk.chatConversations })
    setActiveId(created.id)
    setSelectedModel(model.id)
  }

  async function removeConversation(id: string) {
    const accepted = await confirm({ title: 'Delete conversation?', description: 'Messages and stored images will be permanently deleted.', confirmLabel: 'Delete', destructive: true })
    if (!accepted) return
    await chatService.deleteConversation(id)
    if (activeId === id) setActiveId('')
    await queryClient.invalidateQueries({ queryKey: qk.chatConversations })
  }

  function addImages(files: File[]) {
    const model = models.data?.find((item) => item.id === selectedModel)
    if (model && !model.capabilities.vision) {
      toast.error('The selected model does not support image input')
      return
    }
    setPendingImages((current) => {
      const result = validateChatUploads(current, files)
      if (result.rejected === 'too_many') toast.error('You can attach at most four images')
      if (result.rejected === 'invalid_image') toast.error('Use non-empty PNG, JPEG, or WebP images up to 10 MiB')
      return result.rejected ? current : result.accepted
    })
  }

  async function send() {
    const content = draft.trim()
    if ((!content && !pendingImages.length) || controller.current) return
    let conversationId = activeId
    const model = models.data?.find((item) => item.id === selectedModel)
    if (imageMode && !model) return toast.error('Select an image generation model first')
    if (pendingImages.length && model && !model.capabilities.vision) {
      toast.error('The selected model does not support image input')
      return
    }
    if (!conversationId) {
      if (!model) return toast.error('Select a model first')
      const created = await chatService.createConversation({ provider: model.provider, model: model.model })
      conversationId = created.id
      setActiveId(created.id)
    } else if (model) {
      await chatService.updateConversation(conversationId, { provider: model.provider, model: model.model })
    }

    const abort = new AbortController()
    controller.current = abort
    if (imageMode) {
      if (!model) {
        controller.current = null
        return toast.error('Select an image generation model first')
      }
      const now = Date.now()
      const userId = requestId()
      const pending: PendingImageGeneration = {
        prompt: content, provider: model.provider, model: model.model, size: imageSize, quality: imageQuality,
        user: {
          id: userId, conversationId, parentMessageId: '', clientRequestId: '', role: 'user', content,
          provider: model.provider, model: model.model, status: 'complete', errorCode: '', errorMessage: '', requestId: '',
          inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0, createdAt: now, updatedAt: now,
        },
        assistant: {
          id: requestId(), conversationId, parentMessageId: userId, clientRequestId: '', role: 'assistant', content: '',
          provider: model.provider, model: model.model, status: 'streaming', errorCode: '', errorMessage: '', requestId: '',
          inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0, createdAt: now, updatedAt: now,
        },
      }
      setImageGeneration(pending)
      setUploading(true)
      setDraft('')
      try {
        const result = await chatService.generateImage(conversationId, {
          clientRequestId: requestId(), prompt: content, provider: model.provider, model: model.model,
          size: imageSize, quality: imageQuality,
        }, abort.signal)
        setImageGeneration({ ...pending, user: result.userMessage, assistant: { ...result.assistantMessage, attachments: result.attachments } })
      } catch (error) {
        const stopped = abort.signal.aborted
        setImageGeneration({ ...pending, assistant: {
          ...pending.assistant, status: stopped ? 'stopped' : 'error', errorCode: stopped ? 'generation_cancelled' : 'image_generation_failed',
          errorMessage: stopped ? 'Image generation stopped' : error instanceof Error ? error.message : 'Image generation failed',
        } })
        setDraft(content)
        if (!stopped) toast.error(error instanceof Error ? error.message : 'Image generation failed')
      } finally {
        controller.current = null
        setUploading(false)
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: qk.chatMessages(conversationId) }),
          queryClient.invalidateQueries({ queryKey: qk.chatConversations }),
        ])
      }
      return
    }
    setUploading(Boolean(pendingImages.length))
    let attachmentIds: string[] = []
    try {
      if (pendingImages.length) {
        const uploaded = await chatService.uploadAttachments(conversationId, pendingImages)
        attachmentIds = uploaded.map((attachment) => attachment.id)
      }
    } catch (error) {
      controller.current = null
      setUploading(false)
      toast.error(error instanceof Error ? error.message : 'Upload failed')
      return
    }
    setUploading(false)
    setDraft('')
    setPendingImages([])
    const now = Date.now()
    const userId = requestId()
    const userMessage: ChatMessage = {
      id: userId, conversationId, parentMessageId: '', clientRequestId: '', role: 'user', content,
      provider: model?.provider ?? '', model: model?.model ?? '', status: 'complete', errorCode: '', errorMessage: '',
      requestId: '', inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0,
      createdAt: now, updatedAt: now,
    }
    const initialMessage: ChatMessage = {
      id: requestId(), conversationId, parentMessageId: userId, clientRequestId: '', role: 'assistant', content: '',
      provider: model?.provider ?? '', model: model?.model ?? '', status: 'streaming', errorCode: '', errorMessage: '',
      requestId: '', inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0,
      createdAt: now, updatedAt: now,
    }
    setStreamState({ user: userMessage, message: initialMessage, reasoning: '', done: false, persistedIds: false })
    try {
      await chatService.generate(conversationId, { clientRequestId: requestId(), content, attachmentIds }, abort.signal, (event: ChatStreamEvent) => {
        setStreamState((current) => current ? reduceChatStream(current, event) : current)
      })
    } catch (error) {
      const stopped = abort.signal.aborted
      const message = error instanceof Error ? error.message : 'Generation failed'
      setStreamState((current) => current ? failChatStream(current, stopped, message) : current)
      if (!stopped) toast.error(message)
    } finally {
      controller.current = null
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: qk.chatMessages(conversationId) }),
        queryClient.invalidateQueries({ queryKey: qk.chatConversations }),
      ])
    }
  }

  const sidebar = (
    <ChatConversationSidebar
      conversations={conversations.data?.data ?? []}
      activeId={activeId}
      status={conversationStatus}
      search={search}
      loading={conversations.isLoading}
      className="h-full"
      onCreate={createConversation}
      onSelect={(id) => {
        setActiveId(id)
        setMobileSidebarOpen(false)
      }}
      onStatusChange={(status) => {
        setConversationStatus(status)
        setActiveId('')
      }}
      onSearchChange={setSearch}
      onTogglePin={(conversation) => {
        void chatService
          .updateConversation(conversation.id, { pinned: !conversation.pinned })
          .then(() => queryClient.invalidateQueries({ queryKey: qk.chatConversations }))
      }}
      onToggleArchive={(conversation) => {
        void chatService
          .updateConversation(conversation.id, {
            status: conversation.status === 'active' ? 'archived' : 'active',
          })
          .then(() => {
            if (activeId === conversation.id) setActiveId('')
            return queryClient.invalidateQueries({ queryKey: qk.chatConversations })
          })
      }}
      onDelete={(id) => { void removeConversation(id) }}
    />
  )

  return (
    <div className="flex h-[calc(100dvh-7rem)] min-h-[32rem] overflow-hidden rounded-2xl border bg-background shadow-sm">
      <div className="hidden w-72 shrink-0 border-r border-sidebar-border md:block">
        {sidebar}
      </div>
      <Sheet open={mobileSidebarOpen} onOpenChange={setMobileSidebarOpen}>
        <SheetContent side="left" className="w-[min(20rem,calc(100%-2rem))] gap-0 p-0" showCloseButton={false}>
          {sidebar}
        </SheetContent>
      </Sheet>

      <section className="flex min-w-0 flex-1 flex-col">
        <ChatHeader
          conversation={activeConversation}
          models={models.data ?? []}
          selectedModel={selectedModel}
          imageMode={imageMode}
          imageSize={imageSize}
          imageQuality={imageQuality}
          onOpenSidebar={() => setMobileSidebarOpen(true)}
          onRename={() => { void renameConversation() }}
          onExport={exportConversation}
          onToggleImageMode={() => {
            const next = !imageMode
            setImageMode(next)
            setPendingImages([])
            if (next) {
              const imageModel = models.data?.find((item) => item.capabilities.imageGeneration)
              if (imageModel) setSelectedModel(imageModel.id)
              else toast.error('No image generation model is available')
            }
          }}
          onModelChange={setSelectedModel}
          onImageSizeChange={setImageSize}
          onImageQualityChange={setImageQuality}
        />

        <ChatTranscript
          transcript={transcript}
          pendingStreamMessages={pendingStreamMessages}
          streamState={streamState}
          imageGeneration={imageGeneration}
          loading={messages.isLoading}
          error={messages.isError}
          onRetry={(prompt, image) => {
            setDraft(prompt)
            setImageMode(image)
          }}
          onRetryImage={(turn) => {
            setDraft(turn.prompt)
            setSelectedModel(`${turn.provider}:${turn.model}`)
            setImageSize(turn.size)
            setImageQuality(turn.quality)
            setImageMode(true)
          }}
        />

        <ChatComposer
          draft={draft}
          pendingImages={pendingImages}
          imageMode={imageMode}
          busy={Boolean(controller.current)}
          uploading={uploading}
          selectedModel={models.data?.find((item) => item.id === selectedModel)}
          onDraftChange={setDraft}
          onAddImages={addImages}
          onRemoveImage={(index) => setPendingImages((current) => current.filter((_, itemIndex) => itemIndex !== index))}
          onSubmit={async () => { await send() }}
          onStop={() => controller.current?.abort()}
        />
      </section>
    </div>
  )
}
