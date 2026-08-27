// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ModelInfo } from '@/types/account'
import { AccountModelPicker } from './AccountModelPicker'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) =>
      ({
        'accounts.modelPicker.open': 'Select test model',
        'accounts.modelPicker.title': 'Choose a test model',
        'accounts.modelPicker.search': 'Search models...',
        'accounts.modelPicker.empty': 'No matching models',
        'accounts.modelPicker.group': 'Available models',
        'accounts.selectModel': 'Select Test Model',
        'accounts.testModelsLoading': 'Loading models...',
      })[key] ?? key,
  }),
}))

class ResizeObserverMock {
  observe() {}
  unobserve() {}
  disconnect() {}
}

vi.stubGlobal('ResizeObserver', ResizeObserverMock)
window.HTMLElement.prototype.scrollIntoView = () => {}

const models: ModelInfo[] = [
  { modelId: 'claude-sonnet', modelName: 'Claude Sonnet', description: 'Fast model' },
  { modelId: 'claude-haiku', modelName: 'Claude Haiku', description: 'Small model' },
]

afterEach(cleanup)

describe('AccountModelPicker', () => {
  it('searches account models and reports the selected model', () => {
    const onChange = vi.fn()
    render(<AccountModelPicker models={models} value="" onChange={onChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Select test model' }))
    expect(screen.getByText('Claude Sonnet')).toBeInTheDocument()
    expect(screen.getByText('Claude Haiku')).toBeInTheDocument()

    fireEvent.change(screen.getByPlaceholderText('Search models...'), {
      target: { value: 'haiku' },
    })
    expect(screen.queryByText('Claude Sonnet')).not.toBeInTheDocument()
    fireEvent.click(screen.getByText('Claude Haiku'))

    expect(onChange).toHaveBeenCalledWith('claude-haiku')
  })
})
