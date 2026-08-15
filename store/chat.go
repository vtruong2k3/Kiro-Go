package store

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrChatNotFound           = errors.New("store: chat conversation not found")
	ErrChatMessageNotFound    = errors.New("store: chat message not found")
	ErrChatAttachmentNotFound = errors.New("store: chat attachment not found")
	ErrChatConflict           = errors.New("store: chat conflict")
	ErrChatInvalidCursor      = errors.New("store: invalid chat cursor")
)

type ChatConversation struct {
	ID, Title, Provider, Model, Mode, Status, ProjectID string
	Pinned                                              bool
	CreatedAt, UpdatedAt                                int64
	ArchivedAt                                          *int64
}

type ChatMessage struct {
	ID, ConversationID, ParentMessageID, ClientRequestID, RequestHash string
	Role, Content, Provider, Model, Status                            string
	ErrorCode, ErrorMessage, ProviderResponseID, RequestID            string
	InputTokens, OutputTokens, CacheReadTokens, CacheCreationTokens   int
	CreatedAt, UpdatedAt                                              int64
}

type ChatAttachment struct {
	ID, ConversationID, MessageID, Kind, Name, MIMEType, StorageKey string
	SizeBytes                                                       int64
	Width, Height                                                   *int
	CreatedAt                                                       int64
}

type ChatConversationPage struct {
	Items      []ChatConversation
	NextCursor string
}
type ChatMessagePage struct {
	Items      []ChatMessage
	NextCursor string
}
type ChatTurn struct {
	User, Assistant ChatMessage
	Created         bool
}

type chatCursor struct {
	Time   int64  `json:"t"`
	ID     string `json:"i"`
	Pinned bool   `json:"p,omitempty"`
}

func encodeChatCursor(t int64, id string) string {
	b, _ := json.Marshal(chatCursor{Time: t, ID: id})
	return base64.RawURLEncoding.EncodeToString(b)
}

func encodeChatConversationCursor(c ChatConversation) string {
	b, _ := json.Marshal(chatCursor{Time: c.UpdatedAt, ID: c.ID, Pinned: c.Pinned})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeChatCursor(value string) (chatCursor, error) {
	if value == "" {
		return chatCursor{}, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return chatCursor{}, ErrChatInvalidCursor
	}
	var c chatCursor
	if json.Unmarshal(b, &c) != nil || c.ID == "" {
		return chatCursor{}, ErrChatInvalidCursor
	}
	return c, nil
}

func (s *Store) chatDBLocked() (*sql.DB, error) {
	if s == nil || s.db == nil {
		return nil, ErrStorageUnavailable
	}
	return s.db, nil
}

func scanChatConversation(row interface{ Scan(...any) error }) (ChatConversation, error) {
	var c ChatConversation
	var pinned int
	err := row.Scan(&c.ID, &c.Title, &c.Provider, &c.Model, &c.Mode, &c.Status, &pinned, &c.ProjectID, &c.CreatedAt, &c.UpdatedAt, &c.ArchivedAt)
	c.Pinned = pinned != 0
	return c, err
}

const chatConversationColumns = `id,title,provider,model,mode,status,pinned,COALESCE(project_id,''),created_at,updated_at,archived_at`

func (s *Store) CreateChatConversation(c ChatConversation) (ChatConversation, error) {
	if s == nil {
		return ChatConversation{}, ErrStorageUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.chatDBLocked()
	if err != nil {
		return ChatConversation{}, err
	}
	now := time.Now().UnixMilli()
	if c.Mode == "" {
		c.Mode = "chat"
	}
	if c.Status == "" {
		c.Status = "active"
	}
	c.CreatedAt, c.UpdatedAt = now, now
	_, err = db.Exec(`INSERT INTO chat_conversations(id,title,provider,model,mode,status,pinned,project_id,created_at,updated_at,archived_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, c.ID, c.Title, c.Provider, c.Model, c.Mode, c.Status, c.Pinned, c.ProjectID, now, now, c.ArchivedAt)
	if err != nil {
		return ChatConversation{}, fmt.Errorf("store: create chat conversation: %w", err)
	}
	return c, nil
}

func (s *Store) GetChatConversation(id string) (ChatConversation, error) {
	if s == nil {
		return ChatConversation{}, ErrStorageUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.chatDBLocked()
	if err != nil {
		return ChatConversation{}, err
	}
	c, err := scanChatConversation(db.QueryRow(`SELECT `+chatConversationColumns+` FROM chat_conversations WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return ChatConversation{}, ErrChatNotFound
	}
	if err != nil {
		return ChatConversation{}, fmt.Errorf("store: get chat conversation: %w", err)
	}
	return c, nil
}

func (s *Store) UpdateChatConversation(c ChatConversation) (ChatConversation, error) {
	if s == nil {
		return ChatConversation{}, ErrStorageUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.chatDBLocked()
	if err != nil {
		return ChatConversation{}, err
	}
	now := time.Now().UnixMilli()
	res, err := db.Exec(`UPDATE chat_conversations SET title=?,provider=?,model=?,mode=?,status=?,pinned=?,project_id=?,archived_at=?,updated_at=? WHERE id=?`, c.Title, c.Provider, c.Model, c.Mode, c.Status, c.Pinned, c.ProjectID, c.ArchivedAt, now, c.ID)
	if err != nil {
		return ChatConversation{}, fmt.Errorf("store: update chat conversation: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ChatConversation{}, ErrChatNotFound
	}
	c.UpdatedAt = now
	return c, nil
}

func (s *Store) DeleteChatConversation(id string) error {
	if s == nil {
		return ErrStorageUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.chatDBLocked()
	if err != nil {
		return err
	}
	res, err := db.Exec(`DELETE FROM chat_conversations WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("store: delete chat conversation: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrChatNotFound
	}
	return nil
}

func escapeChatLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

func (s *Store) ListChatConversations(status, search, cursor string, limit int) (ChatConversationPage, error) {
	if s == nil {
		return ChatConversationPage{}, ErrStorageUnavailable
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	cur, err := decodeChatCursor(cursor)
	if err != nil {
		return ChatConversationPage{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.chatDBLocked()
	if err != nil {
		return ChatConversationPage{}, err
	}
	query := `SELECT ` + chatConversationColumns + ` FROM chat_conversations WHERE (?='' OR status=?) AND (?='' OR title LIKE ? ESCAPE '\' COLLATE NOCASE OR model LIKE ? ESCAPE '\' COLLATE NOCASE)`
	searchPattern := "%" + escapeChatLike(strings.TrimSpace(search)) + "%"
	args := []any{status, status, strings.TrimSpace(search), searchPattern, searchPattern}
	if cursor != "" {
		query += ` AND (pinned < ? OR (pinned = ? AND (updated_at < ? OR (updated_at = ? AND id < ?))))`
		args = append(args, cur.Pinned, cur.Pinned, cur.Time, cur.Time, cur.ID)
	}
	query += ` ORDER BY pinned DESC,updated_at DESC,id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := db.Query(query, args...)
	if err != nil {
		return ChatConversationPage{}, err
	}
	defer rows.Close()
	var out []ChatConversation
	for rows.Next() {
		c, e := scanChatConversation(rows)
		if e != nil {
			return ChatConversationPage{}, e
		}
		out = append(out, c)
	}
	if err = rows.Err(); err != nil {
		return ChatConversationPage{}, err
	}
	page := ChatConversationPage{Items: out}
	if len(out) > limit {
		last := out[limit-1]
		page.Items = out[:limit]
		page.NextCursor = encodeChatConversationCursor(last)
	}
	return page, nil
}

func scanChatMessage(row interface{ Scan(...any) error }) (ChatMessage, error) {
	var m ChatMessage
	err := row.Scan(&m.ID, &m.ConversationID, &m.ParentMessageID, &m.ClientRequestID, &m.RequestHash, &m.Role, &m.Content, &m.Provider, &m.Model, &m.Status, &m.ErrorCode, &m.ErrorMessage, &m.ProviderResponseID, &m.RequestID, &m.InputTokens, &m.OutputTokens, &m.CacheReadTokens, &m.CacheCreationTokens, &m.CreatedAt, &m.UpdatedAt)
	return m, err
}

const chatMessageColumns = `id,conversation_id,COALESCE(parent_message_id,''),client_request_id,request_hash,role,content,provider,model,status,error_code,error_message,provider_response_id,request_id,input_tokens,output_tokens,cache_read_tokens,cache_creation_tokens,created_at,updated_at`

func (s *Store) CreateChatTurn(conversationID, clientRequestID string, user, assistant ChatMessage) (ChatTurn, error) {
	return s.CreateChatTurnWithAttachments(conversationID, clientRequestID, user, assistant, nil)
}

func (s *Store) CreateChatTurnWithAttachments(conversationID, clientRequestID string, user, assistant ChatMessage, attachmentIDs []string) (ChatTurn, error) {
	if s == nil {
		return ChatTurn{}, ErrStorageUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.chatDBLocked()
	if err != nil {
		return ChatTurn{}, err
	}
	tx, err := db.Begin()
	if err != nil {
		return ChatTurn{}, err
	}
	defer tx.Rollback()
	if clientRequestID != "" {
		existing, e := scanChatMessage(tx.QueryRow(`SELECT `+chatMessageColumns+` FROM chat_messages WHERE conversation_id=? AND client_request_id=?`, conversationID, clientRequestID))
		if e == nil {
			if existing.RequestHash != user.RequestHash {
				return ChatTurn{}, ErrChatConflict
			}
			a, e := scanChatMessage(tx.QueryRow(`SELECT `+chatMessageColumns+` FROM chat_messages WHERE parent_message_id=? AND role='assistant' ORDER BY created_at,id LIMIT 1`, existing.ID))
			if e != nil {
				return ChatTurn{}, e
			}
			return ChatTurn{User: existing, Assistant: a, Created: false}, nil
		} else if e != sql.ErrNoRows {
			return ChatTurn{}, e
		}
	}
	var status string
	if err = tx.QueryRow(`SELECT status FROM chat_conversations WHERE id=?`, conversationID).Scan(&status); err == sql.ErrNoRows {
		return ChatTurn{}, ErrChatNotFound
	} else if err != nil {
		return ChatTurn{}, err
	}
	if status != "active" {
		return ChatTurn{}, ErrChatConflict
	}
	now := time.Now().UnixMilli()
	user.ConversationID = conversationID
	user.ClientRequestID = clientRequestID
	user.Role = "user"
	user.Status = "complete"
	user.CreatedAt = now
	user.UpdatedAt = now
	_, err = tx.Exec(`INSERT INTO chat_messages(id,conversation_id,parent_message_id,client_request_id,request_hash,role,content,provider,model,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, user.ID, conversationID, nil, clientRequestID, user.RequestHash, user.Role, user.Content, user.Provider, user.Model, user.Status, now, now)
	if err != nil {
		return ChatTurn{}, fmt.Errorf("store: create chat user message: %w", err)
	}
	for _, attachmentID := range attachmentIDs {
		var attachmentConversation string
		var messageID sql.NullString
		if bindErr := tx.QueryRow(`SELECT conversation_id,message_id FROM chat_attachments WHERE id=?`, attachmentID).Scan(&attachmentConversation, &messageID); bindErr == sql.ErrNoRows || attachmentConversation != conversationID {
			return ChatTurn{}, ErrChatAttachmentNotFound
		} else if bindErr != nil {
			return ChatTurn{}, bindErr
		} else if messageID.Valid {
			return ChatTurn{}, ErrChatConflict
		}
		result, bindErr := tx.Exec(`UPDATE chat_attachments SET message_id=? WHERE id=? AND conversation_id=? AND message_id IS NULL AND kind='image_input'`, user.ID, attachmentID, conversationID)
		if bindErr != nil {
			return ChatTurn{}, bindErr
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return ChatTurn{}, ErrChatConflict
		}
	}
	assistant.ConversationID = conversationID
	assistant.ParentMessageID = user.ID
	assistant.Role = "assistant"
	assistant.Status = "pending"
	assistant.CreatedAt = now
	assistant.UpdatedAt = now
	_, err = tx.Exec(`INSERT INTO chat_messages(id,conversation_id,parent_message_id,role,content,provider,model,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, assistant.ID, conversationID, user.ID, assistant.Role, assistant.Content, assistant.Provider, assistant.Model, assistant.Status, now, now)
	if err != nil {
		return ChatTurn{}, fmt.Errorf("store: create chat assistant message: %w", err)
	}
	if _, err = tx.Exec(`UPDATE chat_conversations SET updated_at=? WHERE id=?`, now, conversationID); err != nil {
		return ChatTurn{}, err
	}
	if err = tx.Commit(); err != nil {
		return ChatTurn{}, err
	}
	return ChatTurn{User: user, Assistant: assistant, Created: true}, nil
}

func (s *Store) AbortChatTurn(userMessageID string) error {
	if s == nil {
		return ErrStorageUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.chatDBLocked()
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE chat_attachments SET message_id=NULL WHERE message_id=?`, userMessageID); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM chat_messages WHERE parent_message_id=? AND role='assistant'`, userMessageID); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM chat_messages WHERE id=? AND role='user'`, userMessageID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) FinalizeChatMessage(m ChatMessage) (ChatMessage, error) {
	if m.Status != "complete" && m.Status != "stopped" && m.Status != "error" {
		return ChatMessage{}, ErrChatConflict
	}
	if s == nil {
		return ChatMessage{}, ErrStorageUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.chatDBLocked()
	if err != nil {
		return ChatMessage{}, err
	}
	tx, err := db.Begin()
	if err != nil {
		return ChatMessage{}, err
	}
	defer tx.Rollback()
	existing, err := scanChatMessage(tx.QueryRow(`SELECT `+chatMessageColumns+` FROM chat_messages WHERE id=?`, m.ID))
	if err == sql.ErrNoRows {
		return ChatMessage{}, ErrChatMessageNotFound
	}
	if err != nil {
		return ChatMessage{}, err
	}
	if existing.Role != "assistant" || (existing.Status != "pending" && existing.Status != "streaming") {
		return ChatMessage{}, ErrChatConflict
	}
	now := time.Now().UnixMilli()
	_, err = tx.Exec(`UPDATE chat_messages SET content=?,provider=?,model=?,status=?,error_code=?,error_message=?,provider_response_id=?,request_id=?,input_tokens=?,output_tokens=?,cache_read_tokens=?,cache_creation_tokens=?,updated_at=? WHERE id=?`, m.Content, m.Provider, m.Model, m.Status, m.ErrorCode, m.ErrorMessage, m.ProviderResponseID, m.RequestID, m.InputTokens, m.OutputTokens, m.CacheReadTokens, m.CacheCreationTokens, now, m.ID)
	if err != nil {
		return ChatMessage{}, err
	}
	if _, err = tx.Exec(`UPDATE chat_conversations SET updated_at=? WHERE id=?`, now, existing.ConversationID); err != nil {
		return ChatMessage{}, err
	}
	updated, err := scanChatMessage(tx.QueryRow(`SELECT `+chatMessageColumns+` FROM chat_messages WHERE id=?`, m.ID))
	if err != nil {
		return ChatMessage{}, err
	}
	if err = tx.Commit(); err != nil {
		return ChatMessage{}, err
	}
	return updated, nil
}

func (s *Store) StopStaleChatMessages(updatedBefore int64) (int64, error) {
	if s == nil {
		return 0, ErrStorageUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.chatDBLocked()
	if err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	result, err := db.Exec(`UPDATE chat_messages SET status='stopped',error_code='generation_stopped',error_message='generation was interrupted by a server restart',updated_at=? WHERE role='assistant' AND status IN ('pending','streaming') AND updated_at<?`, now, updatedBefore)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) ListCompletedChatMessages(conversationID string) ([]ChatMessage, error) {
	if s == nil {
		return nil, ErrStorageUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.chatDBLocked()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT `+chatMessageColumns+` FROM chat_messages WHERE conversation_id=? AND status='complete' ORDER BY created_at,rowid`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ChatMessage, 0)
	for rows.Next() {
		m, scanErr := scanChatMessage(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) ListChatMessages(conversationID, cursor string, limit int) (ChatMessagePage, error) {
	if s == nil {
		return ChatMessagePage{}, ErrStorageUnavailable
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	cur, err := decodeChatCursor(cursor)
	if err != nil {
		return ChatMessagePage{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.chatDBLocked()
	if err != nil {
		return ChatMessagePage{}, err
	}
	query := `SELECT ` + chatMessageColumns + ` FROM chat_messages WHERE conversation_id=?`
	args := []any{conversationID}
	if cursor != "" {
		query += ` AND (created_at > ? OR (created_at=? AND rowid>(SELECT rowid FROM chat_messages WHERE conversation_id=? AND id=?)))`
		args = append(args, cur.Time, cur.Time, conversationID, cur.ID)
	}
	query += ` ORDER BY created_at,rowid LIMIT ?`
	args = append(args, limit+1)
	rows, err := db.Query(query, args...)
	if err != nil {
		return ChatMessagePage{}, err
	}
	defer rows.Close()
	var out []ChatMessage
	for rows.Next() {
		m, e := scanChatMessage(rows)
		if e != nil {
			return ChatMessagePage{}, e
		}
		out = append(out, m)
	}
	if err = rows.Err(); err != nil {
		return ChatMessagePage{}, err
	}
	p := ChatMessagePage{Items: out}
	if len(out) > limit {
		last := out[limit-1]
		p.Items = out[:limit]
		p.NextCursor = encodeChatCursor(last.CreatedAt, last.ID)
	}
	return p, nil
}

func scanChatAttachment(row interface{ Scan(...any) error }) (ChatAttachment, error) {
	var a ChatAttachment
	err := row.Scan(&a.ID, &a.ConversationID, &a.MessageID, &a.Kind, &a.Name, &a.MIMEType, &a.SizeBytes, &a.StorageKey, &a.Width, &a.Height, &a.CreatedAt)
	return a, err
}

const chatAttachmentColumns = `id,conversation_id,COALESCE(message_id,''),kind,name,mime_type,size_bytes,storage_key,width,height,created_at`

func (s *Store) CreateChatAttachment(a ChatAttachment) (ChatAttachment, error) {
	if s == nil {
		return ChatAttachment{}, ErrStorageUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.chatDBLocked()
	if err != nil {
		return ChatAttachment{}, err
	}
	if a.CreatedAt == 0 {
		a.CreatedAt = time.Now().UnixMilli()
	}
	var message any
	if a.MessageID != "" {
		message = a.MessageID
	}
	_, err = db.Exec(`INSERT INTO chat_attachments(id,conversation_id,message_id,kind,name,mime_type,size_bytes,storage_key,width,height,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, a.ID, a.ConversationID, message, a.Kind, a.Name, a.MIMEType, a.SizeBytes, a.StorageKey, a.Width, a.Height, a.CreatedAt)
	if err != nil {
		return ChatAttachment{}, fmt.Errorf("store: create chat attachment: %w", err)
	}
	return a, nil
}
func (s *Store) GetChatAttachment(conversationID, id string) (ChatAttachment, error) {
	if s == nil {
		return ChatAttachment{}, ErrStorageUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.chatDBLocked()
	if err != nil {
		return ChatAttachment{}, err
	}
	a, err := scanChatAttachment(db.QueryRow(`SELECT `+chatAttachmentColumns+` FROM chat_attachments WHERE conversation_id=? AND id=?`, conversationID, id))
	if err == sql.ErrNoRows {
		return ChatAttachment{}, ErrChatAttachmentNotFound
	}
	if err != nil {
		return ChatAttachment{}, fmt.Errorf("store: get chat attachment: %w", err)
	}
	return a, nil
}

func (s *Store) ListAllChatAttachments() ([]ChatAttachment, error) {
	if s == nil {
		return nil, ErrStorageUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.chatDBLocked()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT ` + chatAttachmentColumns + ` FROM chat_attachments ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatAttachment
	for rows.Next() {
		a, scanErr := scanChatAttachment(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) DeleteUnboundChatAttachments(createdBefore int64) ([]ChatAttachment, error) {
	if s == nil {
		return nil, ErrStorageUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.chatDBLocked()
	if err != nil {
		return nil, err
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT `+chatAttachmentColumns+` FROM chat_attachments WHERE message_id IS NULL AND created_at<? ORDER BY created_at,id`, createdBefore)
	if err != nil {
		return nil, err
	}
	var out []ChatAttachment
	for rows.Next() {
		a, scanErr := scanChatAttachment(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		out = append(out, a)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(`DELETE FROM chat_attachments WHERE message_id IS NULL AND created_at<?`, createdBefore); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) ListChatAttachments(conversationID string) ([]ChatAttachment, error) {
	if s == nil {
		return nil, ErrStorageUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.chatDBLocked()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT `+chatAttachmentColumns+` FROM chat_attachments WHERE conversation_id=? ORDER BY created_at,id`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatAttachment
	for rows.Next() {
		a, scanErr := scanChatAttachment(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) BindChatAttachments(conversationID, messageID string, attachmentIDs []string) ([]ChatAttachment, error) {
	if s == nil {
		return nil, ErrStorageUnavailable
	}
	if len(attachmentIDs) == 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.chatDBLocked()
	if err != nil {
		return nil, err
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var messageConversation string
	if err = tx.QueryRow(`SELECT conversation_id FROM chat_messages WHERE id=? AND role='user'`, messageID).Scan(&messageConversation); err == sql.ErrNoRows {
		return nil, ErrChatMessageNotFound
	} else if err != nil {
		return nil, err
	}
	if messageConversation != conversationID {
		return nil, ErrChatConflict
	}
	bound := make([]ChatAttachment, 0, len(attachmentIDs))
	for _, id := range attachmentIDs {
		a, queryErr := scanChatAttachment(tx.QueryRow(`SELECT `+chatAttachmentColumns+` FROM chat_attachments WHERE conversation_id=? AND id=?`, conversationID, id))
		if queryErr == sql.ErrNoRows {
			return nil, ErrChatAttachmentNotFound
		}
		if queryErr != nil {
			return nil, queryErr
		}
		if a.MessageID != "" && a.MessageID != messageID {
			return nil, ErrChatConflict
		}
		if a.MessageID == "" {
			if _, err = tx.Exec(`UPDATE chat_attachments SET message_id=? WHERE id=? AND message_id IS NULL`, messageID, id); err != nil {
				return nil, err
			}
			a.MessageID = messageID
		}
		bound = append(bound, a)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return bound, nil
}

func (s *Store) DeleteChatAttachment(id string) error {
	if s == nil {
		return ErrStorageUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.chatDBLocked()
	if err != nil {
		return err
	}
	res, err := db.Exec(`DELETE FROM chat_attachments WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrChatAttachmentNotFound
	}
	return nil
}
