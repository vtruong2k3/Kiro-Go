import { useEffect, useState } from 'react'
import { ImagePlus, Sparkles } from 'lucide-react'
import type { ChatStatus } from 'ai'
import {
  Attachment,
  AttachmentPreview,
  AttachmentRemove,
  Attachments,
  type AttachmentData,
} from '@/components/ai-elements/attachments'
import {
  PromptInput,
  PromptInputBody,
  PromptInputButton,
  PromptInputFooter,
  PromptInputHeader,
  PromptInputSubmit,
  PromptInputTextarea,
  PromptInputTools,
} from '@/components/ai-elements/prompt-input'
import type { ChatModel } from '@/types/chat'

const MAX_IMAGE_BYTES = 10 * 1024 * 1024

interface ChatComposerProps {
  draft: string
  pendingImages: File[]
  imageMode: boolean
  busy: boolean
  uploading: boolean
  selectedModel?: ChatModel
  onDraftChange: (value: string) => void
  onAddImages: (files: File[]) => void
  onRemoveImage: (index: number) => void
  onSubmit: () => void | Promise<void>
  onStop: () => void
}

function PendingAttachment({
  file,
  index,
  onRemove,
}: {
  file: File
  index: number
  onRemove: (index: number) => void
}) {
  const [url, setURL] = useState('')

  useEffect(() => {
    const objectURL = URL.createObjectURL(file)
    setURL(objectURL)
    return () => URL.revokeObjectURL(objectURL)
  }, [file])

  const data: AttachmentData = {
    id: `${file.name}-${file.lastModified}-${index}`,
    type: 'file',
    filename: file.name,
    mediaType: file.type,
    url,
  }

  return (
    <Attachment data={data} onRemove={() => onRemove(index)}>
      <AttachmentPreview />
      <AttachmentRemove label={`Remove ${file.name}`} />
    </Attachment>
  )
}

export function ChatComposer({
  draft,
  pendingImages,
  imageMode,
  busy,
  uploading,
  selectedModel,
  onDraftChange,
  onAddImages,
  onRemoveImage,
  onSubmit,
  onStop,
}: ChatComposerProps) {
  const status: ChatStatus = busy
    ? uploading
      ? 'submitted'
      : 'streaming'
    : 'ready'
  const attachmentsDisabled =
    imageMode || busy || pendingImages.length >= 4 || selectedModel?.capabilities.vision === false

  return (
    <div className="border-t bg-background/95 px-3 py-3 backdrop-blur supports-[backdrop-filter]:bg-background/80 sm:px-6">
      <PromptInput
        data-chat-composer
        className="mx-auto w-full max-w-3xl rounded-2xl border-border/80 bg-card shadow-[0_12px_40px_-24px_var(--glow)]"
        accept="image/png,image/jpeg,image/webp"
        multiple
        maxFiles={4}
        maxFileSize={MAX_IMAGE_BYTES}
        onFilesAdded={(files) => {
          if (imageMode) return false
          onAddImages(files)
          return false
        }}
        onError={() => undefined}
        onSubmit={() => onSubmit()}
      >
        {pendingImages.length ? (
          <PromptInputHeader className="border-b px-3 py-3">
            <Attachments variant="grid" className="mr-auto ml-0">
              {pendingImages.map((file, index) => (
                <PendingAttachment
                  key={`${file.name}-${file.lastModified}-${index}`}
                  file={file}
                  index={index}
                  onRemove={onRemoveImage}
                />
              ))}
            </Attachments>
          </PromptInputHeader>
        ) : null}
        <PromptInputBody>
          <PromptInputTextarea
            className="min-h-20 px-4 pt-4 text-[15px]"
            placeholder={
              imageMode ? 'Describe the image to create…' : 'Message AI…'
            }
            value={draft}
            disabled={busy}
            onChange={(event) => onDraftChange(event.target.value)}
            onPaste={(event) => {
              if (imageMode) return
              const files = Array.from(event.clipboardData.files)
              if (files.length) {
                event.preventDefault()
                onAddImages(files)
              }
            }}
          />
        </PromptInputBody>
        <PromptInputFooter className="px-2.5 pb-2.5">
          <PromptInputTools>
            {!imageMode ? (
              <PromptInputButton
                tooltip="Attach images"
                disabled={attachmentsDisabled}
                onClick={() => {
                  const input = document.querySelector<HTMLInputElement>(
                    'form[data-chat-composer] input[type="file"]',
                  )
                  input?.click()
                }}
              >
                <ImagePlus />
              </PromptInputButton>
            ) : (
              <div className="flex items-center gap-1.5 rounded-lg bg-primary/10 px-2.5 py-1.5 text-xs font-medium text-primary">
                <Sparkles className="size-3.5" /> Image generation
              </div>
            )}
            <span className="hidden truncate text-xs text-muted-foreground sm:inline">
              {selectedModel ? `${selectedModel.provider} · ${selectedModel.displayName}` : 'Select a model'}
            </span>
          </PromptInputTools>
          <PromptInputSubmit
            status={status}
            onStop={onStop}
            disabled={!busy && ((!draft.trim() && !pendingImages.length) || uploading)}
          />
        </PromptInputFooter>
      </PromptInput>
    </div>
  )
}
