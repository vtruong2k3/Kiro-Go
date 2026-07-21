package proxy

import (
	"kiro-go/config"
	"kiro-go/logger"
	"kiro-go/pool"
	"strings"
)

// overridePayloadModel rewrites the model id that upstream providers read from the
// shared KiroPayload, without touching the client-facing model name held by the
// handler. Every provider resolves the model from one of these three places, so
// all three are kept in sync.
func overridePayloadModel(payload *KiroPayload, model string) {
	if payload == nil {
		return
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	payload.ConversationState.CurrentMessage.UserInputMessage.ModelID = model
	if payload.SourceClaude != nil {
		payload.SourceClaude.Model = model
	}
	if payload.SourceOpenAI != nil {
		payload.SourceOpenAI.Model = model
	}
}

// providerLabel returns a short stable name for the upstream that will serve an
// account. Used for admin-only request logs so operators can see whether a call
// was answered by Kiro, Grok, Codex, or Antigravity after a fallback.
func providerLabel(account *config.Account) string {
	if account == nil {
		return ""
	}
	switch {
	case isAntigravityAccount(account):
		return "antigravity"
	case isCodexAccount(account):
		return "codex"
	case isGrokAccount(account):
		return "grok"
	case isRemoteKiroAccount(account):
		return "remotekiro"
	case isKiroAPIKeyAccount(account):
		return "kiro-apikey"
	default:
		if p := strings.TrimSpace(account.Provider); p != "" {
			return strings.ToLower(p)
		}
		if p := strings.TrimSpace(account.AuthMethod); p != "" {
			return strings.ToLower(p)
		}
		return "kiro"
	}
}

// maxFallbackAccountAttempts caps how many alternate-provider accounts we try
// after native accounts are exhausted (across the whole fallback chain).
const maxFallbackAccountAttempts = 6

// nextAccountForAttempt picks the next account for a request attempt.
//
// Phase 1 (native): route by the original client model until either the pool has
// no more native accounts or the native attempt budget is spent.
// Phase 2 (fallback): walk the configured ModelFallback chain. For each target
// model, keep selecting accounts (skipping excluded) until the pool returns nil,
// then advance to the next target. The client-facing model name is NOT changed —
// only the payload ModelID is rewritten for the alternate provider.
//
// nativeDone / nativeAttempts / fallbackIdx / fallbackAttempts are attempt-loop
// state owned by the caller.
func nextAccountForAttempt(
	p *pool.AccountPool,
	originalModel string,
	payload *KiroPayload,
	excluded map[string]bool,
	nativeDone *bool,
	nativeAttempts *int,
	fallbackIdx *int,
	fallbackAttempts *int,
) (account *config.Account, usedFallbackModel string) {
	if p == nil {
		return nil, ""
	}

	// Phase 1: native accounts for the original model.
	if nativeDone == nil || !*nativeDone {
		underBudget := nativeAttempts == nil || *nativeAttempts < maxAccountRetryAttempts
		if underBudget {
			acc := p.GetNextForModelExcluding(originalModel, excluded)
			if acc != nil {
				if nativeAttempts != nil {
					*nativeAttempts++
				}
				return acc, ""
			}
		}
		if nativeDone != nil {
			*nativeDone = true
		}
	}

	// Phase 2: configured cross-provider fallback chain.
	targets := config.GetModelFallback(originalModel)
	if len(targets) == 0 || fallbackIdx == nil {
		return nil, ""
	}
	if fallbackAttempts != nil && *fallbackAttempts >= maxFallbackAccountAttempts {
		return nil, ""
	}

	for *fallbackIdx < len(targets) {
		target := strings.TrimSpace(targets[*fallbackIdx].Model)
		if target == "" || strings.EqualFold(target, originalModel) {
			*fallbackIdx++
			continue
		}
		acc := p.GetNextForModelExcluding(target, excluded)
		if acc == nil {
			// No more accounts for this target — move on.
			*fallbackIdx++
			continue
		}
		if fallbackAttempts != nil {
			*fallbackAttempts++
		}
		overridePayloadModel(payload, target)
		logger.Warnf("[ModelFallback] %s exhausted native pool → routing via %s account %s (upstream model %s)",
			originalModel, providerLabel(acc), accountLabel(acc), target)
		return acc, target
	}
	return nil, ""
}

func accountLabel(account *config.Account) string {
	if account == nil {
		return ""
	}
	if account.Email != "" {
		return account.Email
	}
	if account.Nickname != "" {
		return account.Nickname
	}
	return account.ID
}

// maxAttemptsForModel returns how many account tries the handler should allow:
// native budget + fallback account budget.
func maxAttemptsForModel(model string) int {
	n := maxAccountRetryAttempts
	if len(config.GetModelFallback(model)) > 0 {
		n += maxFallbackAccountAttempts
	}
	return n
}
