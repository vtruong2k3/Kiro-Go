import { useState } from 'react'
import { Bot, Check, FileJson, FileText, ImagePlus, Menu, Pencil } from 'lucide-react'
import { useTranslation } from 'react-i18next'
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
  const { t } = useTranslation()
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
          aria-label={t('chat.header.openConversations')}
        >
          <Menu />
        </Button>
        <div className="grid size-8 shrink-0 place-items-center rounded-xl bg-primary/10 text-primary">
          <Bot className="size-4" />
        </div>
        <div className="min-w-0">
          <div className="flex items-center gap-1">
            <span className="truncate text-sm font-semibold">
              {conversation?.title || t('chat.header.title')}
            </span>
            {conversation ? (
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                onClick={onRename}
                aria-label={t('chat.header.rename')}
              >
                <Pencil />
              </Button>
            ) : null}
          </div>
          <span className="hidden truncate text-[11px] text-muted-foreground sm:block">
            {imageMode ? t('chat.header.subtitleImage') : t('chat.header.subtitleChat')}
          </span>
        </div>
      </div>

      <div className="flex min-w-0 flex-1 flex-wrap items-center justify-end gap-1.5">
        {conversation ? (
          <div className="hidden items-center sm:flex">
            <Button type="button" variant="ghost" size="icon-sm" onClick={() => onExport('markdown')} aria-label={t('chat.header.exportMarkdown')}>
              <FileText />
            </Button>
            <Button type="button" variant="ghost" size="icon-sm" onClick={() => onExport('json')} aria-label={t('chat.header.exportJson')}>
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
          <span className="hidden lg:inline">{imageMode ? t('chat.header.createImage') : t('chat.header.chatMode')}</span>
        </Button>
        {imageMode ? (
          <div className="order-last flex basis-full items-center gap-1 sm:order-none sm:basis-auto">
            <Select value={imageSize} onValueChange={onImageSizeChange}>
              <SelectTrigger className="min-w-0 flex-1 sm:w-28 sm:flex-none"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="auto">{t('chat.header.sizeAuto')}</SelectItem>
                <SelectItem value="1024x1024">{t('chat.header.sizeSquare')}</SelectItem>
                <SelectItem value="1536x1024">{t('chat.header.sizeLandscape')}</SelectItem>
                <SelectItem value="1024x1536">{t('chat.header.sizePortrait')}</SelectItem>
              </SelectContent>
            </Select>
            <Select value={imageQuality} onValueChange={onImageQualityChange}>
              <SelectTrigger className="min-w-0 flex-1 sm:w-28 sm:flex-none"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="auto">{t('chat.header.qualityAuto')}</SelectItem>
                <SelectItem value="low">{t('chat.header.qualityLow')}</SelectItem>
                <SelectItem value="medium">{t('chat.header.qualityMedium')}</SelectItem>
                <SelectItem value="high">{t('chat.header.qualityHigh')}</SelectItem>
              </SelectContent>
            </Select>
          </div>
        ) : null}
        <ModelSelector open={modelSelectorOpen} onOpenChange={setModelSelectorOpen}>
          <ModelSelectorTrigger asChild>
            <Button
              variant="outline"
              className="min-w-0 max-w-56 justify-start sm:w-56"
              aria-label={t('chat.header.selectProviderModel')}
            >
              <span className="size-2 shrink-0 rounded-full bg-primary shadow-[0_0_10px_var(--glow)]" />
              <span className="truncate">
                {currentModel
                  ? `${currentModel.provider} · ${currentModel.displayName}`
                  : t('chat.header.selectModel')}
              </span>
            </Button>
          </ModelSelectorTrigger>
          <ModelSelectorContent className="sm:max-w-xl" title={t('chat.header.chooseProviderModel')}>
            <ModelSelectorInput placeholder={t('chat.header.searchProvidersModels')} />
            <ModelSelectorList>
              <ModelSelectorEmpty>{t('chat.header.noMatchingModel')}</ModelSelectorEmpty>
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
