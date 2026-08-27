# AGENTS.md — Chỉ mục tài liệu & quy tắc bắt buộc cho agent

File này là điểm vào duy nhất. **Đọc file này trước, sau đó chỉ đọc tài liệu liên quan đến task — không đọc tất cả.**

## Cấu trúc tài liệu

| File / thư mục | Nội dung | Khi nào đọc |
|---|---|---|
| [CONTEXT.md](CONTEXT.md) | Glossary thuật ngữ domain (Account, Provider, Combo, Pool…) | Task đụng đến khái niệm chưa rõ nghĩa |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Kiến trúc hiện tại của hệ thống | Task cần hiểu tổng quan / sửa logic liên module |
| [docs/flows/](docs/flows/) | Luồng nghiệp vụ end-to-end và module lớn | Task sửa một luồng cụ thể (auth, chat, proxy…) |
| [docs/troubleshooting/](docs/troubleshooting/) | Lỗi đã gặp, xếp theo nhóm (authentication, network, database…) | Đang fix bug hoặc gặp lỗi quen thuộc |
| [docs/changes/](docs/changes/) | Nhật ký thay đổi: mỗi lần đổi code ghi 1 file | Trước khi sửa vùng code từng được thay đổi gần đây |
| [docs/adr/](docs/adr/) | Quyết định kiến trúc khó đảo ngược | Trước khi định đổi thiết kế nền tảng |

## Quy tắc bắt buộc khi đọc

1. Chỉ đọc tài liệu có `status: current`. Tài liệu `status: draft` là bản nháp chưa được duyệt — dùng tham khảo nhưng không tin tuyệt đối. `status: deprecated` thì bỏ qua và đi theo link tới tài liệu thay thế.
2. Trước khi fix bug hoặc sửa code: **mở `related_files` và `related_tests` trong metadata của tài liệu liên quan để xác minh chúng còn tồn tại và đúng như mô tả.** Nếu tài liệu lệch với code, tin code và báo cho người dùng.
3. Với luồng quan trọng, mọi khẳng định trong docs phải trích dẫn `file:dòng`. Không bịa hành vi không có trong code.

## Quy tắc bắt buộc khi viết

1. **Kết thúc một task mà thay đổi hành vi, API, database, bảo mật, luồng dữ liệu hoặc cách debug → bắt buộc tạo `docs/changes/YYYY-MM-DD-<tên>.md`** theo [_TEMPLATE.md](docs/changes/_TEMPLATE.md), đặt `status: draft`, commit cùng với code.
2. Agent soạn thảo, **người dùng là người chuyển `status: draft` → `current`**. Agent không bao giờ tự đánh dấu `current`.
3. Khi kiến trúc thay đổi: cập nhật **cả hai** — `docs/ARCHITECTURE.md` (trạng thái mới) và `docs/changes/` (lý do thay đổi). Agent chỉ được đề xuất bản vá cho ARCHITECTURE.md, không tự merge.
4. Tài liệu hết đúng: **không sửa trực tiếp** — đánh dấu `status: deprecated`, tạo tài liệu mới và link ngược về tài liệu cũ.
5. **Không ghi secret thật vào docs**: không authorization code, state, access/refresh token, client secret, cookie/session ID, callback URL đầy đủ, email tài khoản. Chỉ dùng tên trường và placeholder dạng `<authorization-code>`, `<session-id>`.
6. Mục Xác minh trong change doc ghi: file đã kiểm tra, lệnh đã chạy (vd `go test ./auth ./proxy`), kết quả, thời gian — nhưng không dính output nhạy cảm.
7. Ngôn ngữ docs: **tiếng Việt**; giữ nguyên tên kỹ thuật tiếng Anh (tên hàm, endpoint, biến).

## Lệnh kiểm tra nhanh

```bash
# Backend (Go)
go build ./... && go test ./...

# Frontend (web/frontend)
cd web/frontend && pnpm typecheck && pnpm lint && pnpm test:run
```