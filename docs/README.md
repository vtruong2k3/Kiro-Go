# docs/ — Cấu trúc tài liệu dự án

Mọi tài liệu viết bằng **tiếng Việt**, giữ nguyên tên kỹ thuật tiếng Anh. Quy tắc đầy đủ cho agent nằm ở [AGENTS.md](../AGENTS.md); thuật ngữ domain ở [CONTEXT.md](../CONTEXT.md).

## Thư mục

- **[changes/](changes/)** — Nhật ký thay đổi. Mỗi thay đổi đáng kể = 1 file `YYYY-MM-DD-<tên>.md` theo [changes/_TEMPLATE.md](changes/_TEMPLATE.md). Ghi khi thay đổi ảnh hưởng: hành vi, API, database, bảo mật, luồng dữ liệu, hoặc cách debug.
- **[flows/](flows/)** — Luồng nghiệp vụ end-to-end và tài liệu module lớn (vd: luồng đăng nhập Antigravity, luồng request qua proxy).
- **[troubleshooting/](troubleshooting/)** — Lỗi đã gặp, xếp theo nhóm: `authentication.md`, `network.md`, `database.md`… Lỗi phức tạp được tách file riêng và link từ file nhóm.
- **[adr/](adr/)** — Quyết định kiến trúc: chỉ ghi khi quyết định **khó đảo ngược + gây bất ngờ + có trade-off thật**.
- **[ARCHITECTURE.md](ARCHITECTURE.md)** — *(sẽ tạo)* Kiến trúc hiện tại. Khi kiến trúc đổi: cập nhật file này **và** ghi thêm một file trong `changes/` nêu lý do.

## Metadata bắt buộc (đầu mỗi file .md)

```markdown
---
status: current | draft | deprecated
last_verified: YYYY-MM-DD
owner: <tên>
related_files:
  - auth/antigravity.go
related_tests:
  - auth/antigravity_test.go
---
```

- `draft`: agent mới soạn, chờ người dùng duyệt. **Chỉ người dùng** chuyển sang `current`.
- Tài liệu sai/lỗi thời: đổi `status: deprecated`, tạo file mới, link ngược về file cũ — không sửa trực tiếp.

## Bảo mật

Không ghi secret thật vào bất kỳ tài liệu nào: authorization code, state, access/refresh token, client secret, cookie/session ID, callback URL đầy đủ, email tài khoản. Chỉ dùng placeholder: `<authorization-code>`, `<session-id>`, `<refresh-token>`…