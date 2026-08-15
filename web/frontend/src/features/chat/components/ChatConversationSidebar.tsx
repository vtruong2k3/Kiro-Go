import { Archive, MessageSquarePlus, Pin, PinOff, RotateCcw, Search, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { ChatConversation } from '@/types/chat'

interface ChatConversationSidebarProps {
  conversations: ChatConversation[]
  activeId: string
  status: 'active' | 'archived'
  search: string
  loading: boolean
  className?: string
  onCreate: () => void
  onSelect: (id: string) => void
  onStatusChange: (status: 'active' | 'archived') => void
  onSearchChange: (value: string) => void
  onTogglePin: (conversation: ChatConversation) => void
  onToggleArchive: (conversation: ChatConversation) => void
  onDelete: (id: string) => void
}

export function ChatConversationSidebar({
  conversations,
  activeId,
  status,
  search,
  loading,
  className,
  onCreate,
  onSelect,
  onStatusChange,
  onSearchChange,
  onTogglePin,
  onToggleArchive,
  onDelete,
}: ChatConversationSidebarProps) {
  return (
    <aside className={`flex min-h-0 flex-col bg-sidebar text-sidebar-foreground ${className ?? ''}`}>
      <div className="space-y-3 border-b border-sidebar-border p-3">
        <Button className="h-9 w-full justify-start shadow-sm" onClick={onCreate}>
          <MessageSquarePlus /> New chat
        </Button>
        <div className="grid grid-cols-2 rounded-lg bg-muted/60 p-1">
          <Button
            size="sm"
            variant={status === 'active' ? 'secondary' : 'ghost'}
            onClick={() => onStatusChange('active')}
          >
            Active
          </Button>
          <Button
            size="sm"
            variant={status === 'archived' ? 'secondary' : 'ghost'}
            onClick={() => onStatusChange('archived')}
          >
            Archived
          </Button>
        </div>
        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="h-9 bg-background pl-8"
            placeholder="Search chats…"
            value={search}
            onChange={(event) => onSearchChange(event.target.value)}
          />
        </div>
      </div>
      <div className="min-h-0 flex-1 space-y-1 overflow-y-auto p-2">
        {loading ? (
          <p className="px-3 py-6 text-center text-xs text-muted-foreground">Loading conversations…</p>
        ) : conversations.length === 0 ? (
          <p className="px-3 py-6 text-center text-xs text-muted-foreground">
            {search ? 'No matching conversations.' : `No ${status} conversations.`}
          </p>
        ) : conversations.map((conversation) => (
          <div
            key={conversation.id}
            className={`group flex items-center rounded-xl transition-colors ${
              activeId === conversation.id
                ? 'bg-sidebar-accent text-sidebar-accent-foreground'
                : 'hover:bg-sidebar-accent/60'
            }`}
          >
            <button
              className="min-w-0 flex-1 px-3 py-2.5 text-left"
              onClick={() => onSelect(conversation.id)}
            >
              <span className="flex items-center gap-1.5">
                {conversation.pinned ? <Pin className="size-3 shrink-0 fill-current" /> : null}
                <span className="truncate text-sm font-medium">
                  {conversation.title || conversation.model}
                </span>
              </span>
              <span className="mt-0.5 block truncate text-[11px] text-muted-foreground">
                {conversation.provider} · {conversation.model}
              </span>
            </button>
            <div className="flex items-center pr-1 opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100">
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label={conversation.pinned ? 'Unpin conversation' : 'Pin conversation'}
                onClick={() => onTogglePin(conversation)}
              >
                {conversation.pinned ? <PinOff /> : <Pin />}
              </Button>
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label={conversation.status === 'active' ? 'Archive conversation' : 'Restore conversation'}
                onClick={() => onToggleArchive(conversation)}
              >
                {conversation.status === 'active' ? <Archive /> : <RotateCcw />}
              </Button>
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label="Delete conversation"
                onClick={() => onDelete(conversation.id)}
              >
                <Trash2 />
              </Button>
            </div>
          </div>
        ))}
      </div>
    </aside>
  )
}
