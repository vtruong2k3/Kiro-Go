# Hướng dẫn Build & Deploy Kiro-Go

Tài liệu này mô tả cách build và deploy Kiro-Go, gồm build binary trực tiếp bằng Go và deploy bằng Docker / Docker Compose.

## Yêu cầu

| Công cụ | Phiên bản | Dùng cho |
| ------- | --------- | -------- |
| Go | 1.23+ | Build từ source |
| Docker | 24+ | Build image / chạy container |
| Docker Compose | v2 (`docker compose`) | Deploy nhiều bước |

## 1. Build

### 1.1. Build binary bằng Go (local)

Chạy từ thư mục gốc của project:

```bash
go mod download          # tải dependency (chỉ cần lần đầu)
go build -o kiro-go .    # tạo binary kiro-go
```

Kết quả là file `kiro-go` (Linux/macOS) trong thư mục hiện tại. Chạy thử:

```bash
./kiro-go
```

Mặc định service lắng nghe ở `0.0.0.0:8080`. Config tự tạo tại `data/config.json`.

#### Cross-compile (build cho nền tảng khác)

```bash
# Windows 64-bit
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o kiro-go.exe .

# Linux ARM64
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o kiro-go-arm64 .
```

`CGO_ENABLED=0` tạo binary tĩnh, không phụ thuộc thư viện hệ thống — thuận tiện để đóng gói.

### 1.2. Build Docker image

`Dockerfile` dùng multi-stage build: stage `builder` (Go alpine) biên dịch binary, stage cuối (alpine) chỉ chứa binary + thư mục `web`, nên image gọn nhẹ.

```bash
# Build image thủ công
docker build -t kiro-go:latest .

# Hoặc để Docker Compose build (đọc từ docker-compose.yml)
docker compose build
```

Dockerfile hỗ trợ cross-platform qua `TARGETOS` / `TARGETARCH`. Ví dụ build cho ARM64:

```bash
docker buildx build --platform linux/arm64 -t kiro-go:arm64 .
```

## 2. Deploy

### 2.1. Docker Compose (khuyến nghị)

Đây là cách deploy nhanh nhất, đã cấu hình sẵn port, volume và biến môi trường.

```bash
mkdir -p data            # thư mục lưu config/account, cần tạo trước
docker compose up -d     # build (nếu cần) và chạy nền
```

Kiểm tra trạng thái và log:

```bash
docker compose ps
docker compose logs -f kiro-go
```

Khi thấy log dạng dưới đây là service đã sẵn sàng:

```
INFO  Kiro-Go starting on http://0.0.0.0:8080 (log level: info)
INFO  Admin panel: http://0.0.0.0:8080/admin
```

Mở `http://localhost:8080/admin` để đăng nhập.

#### Cập nhật / redeploy sau khi sửa code

```bash
docker compose up -d --build   # build lại image rồi thay container
```

#### Dừng service

```bash
docker compose down            # dừng và xóa container (giữ nguyên ./data)
```

Dữ liệu account/config nằm ở `./data` trên host (mount vào `/app/data`), nên vẫn còn sau khi `down` / rebuild.

### 2.2. Docker Run (không dùng Compose)

```bash
docker run -d \
  --name kiro-go \
  -p 8080:8080 \
  -e ADMIN_PASSWORD=mat_khau_cua_ban \
  -v "$(pwd)/data:/app/data" \
  --restart unless-stopped \
  kiro-go:latest
```

### 2.3. Chạy binary trực tiếp (không Docker)

Phù hợp khi deploy lên VPS/server thường:

```bash
./kiro-go
```

Nên chạy dưới một service manager (systemd) để tự khởi động lại. Ví dụ file `/etc/systemd/system/kiro-go.service`:

```ini
[Unit]
Description=Kiro-Go API Proxy
After=network.target

[Service]
WorkingDirectory=/opt/kiro-go
ExecStart=/opt/kiro-go/kiro-go
Environment=ADMIN_PASSWORD=mat_khau_cua_ban
Restart=always

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now kiro-go
```

## 3. Cấu hình

### Biến môi trường

| Biến | Mô tả | Mặc định |
| ---- | ----- | -------- |
| `CONFIG_PATH` | Đường dẫn file config | `data/config.json` |
| `ADMIN_PASSWORD` | Mật khẩu admin panel (ghi đè config) | - |
| `LOG_LEVEL` | Mức log (`debug`/`info`/`warn`/`error`) | `info` |
| `KIRO_SSO_CALLBACK_BIND` | Interface listener callback Enterprise SSO trong container | loopback |

> Mật khẩu admin mặc định là `changeme`. Hãy đổi qua `ADMIN_PASSWORD` hoặc trong admin panel trước khi chạy production.

### Cổng

| Cổng | Mục đích |
| ---- | -------- |
| `8080` | HTTP API + admin panel |
| `3128` | Callback Enterprise SSO (chỉ dùng khi đăng nhập IAM Identity Center) |

## 4. Kiểm tra sau khi deploy

```bash
# Admin panel (kỳ vọng HTTP 200)
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8080/admin

# Gọi thử API Claude (sau khi đã thêm account trong admin)
curl http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"claude-sonnet-4.5","max_tokens":1024,"messages":[{"role":"user","content":"Hello!"}]}'
```

## 5. Xử lý sự cố

- **Port 8080 đã bị chiếm**: đổi mapping trong `docker-compose.yml` (`"8081:8080"`) hoặc dừng process đang giữ port.
- **Mất config sau redeploy**: kiểm tra volume `./data` đã được mount đúng chưa; config nằm ở `data/config.json`.
- **Cảnh báo `version` obsolete khi chạy compose**: vô hại, có thể xóa dòng `version:` ở đầu `docker-compose.yml`.
- **Xem log chi tiết**: đặt `LOG_LEVEL=debug` rồi restart container.
