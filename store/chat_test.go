package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func openChatTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestChatMigrationSchemaAndRecovery(t *testing.T) {
	s := openChatTestStore(t)
	var version int
	if err := s.db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("version=%d err=%v", version, err)
	}
	for _, table := range []string{"chat_conversations", "chat_messages", "chat_attachments"} {
		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("table %s count=%d err=%v", table, count, err)
		}
	}

	path := filepath.Join(t.TempDir(), "recover.db")
	db, err := sql.Open(driverName, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE schema_version(version INTEGER NOT NULL); INSERT INTO schema_version VALUES(8)`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	recovered, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if _, err = recovered.CreateChatConversation(ChatConversation{ID: "recovered"}); err != nil {
		t.Fatalf("reconciled create: %v", err)
	}
}

func TestChatConversationCRUDCursorAndCascade(t *testing.T) {
	s := openChatTestStore(t)
	for _, id := range []string{"a", "b", "c"} {
		if _, err := s.CreateChatConversation(ChatConversation{ID: id, Title: id}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`UPDATE chat_conversations SET updated_at=42`); err != nil {
		t.Fatal(err)
	}
	first, err := s.ListChatConversations("active", "", "", 2)
	if err != nil || len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("first page=%+v err=%v", first, err)
	}
	second, err := s.ListChatConversations("active", "", first.NextCursor, 2)
	if err != nil || len(second.Items) != 1 {
		t.Fatalf("second page=%+v err=%v", second, err)
	}
	seen := map[string]bool{}
	for _, c := range append(first.Items, second.Items...) {
		if seen[c.ID] {
			t.Fatalf("duplicate %s", c.ID)
		}
		seen[c.ID] = true
	}
	if _, err = s.ListChatConversations("", "", "invalid", 2); !errors.Is(err, ErrChatInvalidCursor) {
		t.Fatalf("invalid cursor: %v", err)
	}
	c, err := s.GetChatConversation("a")
	if err != nil {
		t.Fatal(err)
	}
	c.Title, c.Pinned = "renamed", true
	if c, err = s.UpdateChatConversation(c); err != nil || c.Title != "renamed" || !c.Pinned {
		t.Fatalf("update=%+v err=%v", c, err)
	}
	turn, err := s.CreateChatTurn("a", "req-a", ChatMessage{ID: "u-a", Content: "hello"}, ChatMessage{ID: "as-a"})
	if err != nil || turn.Assistant.Status != "pending" {
		t.Fatalf("turn=%+v err=%v", turn, err)
	}
	if _, err = s.CreateChatAttachment(ChatAttachment{ID: "att", ConversationID: "a", MessageID: "u-a", Kind: "image_input", MIMEType: "image/png", StorageKey: "a/att"}); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteChatConversation("a"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"chat_messages", "chat_attachments"} {
		var n int
		if err = s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("cascade %s=%d err=%v", table, n, err)
		}
	}
}

func TestChatConversationSearchAndPinnedCursor(t *testing.T) {
	s := openChatTestStore(t)
	for _, conversation := range []ChatConversation{
		{ID: "plain", Title: "Alpha chat", Model: "model-a"},
		{ID: "percent", Title: "100% literal", Model: "model-b"},
		{ID: "pinned", Title: "Pinned Alpha", Model: "model-c", Pinned: true},
	} {
		if _, err := s.CreateChatConversation(conversation); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`UPDATE chat_conversations SET updated_at=42`); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListChatConversations("active", "Alpha", "", 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "pinned" || page.NextCursor == "" {
		t.Fatalf("first=%+v err=%v", page, err)
	}
	next, err := s.ListChatConversations("active", "Alpha", page.NextCursor, 1)
	if err != nil || len(next.Items) != 1 || next.Items[0].ID != "plain" {
		t.Fatalf("next=%+v err=%v", next, err)
	}
	literal, err := s.ListChatConversations("active", "%", "", 10)
	if err != nil || len(literal.Items) != 1 || literal.Items[0].ID != "percent" {
		t.Fatalf("literal=%+v err=%v", literal, err)
	}
}

func TestChatTurnIdempotencyFinalizeAndMessageCursor(t *testing.T) {
	s := openChatTestStore(t)
	if _, err := s.CreateChatConversation(ChatConversation{ID: "chat"}); err != nil {
		t.Fatal(err)
	}
	create := func() (ChatTurn, error) {
		return s.CreateChatTurn("chat", "request-1", ChatMessage{ID: "user", Content: "prompt"}, ChatMessage{ID: "assistant"})
	}
	first, err := create()
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created {
		t.Fatal("first turn should own generation")
	}
	second, err := create()
	if err != nil || second.Created || second.User.ID != first.User.ID || second.Assistant.ID != first.Assistant.ID {
		t.Fatalf("idempotent turn=%+v err=%v", second, err)
	}
	final := first.Assistant
	final.Status = "complete"
	final.Content = "answer"
	final.Provider, final.Model = "kiro", "claude"
	final.InputTokens, final.OutputTokens = 10, 2
	final.CacheReadTokens, final.CacheCreationTokens = 7, 1
	if final, err = s.FinalizeChatMessage(final); err != nil || final.Content != "answer" {
		t.Fatalf("final=%+v err=%v", final, err)
	}
	if _, err = s.FinalizeChatMessage(final); !errors.Is(err, ErrChatConflict) {
		t.Fatalf("second finalize: %v", err)
	}
	if _, err = s.db.Exec(`UPDATE chat_messages SET created_at=7`); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListChatMessages("chat", "", 1)
	if err != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("message page=%+v err=%v", page, err)
	}
	if page.Items[0].ID != "user" {
		t.Fatalf("first message=%q, want user", page.Items[0].ID)
	}
	next, err := s.ListChatMessages("chat", page.NextCursor, 1)
	if err != nil || len(next.Items) != 1 || next.Items[0].ID != "assistant" {
		t.Fatalf("next=%+v err=%v", next, err)
	}
	attachments, err := s.ListChatAttachments("chat")
	if err != nil || len(attachments) != 0 {
		t.Fatalf("attachments=%+v err=%v", attachments, err)
	}
}

func TestChatTurnConflictArchiveAndCompletedHistory(t *testing.T) {
	s := openChatTestStore(t)
	conversation, err := s.CreateChatConversation(ChatConversation{ID: "chat", Title: "history"})
	if err != nil {
		t.Fatal(err)
	}

	first, err := s.CreateChatTurn("chat", "request-1",
		ChatMessage{ID: "user-1", Content: "first", RequestHash: "hash-1"},
		ChatMessage{ID: "assistant-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateChatTurn("chat", "request-1",
		ChatMessage{ID: "other-user", Content: "changed", RequestHash: "hash-2"},
		ChatMessage{ID: "other-assistant"}); !errors.Is(err, ErrChatConflict) {
		t.Fatalf("mismatched replay: %v", err)
	}

	completed := first.Assistant
	completed.Content = "answer"
	completed.Status = "complete"
	if _, err = s.FinalizeChatMessage(completed); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateChatTurn("chat", "request-2",
		ChatMessage{ID: "user-2", Content: "second", RequestHash: "hash-2"},
		ChatMessage{ID: "assistant-2"}); err != nil {
		t.Fatal(err)
	}

	history, err := s.ListCompletedChatMessages("chat")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 || history[0].ID != "user-1" || history[1].ID != "assistant-1" || history[2].ID != "user-2" {
		t.Fatalf("completed history=%+v", history)
	}

	conversation.Status = "archived"
	if _, err = s.UpdateChatConversation(conversation); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateChatTurn("chat", "request-3",
		ChatMessage{ID: "user-3", Content: "third", RequestHash: "hash-3"},
		ChatMessage{ID: "assistant-3"}); !errors.Is(err, ErrChatConflict) {
		t.Fatalf("archived turn: %v", err)
	}
}

func TestChatConcurrentIdempotencyAndUnavailable(t *testing.T) {
	var nilStore *Store
	if _, err := nilStore.GetChatConversation("x"); !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("nil store: %v", err)
	}
	s := openChatTestStore(t)
	if _, err := s.CreateChatConversation(ChatConversation{ID: "chat"}); err != nil {
		t.Fatal(err)
	}
	const workers = 12
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CreateChatTurn("chat", "same", ChatMessage{ID: "user", Content: "p"}, ChatMessage{ID: "assistant"})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM chat_messages`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("messages=%d err=%v", count, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListChatMessages("chat", "", 1); !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("closed store: %v", err)
	}
}
