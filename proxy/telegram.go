package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"kiro-go/config"
	"kiro-go/logger"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	EventBan          = "ban"
	EventQuota        = "quota"
	EventOverage      = "overage"
	EventTokenRefresh = "token_refresh"
	EventSoft         = "soft"

	telegramDedupTTL    = 15 * time.Minute
	telegramMaxDetail   = 500
	telegramAPIBase     = "https://api.telegram.org"
	telegramHTTPTimeout = 10 * time.Second
)

type telegramNotifier struct {
	mu       sync.Mutex
	lastSent map[string]time.Time
	client   *http.Client
	// apiBase overrides Telegram API root in tests (empty = production).
	apiBase string
}

var telegram = newTelegramNotifier()

func newTelegramNotifier() *telegramNotifier {
	return &telegramNotifier{
		lastSent: make(map[string]time.Time),
		client:   &http.Client{Timeout: telegramHTTPTimeout},
	}
}

// NotifyAccountEvent sends a Telegram alert for a notable account failure.
// Fire-and-forget: never blocks the request path. Dedupes by account+event for 15m.
func NotifyAccountEvent(account *config.Account, eventType, detail string) {
	if account == nil || eventType == "" {
		return
	}
	cfg := config.GetTelegramConfig()
	if !cfg.Enabled || cfg.BotToken == "" || cfg.ChatID == "" {
		return
	}
	key := account.ID + "|" + eventType
	if !telegram.shouldSend(key) {
		return
	}
	msg := formatAccountEventMessage(account, eventType, detail)
	token, chatID := cfg.BotToken, cfg.ChatID
	go func() {
		if err := telegram.sendMessage(token, chatID, msg); err != nil {
			logger.Warnf("[Telegram] send failed (%s/%s): %v", account.ID, eventType, err)
		}
	}()
}

// SendTelegramTest sends a synchronous test message using the saved config.
// Enabled is not required so operators can verify credentials before enabling.
func SendTelegramTest() error {
	cfg := config.GetTelegramConfig()
	if cfg.BotToken == "" || cfg.ChatID == "" {
		return fmt.Errorf("telegram bot token and chat id must be configured first")
	}
	msg := fmt.Sprintf("✅ Kiro-Go Telegram test\n\nYour bot token and chat ID are working.\nTime: %s",
		time.Now().UTC().Format("2006-01-02 15:04:05 UTC"))
	return telegram.sendMessage(cfg.BotToken, cfg.ChatID, msg)
}

// MaskBotToken returns a masked preview and whether a token is set.
func MaskBotToken(token string) (masked string, set bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}
	if len(token) <= 4 {
		return "••••", true
	}
	return "••••" + token[len(token)-4:], true
}

func (n *telegramNotifier) shouldSend(key string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := time.Now()
	if t, ok := n.lastSent[key]; ok && now.Sub(t) < telegramDedupTTL {
		return false
	}
	n.lastSent[key] = now
	if len(n.lastSent) > 256 {
		n.pruneLocked(now)
	}
	return true
}

func (n *telegramNotifier) pruneLocked(now time.Time) {
	for k, t := range n.lastSent {
		if now.Sub(t) >= telegramDedupTTL {
			delete(n.lastSent, k)
		}
	}
}

func (n *telegramNotifier) sendMessage(token, chatID, text string) error {
	base := n.apiBase
	if base == "" {
		base = telegramAPIBase
	}
	url := fmt.Sprintf("%s/bot%s/sendMessage", strings.TrimRight(base, "/"), token)
	body, err := json.Marshal(map[string]interface{}{
		"chat_id":                  chatID,
		"text":                     text,
		"disable_web_page_preview": true,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func formatAccountEventMessage(account *config.Account, eventType, detail string) string {
	title := eventTitle(eventType)
	label := accountLabel(account)
	detail = truncateRunes(strings.TrimSpace(detail), telegramMaxDetail)
	var b strings.Builder
	b.WriteString("🚨 Kiro-Go · ")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString("Account: ")
	b.WriteString(label)
	b.WriteString("\n")
	b.WriteString("ID: ")
	b.WriteString(account.ID)
	b.WriteString("\n")
	b.WriteString("Event: ")
	b.WriteString(eventType)
	b.WriteString("\n")
	if detail != "" {
		b.WriteString("Detail: ")
		b.WriteString(detail)
		b.WriteString("\n")
	}
	b.WriteString("Time: ")
	b.WriteString(time.Now().UTC().Format("2006-01-02 15:04:05 UTC"))
	return b.String()
}

func eventTitle(eventType string) string {
	switch eventType {
	case EventBan:
		return "Account banned / disabled"
	case EventQuota:
		return "Quota exceeded (429)"
	case EventOverage:
		return "Overage limit (402)"
	case EventTokenRefresh:
		return "Token refresh failed"
	case EventSoft:
		return "Soft error / cooldown"
	default:
		return "Account event"
	}
}

func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}
