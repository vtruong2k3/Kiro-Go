import { useState } from 'react'
import { Check, ChevronsUpDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { ModelInfo } from '@/types/account'
import { Button } from '@/components/ui/button'
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
import { cn } from '@/lib/utils'

type Props = {
  models: ModelInfo[]
  value: string
  onChange: (model: string) => void
  isLoading?: boolean
  disabled?: boolean
}

export function AccountModelPicker({
  models,
  value,
  onChange,
  isLoading = false,
  disabled = false,
}: Props) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const selected = models.find((model) => model.modelId === value)
  const selectedLabel = selected?.modelName || selected?.modelId || t('accounts.selectModel')

  return (
    <ModelSelector open={open} onOpenChange={setOpen}>
      <ModelSelectorTrigger asChild>
        <Button
          type="button"
          variant="outline"
          disabled={disabled || isLoading}
          aria-label={
            selected
              ? `${t('accounts.modelPicker.open')}: ${selectedLabel}`
              : t('accounts.modelPicker.open')
          }
          aria-haspopup="dialog"
          className="w-full justify-between"
        >
          <span className={cn('truncate', !selected && 'text-muted-foreground')}>
            {isLoading ? t('accounts.testModelsLoading') : selectedLabel}
          </span>
          <ChevronsUpDown className="size-4 shrink-0 text-muted-foreground" />
        </Button>
      </ModelSelectorTrigger>
      <ModelSelectorContent
        className="w-[min(560px,calc(100vw-2rem))] max-w-none"
        title={t('accounts.modelPicker.title')}
      >
        <ModelSelectorInput placeholder={t('accounts.modelPicker.search')} />
        <ModelSelectorList>
          <ModelSelectorEmpty>{t('accounts.modelPicker.empty')}</ModelSelectorEmpty>
          <ModelSelectorGroup heading={t('accounts.modelPicker.group')}>
            {models.map((model) => (
              <ModelSelectorItem
                key={model.modelId}
                value={`${model.modelName} ${model.modelId} ${model.description}`}
                data-checked={model.modelId === value}
                onSelect={() => {
                  onChange(model.modelId)
                  setOpen(false)
                }}
                title={model.description || model.modelId}
              >
                <ModelSelectorName>
                  <span className="block truncate">{model.modelName || model.modelId}</span>
                  {model.modelName && model.modelName !== model.modelId ? (
                    <span className="block truncate font-mono text-[10px] text-muted-foreground">
                      {model.modelId}
                    </span>
                  ) : null}
                </ModelSelectorName>
                {model.modelId === value ? <Check className="size-4 text-primary" /> : null}
              </ModelSelectorItem>
            ))}
          </ModelSelectorGroup>
        </ModelSelectorList>
      </ModelSelectorContent>
    </ModelSelector>
  )
}
