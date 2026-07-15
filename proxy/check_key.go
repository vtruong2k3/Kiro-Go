package proxy

import (
	"encoding/json"
	"io"
	"kiro-go/config"
	"kiro-go/store"
	"net/http"
	"strings"
	"time"
)

// checkKeyMaxLogs caps how many log rows the public check page returns per key.
const checkKeyMaxLogs = 200

// checkKeyLogView is one usage log row shown on the public check page. It omits
// server-internal fields (account id, raw client ip) that a key holder has no
// need to see.
type checkKeyLogView struct {
	Time      int64   `json:"time"`
	Endpoint  string  `json:"endpoint"`
	Model     string  `json:"model"`
	Status    string  `json:"status"`
	ErrorType string  `json:"errorType,omitempty"`
	Tokens    int     `json:"tokens"`
	Credits   float64 `json:"credits"`
	Duration  int64   `json:"duration"`
}

// checkKeyResponse is the public payload returned when a key holder looks up
// their own key. It never contains the raw key value or any other key's data.
type checkKeyResponse struct {
	KeyMasked string `json:"keyMasked"`
	Name      string `json:"name,omitempty"`
	Enabled   bool   `json:"enabled"`

	// Credit quota. CreditLimit == 0 means unlimited (CreditUnlimited=true).
	CreditLimit      float64 `json:"creditLimit"`
	CreditsUsed      float64 `json:"creditsUsed"`
	CreditsRemaining float64 `json:"creditsRemaining"`
	CreditUnlimited  bool    `json:"creditUnlimited"`

	// Token quota. TokenLimit == 0 means unlimited (TokenUnlimited=true).
	TokenLimit      int64 `json:"tokenLimit"`
	TokensUsed      int64 `json:"tokensUsed"`
	TokensRemaining int64 `json:"tokensRemaining"`
	TokenUnlimited  bool  `json:"tokenUnlimited"`

	// Lifetime.
	ExpiresAt     int64 `json:"expiresAt"`     // 0 = never expires
	NeverExpires  bool  `json:"neverExpires"`  // true when ExpiresAt == 0
	Expired       bool  `json:"expired"`       // past ExpiresAt
	DaysRemaining int64 `json:"daysRemaining"` // whole days left (0 when expired/never)

	CreatedAt     int64 `json:"createdAt"`
	LastUsedAt    int64 `json:"lastUsedAt,omitempty"`
	RequestsCount int64 `json:"requestsCount"`

	Logs []checkKeyLogView `json:"logs"`
}

type checkKeyRequest struct {
	Key string `json:"key"`
}

// handleCheckKeyLookup is the public endpoint behind the /check/key page. A key
// holder submits their own key and gets back that key's quota, lifetime, and
// usage log. It self-authenticates on the submitted key value — no admin
// password — and returns a generic error for any key it cannot resolve so the
// endpoint can't be used to probe which keys exist.
func (h *Handler) handleCheckKeyLookup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	// Accept the key from JSON body, form, or header — whichever the client sends.
	provided := extractProvidedKey(r)
	if provided == "" {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		if len(body) > 0 {
			var req checkKeyRequest
			if err := json.Unmarshal(body, &req); err == nil {
				provided = req.Key
			}
		}
	}
	provided = strings.TrimSpace(provided)

	if provided == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Vui lòng nhập API key"})
		return
	}

	entry := config.FindApiKeyByValue(provided)
	if entry == nil {
		// Generic 404: do not distinguish "not found" from "malformed" so the
		// endpoint cannot be used to enumerate valid keys.
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "API key không tồn tại"})
		return
	}

	resp := buildCheckKeyResponse(*entry)
	resp.Logs = h.logsForApiKey(entry.ID, checkKeyMaxLogs)
	json.NewEncoder(w).Encode(resp)
}

// buildCheckKeyResponse derives the public view (quota/lifetime) from an entry.
func buildCheckKeyResponse(e config.ApiKeyEntry) checkKeyResponse {
	resp := checkKeyResponse{
		KeyMasked:     config.MaskApiKey(e.Key),
		Name:          e.Name,
		Enabled:       e.Enabled,
		CreditLimit:   e.CreditLimit,
		CreditsUsed:   e.CreditsUsed,
		TokenLimit:    e.TokenLimit,
		TokensUsed:    e.TokensUsed,
		ExpiresAt:     e.ExpiresAt,
		CreatedAt:     e.CreatedAt,
		LastUsedAt:    e.LastUsedAt,
		RequestsCount: e.RequestsCount,
	}

	if e.CreditLimit > 0 {
		resp.CreditsRemaining = e.CreditLimit - e.CreditsUsed
		if resp.CreditsRemaining < 0 {
			resp.CreditsRemaining = 0
		}
	} else {
		resp.CreditUnlimited = true
	}

	if e.TokenLimit > 0 {
		resp.TokensRemaining = e.TokenLimit - e.TokensUsed
		if resp.TokensRemaining < 0 {
			resp.TokensRemaining = 0
		}
	} else {
		resp.TokenUnlimited = true
	}

	if e.ExpiresAt <= 0 {
		resp.NeverExpires = true
	} else {
		resp.Expired = config.ApiKeyExpired(e)
		if !resp.Expired {
			secondsLeft := e.ExpiresAt - time.Now().Unix()
			if secondsLeft > 0 {
				resp.DaysRemaining = secondsLeft / 86400
			}
		}
	}

	return resp
}

// logsForApiKey returns this key's usage log, newest first: not-yet-flushed RAM
// entries merged ahead of the persisted SQLite history, capped at limit.
func (h *Handler) logsForApiKey(apiKeyID string, limit int) []checkKeyLogView {
	if apiKeyID == "" || limit <= 0 {
		return []checkKeyLogView{}
	}

	out := make([]checkKeyLogView, 0, limit)

	// Pending logs (in RAM, not yet flushed to SQLite) are the newest. Walk them
	// newest-first so they sit at the top of the merged result.
	h.requestLogsMu.RLock()
	for i := len(h.logPending) - 1; i >= 0 && len(out) < limit; i-- {
		e := h.logPending[i]
		if e.ApiKeyID == apiKeyID {
			out = append(out, checkKeyLogViewFromLog(e))
		}
	}
	h.requestLogsMu.RUnlock()

	// Backfill from persisted history (already newest-first from the query).
	if len(out) < limit && h.runtimeStore != nil {
		rows, err := h.runtimeStore.LoadRequestLogsByApiKeyID(apiKeyID, limit)
		if err == nil {
			for _, row := range rows {
				if len(out) >= limit {
					break
				}
				out = append(out, checkKeyLogViewFromRow(row))
			}
		}
	}

	return out
}

func checkKeyLogViewFromLog(e RequestLog) checkKeyLogView {
	return checkKeyLogView{
		Time:      e.Time,
		Endpoint:  e.Endpoint,
		Model:     e.Model,
		Status:    e.Status,
		ErrorType: e.ErrorType,
		Tokens:    e.Tokens,
		Credits:   e.Credits,
		Duration:  e.Duration,
	}
}

func checkKeyLogViewFromRow(r store.RequestLogRow) checkKeyLogView {
	return checkKeyLogView{
		Time:      r.Time,
		Endpoint:  r.Endpoint,
		Model:     r.Model,
		Status:    r.Status,
		ErrorType: r.ErrorType,
		Tokens:    r.Tokens,
		Credits:   r.Credits,
		Duration:  r.Duration,
	}
}
