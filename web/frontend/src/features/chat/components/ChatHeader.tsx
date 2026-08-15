import { useState } from 'react'
import { Bot, Check, FileJson, FileText, ImagePlus, Menu, Pencil } from 'lucide-react'
import {
  ModelSelector,
  ModelSelectorContent,
  ModelSelectorEmpty,
  ModelSelectorGroup,
  ModelSelectorInput,
  ModelSelectorItem,
  ModelSelectorList,
  ModelSelectorName,
  ModelSelectorTrigger,
} from '@/components/ai-elements/model-selector'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { ChatConversation, ChatModel } from '@/types/chat'

interface ChatHeaderProps {
  conversation?: ChatConversation
  models: ChatModel[]
  selectedModel: string
  imageMode: boolean
  imageSize: string
  imageQuality: string
  onOpenSidebar: () => void
  onRename: () => void
  onExport: (format: 'markdown' | 'json') => void
  onToggleImageMode: () => void
  onModelChange: (id: string) => void
  onImageSizeChange: (value: string) => void
  onImageQualityChange: (value: string) => void
}

export function ChatHeader({
  conversation,
  models,
  selectedModel,
  imageMode,
  imageSize,
  imageQuality,
  onOpenSidebar,
  onRename,
  onExport,
  onToggleImageMode,
  onModelChange,
  onImageSizeChange,
  onImageQualityChange,
}: ChatHeaderProps) {
  const [modelSelectorOpen, setModelSelectorOpen] = useState(false)
  const availableModels = models.filter(
    (model) => !imageMode || model.capabilities.imageGeneration,
  )
  const currentModel = models.find((model) => model.id === selectedModel)
  const providers = [...new Set(availableModels.map((model) => model.provider))]

  return (
    <header className="flex min-h-14 flex-wrap items-center justify-between gap-2 border-b bg-background/95 px-3 py-2 backdrop-blur supports-[backdrop-filter]:bg-background/80 sm:flex-nowrap sm:px-4">
      <div className="flex min-w-0 items-center gap-1.5">
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          className="md:hidden"
          onClick={onOpenSidebar}
          aria-label="Open conversations"
        >
          <Menu />
        </Button>
        <div className="grid size-8 shrink-0 place-items-center rounded-xl bg-primary/10 text-primary">
          <Bot className="size-4" />
        </div>
        <div className="min-w-0">
          <div className="flex items-center gap-1">
            <span className="truncate text-sm font-semibold">
              {conversation?.title || 'AI Chat'}
            </span>
            {conversation ? (
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                onClick={onRename}
                aria-label="Rename conversation"
              >
                <Pencil />
              </Button>
            ) : null}
          </div>
          <span className="hidden truncate text-[11px] text-muted-foreground sm:block">
            {imageMode ? 'Image studio' : 'Streaming through the internal provider pipeline'}
          </span>
        </div>
      </div>

      <div className="flex min-w-0 flex-1 flex-wrap items-center justify-end gap-1.5">
        {conversation ? (
          <div className="hidden items-center sm:flex">
            <Button type="button" variant="ghost" size="icon-sm" onClick={() => onExport('markdown')} aria-label="Export Markdown">
              <FileText />
            </Button>
            <Button type="button" variant="ghost" size="icon-sm" onClick={() => onExport('json')} aria-label="Export JSON">
              <FileJson />
            </Button>
          </div>
        ) : null}
        <Button
          type="button"
          variant={imageMode ? 'default' : 'outline'}
          size="sm"
          onClick={onToggleImageMode}
        >
          <ImagePlus />
          <span className="hidden lg:inline">{imageMode ? 'Create image' : 'Chat'}</span>
        </Button>
        {imageMode ? (
          <div className="order-last flex basis-full items-center gap-1 sm:order-none sm:basis-auto">
            <Select value={imageSize} onValueChange={onImageSizeChange}>
              <SelectTrigger className="min-w-0 flex-1 sm:w-28 sm:flex-none"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="auto">Auto size</SelectItem>
                <SelectItem value="1024x1024">Square</SelectItem>
                <SelectItem value="1536x1024">Landscape</SelectItem>
                <SelectItem value="1024x1536">Portrait</SelectItem>
              </SelectContent>
            </Select>
            <Select value={imageQuality} onValueChange={onImageQualityChange}>
              <SelectTrigger className="min-w-0 flex-1 sm:w-28 sm:flex-none"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="auto">Auto quality</SelectItem>
                <SelectItem value="low">Low</SelectItem>
                <SelectItem value="medium">Medium</SelectItem>
                <SelectItem value="high">High</SelectItem>
              </SelectContent>
            </Select>
          </div>
        ) : null}
        <ModelSelector open={modelSelectorOpen} onOpenChange={setModelSelectorOpen}>
          <ModelSelectorTrigger asChild>
            <Button
              variant="outline"
              className="min-w-0 max-w-56 justify-start sm:w-56"
              aria-label="Select provider and model"
            >
              <span className="size-2 shrink-0 rounded-full bg-primary shadow-[0_0_10px_var(--glow)]" />
              <span className="truncate">
                {currentModel
                  ? `${currentModel.provider} · ${currentModel.displayName}`
                  : 'Select model'}
              </span>
            </Button>
          </ModelSelectorTrigger>
          <ModelSelectorContent className="sm:max-w-xl" title="Choose provider and model">
            <ModelSelectorInput placeholder="Search providers and models…" />
            <ModelSelectorList>
              <ModelSelectorEmpty>No matching model.</ModelSelectorEmpty>
              {providers.map((provider) => (
                <ModelSelectorGroup key={provider} heading={provider}>
                  {availableModels
                    .filter((model) => model.provider === provider)
                    .map((model) => (
                      <ModelSelectorItem
                        key={model.id}
                        value={`${model.provider} ${model.displayName} ${model.model}`}
                        data-checked={model.id === selectedModel}
                        onSelect={() => {
                          onModelChange(model.id)
                          setModelSelectorOpen(false)
                        }}
                      >
                        <span className="grid size-6 shrink-0 place-items-center rounded-lg bg-muted font-mono text-[10px] uppercase text-muted-foreground">
                          {provider.slice(0, 2)}
                        </span>
                        <ModelSelectorName>
                          <span className="block truncate">{model.displayName}</span>
                          <span className="block truncate font-mono text-[10px] text-muted-foreground">{model.model}</span>
                        </ModelSelectorName>
                        {model.id === selectedModel ? <Check className="size-4 text-primary" /> : null}
                      </ModelSelectorItem>
                    ))}
                </ModelSelectorGroup>
              ))}
            </ModelSelectorList>
          </ModelSelectorContent>
        </ModelSelector>
      </div>
    </header>
  )
}
