# CONTEXT.md — Glossary thuật ngữ

Chỉ chứa định nghĩa thuật ngữ domain. Không chứa chi tiết triển khai (đọc [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) cho phần đó).

- **Account** — Một tài khoản upstream (Google/Antigravity, Codex, Grok/xAI, Kiro SSO, BuilderID…) đã được đăng nhập và lưu trong hệ thống để phục vụ request.
- **Provider** — Nhà cung cấp upstream mà Account thuộc về (Antigravity, Codex, xAI, Kiro, AWS BuilderID…). Mỗi provider có một flow đăng nhập OAuth riêng trong [auth/](auth/).
- **OAuth session** — Phiên đăng nhập tạm thời phía server khi thêm Account: giữ `sessionId`, state, thời hạn; frontend poll cho tới khi hoàn tất. Session chỉ dùng một lần và bị huỷ khi hết hạn / cancel / hoàn thành.
- **Callback** — URL mà provider redirect về sau khi người dùng đăng nhập. Antigravity dùng redirect cố định `http://localhost:3129/callback`.
- **callbackMode** — `automatic`: trình duyệt cùng máy với server, callback tới được listener. `manual`: server ở xa/container, người dùng phải copy URL callback từ thanh địa chỉ rồi paste vào dialog.
- **Manual completion** — Người dùng paste callback URL/code vào dialog; server gắn với OAuth session đang hoạt động, kiểm tra `state`, hoàn tất một lần duy nhất.
- **Combo** — Tên model ảo do Kiro-Go định nghĩa, ánh xạ tới một hoặc nhiều model/account thật phía sau. Client gọi Combo như một model bình thường.
- **Fusion** — Cơ chế gọi song song nhiều panel (model) cho một request Combo, chọn kết quả theo quorum + judge.
- **Pool** — Nhóm Account cùng provider được quản lý chung: chọn account, xoay vòng, đánh dấu lỗi.
- **Failover** — Khi account/model hiện tại lỗi giữa chừng request, hệ thống tự chuyển sang account/model khác.
- **API key** — Khóa do admin cấp cho client bên ngoài gọi vào proxy. Mỗi key gắn với giới hạn/quyền riêng.
- **Admin session** — Phiên đăng nhập giao diện quản trị (`/admin`), bảo vệ bằng cookie + CSRF token. Mọi endpoint `/admin/api/*` yêu cầu cả hai.
- **9router** — Repo tham chiếu nằm trong [9router/](9router/), nguồn pattern cho các flow auth/session của Kiro-Go. Không phải code chạy trong production.
- **Attempt** — Một lần thử phục vụ request (schema v5: 1 request cha + các attempt con có revision). Billing tính trên tất cả attempt con.