---
status: draft
last_verified: 2026-08-27
owner: Bot MMO
related_files:
  - auth/antigravity.go
  - proxy/handler.go
  - web/frontend/src/hooks/useOAuthFlow.ts
  - web/frontend/src/features/auth-modals/flows/AntigravityFlow.tsx
  - web/frontend/src/features/auth-modals/OAuthFlowView.tsx
  - web/frontend/src/types/auth.ts
related_tests:
  - auth/antigravity_test.go
---

# 2026-08-27 — Fix độ tin cậy đăng nhập Antigravity

## Triệu chứng

- Đăng nhập Antigravity thất bại khi server chạy trên VPS/container: Google redirect về `http://localhost:3129/callback` trên máy người dùng thay vì server, backend không bao giờ nhận callback.
- Frontend tự mở popup từ `useEffect` nên bị trình duyệt chặn; người dùng không có cách nào hoàn tất đăng nhập.
- Polling dừng ngay khi gặp một lỗi mạng tạm thời (`ERR_NETWORK_CHANGED`).

## Cách tái hiện

1. Chạy Kiro-Go trên máy khác với máy mở trình duyệt, thêm tài khoản Antigravity.
2. Đăng nhập xong, trang callback `localhost:3129` không tải được; dialog không bao giờ hoàn tất.
3. Hoặc: tắt/mất mạng thoáng qua trong lúc poll → flow báo lỗi ngay lập tức.

## Nguyên nhân gốc

- Redirect URI cố định `http://localhost:3129/callback` do Google quy định; không có cơ chế manual completion gắn với session cho triển khai từ xa.
- Manual completion cũ nhận code/URL không cần `sessionId`, không kiểm tra `state` khớp session, không chống hoàn tất trùng lặp → race giữa `/poll` và `/complete` có thể tạo tài khoản nhân bản.
- `useOAuthFlow` khởi động trong `useEffect` (dễ bị chặn popup) và coi mọi lỗi mạng là terminal.

## Giải pháp

Củng cố các luồng sẵn có theo pattern của 9router, không thay thiết kế auth:

- **Backend**: `/start` trả thêm `callbackMode` (`automatic`/`manual`) + `callbackHint` để UI hiển thị hướng dẫn theo môi trường triển khai. Listener mặc định chỉ bind loopback; `ANTIGRAVITY_CALLBACK_BIND` chỉ là override triển khai có chủ đích.
- **Manual completion gắn session**: frontend gửi `{ sessionId, callbackUrl }`; backend yêu cầu session hợp lệ, `state` trong URL phải khớp session state, claim session nguyên tử một lần trước bootstrap nên `/poll`/`/complete` không thể hoàn tất trùng.
- **Callback page**: HTML chung không phản chiếu input, header `Cache-Control: no-store`, CSP hạn chế, `X-Content-Type-Options: nosniff`.
- **Frontend**: nút "Start Antigravity login" rõ ràng — click mới mở tab (`window.open('', '_blank')` đồng bộ rồi navigate tới `signInUrl`), popup bị chặn thì hiện link/dán thủ công. Polling retry lỗi mạng/5xx với backoff có giới hạn, generation ref chặn response cũ cập nhật flow mới, cleanup khi cancel/unmount/thành công/lỗi terminal.
- Không log/ghi authorization code, state, token, callback URL đầy đủ vào response hoặc lỗi.

## File đã thay đổi

- `auth/antigravity.go` — manual completion gắn session, kiểm tra state, claim một lần, callback page no-store/CSP.
- `auth/antigravity_test.go` — thêm 3 test: parse callback (URL/query/raw code), header callback page, manual không có session bị từ chối.
- `proxy/handler.go` — start response thêm `callbackMode`/`callbackHint`; complete yêu cầu `sessionId`.
- `web/frontend/src/hooks/useOAuthFlow.ts` — `start(args, popup?)`, retry/backoff, generation guard.
- `web/frontend/src/features/auth-modals/flows/AntigravityFlow.tsx` — khởi động bằng click, pre-open tab, fallback khi popup bị chặn.
- `web/frontend/src/features/auth-modals/OAuthFlowView.tsx`, `web/frontend/src/types/auth.ts` — hiển thị manual theo mode, type cho trường mới.

## API / luồng dữ liệu bị ảnh hưởng

- `POST /admin/api/auth/antigravity/start` — response thêm `callbackMode`, `callbackHint`.
- `POST /admin/api/auth/antigravity/complete` — body yêu cầu `sessionId`; từ chối session lạ/hết hạn/đã dùng, `state` lệch.
- Các endpoint khác không đổi. Vẫn yêu cầu admin session + CSRF.

## Test

- Go: `TestExtractAntigravityCallback`, `TestAntigravityCallbackPageNoStore`, `TestManualAntigravityRequiresSession` trong `auth/antigravity_test.go`. Chạy: `go test ./...`.
- Frontend: `cd web/frontend && pnpm typecheck && pnpm lint && pnpm test:run`.

## Vấn đề còn lại

- Chưa test thực tế trên VPS có forward port 3129; cần xác minh tay khi deploy.
- `docs/flows/` chưa có tài liệu luồng đăng nhập Antigravity end-to-end — viết khi có task đụng luồng này lần nữa.

## Xác minh

- File đã kiểm tra: `auth/antigravity.go`, `proxy/handler.go`, `web/frontend/src/hooks/useOAuthFlow.ts`, `web/frontend/src/features/auth-modals/flows/AntigravityFlow.tsx`.
- Lệnh đã chạy: `go test ./...` (pass), `pnpm typecheck` (sạch), `pnpm lint` (chỉ cảnh báo cũ), `pnpm test:run` (pass).
- Kết quả: toàn bộ pass; không dán output chứa giá trị nhạy cảm.
- Thời gian: 2026-08-27

</parameter>