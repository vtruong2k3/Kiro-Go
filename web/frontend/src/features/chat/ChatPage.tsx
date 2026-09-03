import { useEffect, useMemo, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { useChatConversations, useChatMessages, useChatModels } from '@/hooks/queries/useChat'
import { chatService } from '@/services/chat.service'
import { qk } from '@/config/queryKeys'
import type { ChatMessage, ChatStreamEvent } from '@/types/chat'
import { Sheet, SheetContent } from '@/components/ui/sheet'
import { useConfirm } from '@/components/shared/ConfirmDialog'
import { t } from '@/lib/t'
import { ChatTranscript } from './components/ChatTranscript'
import { ChatComposer } from './components/ChatComposer'
import { ChatConversationSidebar } from './components/ChatConversationSidebar'
import { ChatHeader } from './components/ChatHeader'
import { chatExportJSON, chatExportMarkdown, downloadChatExport } from './chatExport'
import { createOptimisticChatTurn, failChatStream, pendingChatMessages, reconcileChatMessages, reduceChatStream, setChatTurnConversation, validateChatUploads, type ChatStreamState } from './chatLogic'

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
  const renderedMessages = useMemo(
    () => reconcileChatMessages(transcript, streamState),
    [transcript, streamState],
  )
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
    const title = window.prompt(t('chat.page.renamePromptTitle'), activeConversation.title)
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
    if (!model) return toast.error(t('chat.page.noChatModel'))
    const created = await chatService.createConversation({ provider: model.provider, model: model.model })
    await queryClient.invalidateQueries({ queryKey: qk.chatConversations })
    setActiveId(created.id)
    setSelectedModel(model.id)
  }

  async function removeConversation(id: string) {
    const accepted = await confirm({
      title: t('chat.page.deleteTitle'),
      description: t('chat.page.deleteDescription'),
      confirmLabel: t('common.delete'),
      destructive: true,
    })
    if (!accepted) return
    await chatService.deleteConversation(id)
    if (activeId === id) setActiveId('')
    await queryClient.invalidateQueries({ queryKey: qk.chatConversations })
  }

  function addImages(files: File[]) {
    const model = models.data?.find((item) => item.id === selectedModel)
    if (model && !model.capabilities.vision) {
      toast.error(t('chat.page.noImageInput'))
      return
    }
    setPendingImages((current) => {
      const result = validateChatUploads(current, files)
      if (result.rejected === 'too_many') toast.error(t('chat.page.maxImages'))
      if (result.rejected === 'invalid_image') toast.error(t('chat.page.invalidImages'))
      return result.rejected ? current : result.accepted
    })
  }

  async function send() {
    const content = draft.trim()
    const imageFiles = [...pendingImages]
    if ((!content && !imageFiles.length) || controller.current) return

    const model = models.data?.find((item) => item.id === selectedModel)
    if (imageMode && !model) return toast.error(t('chat.page.selectImageModel'))
    if (imageFiles.length && model && !model.capabilities.vision) {
      toast.error(t('chat.page.noImageInput'))
      return
    }
    if (!activeId && !model) return toast.error(t('chat.page.selectModelFirst'))

    const abort = new AbortController()
    controller.current = abort
    const temporaryConversationId = activeId || `local-${requestId()}`
    const optimistic = createOptimisticChatTurn({
      conversationId: temporaryConversationId,
      content,
      provider: model?.provider ?? '',
      model: model?.model ?? '',
      now: Date.now(),
      createId: requestId,
    })

    if (imageMode && model) {
      setImageGeneration({
        prompt: content,
        provider: model.provider,
        model: model.model,
        size: imageSize,
        quality: imageQuality,
        user: optimistic.user,
        assistant: optimistic.message,
      })
    } else {
      setStreamState(optimistic)
    }
    setDraft('')
    setPendingImages([])
    setUploading(Boolean(imageMode || imageFiles.length))

    let conversationId = activeId
    let generationStarted = false

    function restoreInput() {
      setDraft((current) => current || content)
      if (!imageFiles.length) return
      setPendingImages((current) => {
        const restored = validateChatUploads(current, imageFiles)
        return restored.rejected ? current : restored.accepted
      })
    }

    try {
      if (!conversationId) {
        if (!model) throw new Error(t('chat.page.selectModelFirst'))
        const created = await chatService.createConversation({
          provider: model.provider,
          model: model.model,
        })
        conversationId = created.id
        setActiveId(created.id)
        if (imageMode) {
          setImageGeneration((current) => current ? {
            ...current,
            user: { ...current.user, conversationId: created.id },
            assistant: { ...current.assistant, conversationId: created.id },
          } : current)
        } else {
          setStreamState((current) => current
            ? setChatTurnConversation(current, created.id)
            : current)
        }
      } else if (model) {
        await chatService.updateConversation(conversationId, {
          provider: model.provider,
          model: model.model,
        })
      }

      if (abort.signal.aborted) throw new DOMException(t('chat.page.generationStopped'), 'AbortError')

      if (imageMode) {
        if (!model) throw new Error(t('chat.page.selectImageModel'))
        generationStarted = true
        const result = await chatService.generateImage(conversationId, {
          clientRequestId: requestId(),
          prompt: content,
          provider: model.provider,
          model: model.model,
          size: imageSize,
          quality: imageQuality,
        }, abort.signal)
        setImageGeneration((current) => current ? {
          ...current,
          user: result.userMessage,
          assistant: {
            ...result.assistantMessage,
            attachments: result.attachments,
          },
        } : current)
        return
      }

      let attachmentIds: string[] = []
      if (imageFiles.length) {
        const uploaded = await chatService.uploadAttachments(conversationId, imageFiles)
        attachmentIds = uploaded.map((attachment) => attachment.id)
      }
      if (abort.signal.aborted) throw new DOMException(t('chat.page.generationStopped'), 'AbortError')

      setUploading(false)
      generationStarted = true
      await chatService.generate(
        conversationId,
        { clientRequestId: requestId(), content, attachmentIds },
        abort.signal,
        (event: ChatStreamEvent) => {
          setStreamState((current) => current ? reduceChatStream(current, event) : current)
        },
      )
    } catch (error) {
      const stopped = abort.signal.aborted
      const message = error instanceof Error ? error.message : t('chat.page.generationFailed')
      if (imageMode) {
        setImageGeneration((current) => current ? {
          ...current,
          assistant: {
            ...current.assistant,
            status: stopped ? 'stopped' : 'error',
            errorCode: stopped ? 'generation_cancelled' : 'image_generation_failed',
            errorMessage: stopped ? t('chat.page.imageGenerationStopped') : message,
          },
        } : current)
      } else {
        setStreamState((current) => current
          ? failChatStream(current, stopped, message)
          : current)
      }
      if (imageMode || !generationStarted) restoreInput()
      if (!stopped) toast.error(message)
    } finally {
      controller.current = null
      setUploading(false)
      if (conversationId) {
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: qk.chatMessages(conversationId) }),
          queryClient.invalidateQueries({ queryKey: qk.chatConversations }),
        ])
      }
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
              else toast.error(t('chat.page.noImageGenModel'))
            }
          }}
          onModelChange={setSelectedModel}
          onImageSizeChange={setImageSize}
          onImageQualityChange={setImageQuality}
        />

        <ChatTranscript
          messages={renderedMessages}
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
