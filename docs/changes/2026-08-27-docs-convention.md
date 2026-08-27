---
status: draft
last_verified: 2026-08-27
owner: Bot MMO
related_files:
  - AGENTS.md
  - CONTEXT.md
  - docs/README.md
  - docs/changes/_TEMPLATE.md
related_tests: []
---

# 2026-08-27 — Thiết lập hệ thống tài liệu cho agent

## Triệu chứng

Không có tài liệu cấu trúc nào cho agent đọc: repo thiếu AGENTS.md, CONTEXT.md; [docs/](../) chỉ có vài file rời rạc không metadata, không quy ước đặt tên, không vòng đời. Mỗi lần agent sửa code thì không có bản ghi "tại sao đổi, đổi gì, kiểm tra bằng gì", nên lần sửa sau dễ đoán sai và lặp lại bug.

## Cách tái hiện

1. Mở repo, kiểm tra root: không có `AGENTS.md`, `CONTEXT.md`.
2. Liệt kê `docs/`: các file ADR/kế hoạch COMBOS nằm lẫn với docs vận hành, không có header metadata.

## Nguyên nhân gốc

Quy trình làm việc trước đây chỉ commit code; tri thức về quyết định, luồng dữ liệu và cách debug chỉ tồn tại trong hội thoại ngắn hạn của agent, mất sau mỗi phiên.

## Giải pháp

Thiết lập hệ thống docs đã thống nhất với người dùng (20 quyết định trong phiên 2026-08-27):

- [AGENTS.md](../../AGENTS.md) — index + quy tắc bắt buộc: agent đọc theo task, phải xác minh `related_files`/`related_tests` trước khi sửa, không tự đánh dấu `status: current`.
- [CONTEXT.md](../../CONTEXT.md) — glossary thuật ngữ domain, chỉ định nghĩa, không chi tiết triển khai.
- [docs/README.md](../README.md) — cấu trúc thư mục và metadata bắt buộc (`status`, `last_verified`, `owner`, `related_files`, `related_tests`).
- [docs/changes/_TEMPLATE.md](_TEMPLATE.md) — template triệu chứng → tái hiện → nguyên nhân gốc → giải pháp → files → API/luồng dữ liệu → test → vấn đề còn lại → xác minh.
- Ngưỡng ghi change doc: thay đổi ảnh hưởng hành vi, API, database, bảo mật, luồng dữ liệu, hoặc cách debug.
- Vòng đời: `draft` → người dùng duyệt → `current`; hết đúng thì `deprecated` + file mới link ngược. Đổi kiến trúc thì cập nhật cả `docs/ARCHITECTURE.md` lẫn `docs/changes/`.
- Đặt tên `YYYY-MM-DD-<tên>.md` trong [docs/changes/](.) .
- Bảo mật: docs chỉ chứa tên trường và placeholder (`<authorization-code>`, `<session-id>`…), không chứa secret thật.
- Troubleshooting xếp theo nhóm lỗi ([docs/troubleshooting/](../troubleshooting/)); luồng nghiệp vụ + module lớn ở [docs/flows/](../flows/).
- Docs commit cùng code.

## File đã thay đổi

- `AGENTS.md` — mới: index + quy tắc cho agent.
- `CONTEXT.md` — mới: glossary khởi tạo từ thuật ngữ trong code.
- `docs/README.md` — mới: quy ước cấu trúc/metadata.
- `docs/changes/_TEMPLATE.md` — mới: template change doc.
- `docs/changes/2026-08-27-docs-convention.md` — file này.
- `docs/adr/0001-combos.md` — di chuyển từ `docs/COMBOS_ADR.md` (git mv).
- Thư mục mới: `docs/flows/`, `docs/troubleshooting/`, `docs/adr/`.

## API / luồng dữ liệu bị ảnh hưởng

Không. Thay đổi chỉ ở tài liệu; không đụng code chạy.

## Test

Không có test code cho tài liệu. Kiểm tra bằng cấu trúc file và link nội bộ (mục Xác minh).

## Vấn đề còn lại

- `docs/ARCHITECTURE.md` chưa được tạo — cần viết khi có task đụng kiến trúc tổng thể.
- Các file cũ (`admin-chat.md`, `api-keys-admin.md`, `build-and-deploy.md`, `COMBOS_IMPLEMENTATION_PLAN.md`, `COMBOS_MODEL_ADVERTISEMENT_ROLLOUT.md`) chưa có metadata header — sẽ bổ sung khi từng file được động tới.
- `docs/flows/` và `docs/troubleshooting/` còn trống — điền dần theo task thật, không viết trước.

## Xác minh

- File đã kiểm tra: `ls -R docs` sau khi tạo cấu trúc; xác nhận `docs/adr/0001-combos.md` tồn tại và `docs/COMBOS_ADR.md` không còn ở vị trí cũ.
- Lệnh đã chạy: `git mv docs/COMBOS_ADR.md docs/adr/0001-combos.md` (thành công), `ls -R docs`.
- Kết quả: cấu trúc đúng như thiết kế; không chạy test code vì không thay đổi code.
- Thời gian: 2026-08-27