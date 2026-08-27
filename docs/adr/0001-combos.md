# ADR: Combo runtime defaults

**Status:** Accepted for the base Combo release  
**Date:** 2026-07-30

## Decisions

1. Public JSON request bodies are capped at 8 MiB. Combo names and model IDs are capped at 128 UTF-8 bytes after trimming. A successful Fusion panel output is capped at 256 KiB; oversized output fails that attempt rather than being truncated. Fusion permits at most 16 globally in-flight panel calls and returns 503 when the semaphore is saturated.
2. Fusion's post-quorum grace window is a fixed 8 seconds. `fusionTimeoutMs` remains the per-Combo hard timeout.
3. Public protocol responses preserve the exact requested Combo model string. Effective serving models remain internal metadata.
4. Claude `count_tokens` uses the existing protocol-neutral local estimator over the original request. It does not reserve rotation, select an account, invoke a panel/judge, or make a network request.
5. Attempt observability uses schema v5: one parent public request plus revisioned child attempt rows. Parent retention remains capped at the newest 10,000 requests, with child attempts deleted by cascade. Billing aggregates all child attempts while public protocol usage describes the serving result.
6. Direct known models fail open when the Combo registry is unavailable. Combo routing is entered only after positive resolution; an unresolved non-direct model may report registry unavailability only when registry loading failed. Direct model identity always wins over a colliding Combo name.

## Base-release invariants

- Round-robin reservation happens once before network activity and is revision-bound.
- Public output cannot switch candidate or account after the first public byte.
- Fusion panels are non-streaming and tool-free; judge tools are disabled.
- One successful Fusion panel is returned directly; zero successes returns 503; multiple successes below quorum return a quorum error.
- Prompt, tool arguments, panel content, and credentials are not logged.

## Rejected alternatives

- Configurable grace was rejected because it expands persistence/API/UI without a demonstrated runtime need.
- Candidate-specific token counting was rejected because it would mutate rotation or make estimates depend on routing state.
- One parent log row per attempt was rejected because it breaks public-request retention and UI semantics.
- Truncating panel output was rejected because it can silently change the material presented to the judge.
- Treating every unknown model as a Combo during registry failure was rejected because it changes existing direct-model errors.

## Required characterization and acceptance tests

- Request aliases reject bodies over 8 MiB with 413; model IDs enforce byte-length boundaries.
- Fusion tests cover output caps, semaphore saturation/cancellation, fixed grace, quorum and timeout.
- Every protocol preserves requested Combo identity in streaming and non-streaming output.
- Direct and Combo `count_tokens` estimates match and rotation state remains unchanged.
- Schema v4→v5, cascade/prune, retries, panel/judge, partial usage and no-double-counting are covered.
- Registry failure tests prove known direct models continue, Combo requests fail clearly, and healthy-registry unknown-model behavior remains unchanged.

9router is characterization input only. Its 8-second grace and tool-free/non-streaming panel behavior are portable; its in-memory rotation, case-sensitive lookup, below-quorum judging, judge tools, and unbounded bodies/output/concurrency are intentionally not copied.
