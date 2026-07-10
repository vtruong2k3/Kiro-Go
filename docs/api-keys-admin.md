# API Keys Admin API

Tài liệu cho tool tự động tạo/quản lý API key qua admin API.

## Xác thực

Mọi endpoint dưới `/admin/api` yêu cầu mật khẩu admin, truyền qua **một trong hai**:

- Header: `X-Admin-Password: <password>`
- Cookie: `admin_password=<password>`

Sai/thiếu mật khẩu → `401 {"error":"Unauthorized"}`.

`<password>` là giá trị `password` trong `config.json` (server bind mặc định `0.0.0.0:8080`).

Base URL trong các ví dụ: `http://localhost:8080`.

---

## Mô hình dữ liệu

Đối tượng key khi đọc về (`apiKeyView`):

| Field           | Kiểu    | Ý nghĩa                                                       |
|-----------------|---------|--------------------------------------------------------------|
| `id`            | string  | UUID, dùng cho mọi thao tác update/delete                    |
| `name`          | string  | Nhãn tuỳ ý                                                   |
| `keyMasked`     | string  | Key đã che (`sk-123****7890`) — **không** phải key thật      |
| `enabled`       | bool    | Có được phép auth không                                       |
| `migrated`      | bool    | Key được migrate từ config cũ                                 |
| `createdAt`     | int64   | Unix giây, thời điểm tạo                                      |
| `lastUsedAt`    | int64   | Unix giây, lần dùng gần nhất (0 nếu chưa dùng)               |
| `tokenLimit`    | int64   | Giới hạn token (0 = không giới hạn)                           |
| `creditLimit`   | float64 | Giới hạn credit (0 = không giới hạn)                          |
| `tokensUsed`    | int64   | Token đã dùng (tích luỹ)                                      |
| `creditsUsed`   | float64 | Credit đã dùng (tích luỹ)                                     |
| `requestsCount` | int64   | Số request đã thực hiện                                       |
| `expiresAt`     | int64   | Unix giây, thời điểm hết hạn (0 = vĩnh viễn)                  |
| `expired`       | bool    | `true` khi `expiresAt > 0 && now >= expiresAt`               |

> **Key thật (`sk-...`) được trả về khi tạo** (`POST`). List/get chỉ có `keyMasked`. Có thể lấy lại cleartext qua `GET /admin/api/api-keys/{id}/reveal` (cần admin password).

### Ngữ nghĩa `expiresAt`

`expiresAt` là mốc **hết hiệu lực** (Unix giây). Key hợp lệ khi `now < expiresAt`. Muốn key sống hết trọn ngày D, đặt `expiresAt` = 00:00 của ngày D+1 (theo múi giờ mong muốn).

Ví dụ: muốn key chết vào đầu ngày 6/3/2026 (tức dùng được hết 5/3):
```
expiresAt = unix("2026-03-06 00:00:00")
```

---

## Endpoints

### 1. Liệt kê key — `GET /admin/api/api-keys`

```bash
curl http://localhost:8080/admin/api/api-keys \
  -H "X-Admin-Password: $ADMIN_PW"
```

Response `200`:
```json
{
  "apiKeys": [
    {
      "id": "b3f1...",
      "name": "ci-bot",
      "keyMasked": "sk-abc****1234",
      "enabled": true,
      "createdAt": 1751449200,
      "tokenLimit": 0,
      "creditLimit": 0,
      "tokensUsed": 0,
      "creditsUsed": 0,
      "requestsCount": 0,
      "expiresAt": 1772928000,
      "expired": false
    }
  ]
}
```


### 1b. Hiện cleartext key — `GET /admin/api/api-keys/{id}/reveal`

Dùng khi admin cần copy lại key đã tạo (list/get chỉ trả `keyMasked`).

```bash
curl http://localhost:8080/admin/api/api-keys/$ID/reveal   -H "X-Admin-Password: $ADMIN_PW"
```

Response `200`:
```json
{
  "success": true,
  "id": "b3f1...",
  "key": "sk-...."
}
```

`404` nếu id không tồn tại. Endpoint này chỉ dành cho admin đã xác thực; không dùng cho client `/v1/*`.

### 2. Tạo key — `POST /admin/api/api-keys`

Request body:

| Field         | Kiểu    | Bắt buộc | Ghi chú                                                     |
|---------------|---------|----------|-------------------------------------------------------------|
| `name`        | string  | Không    | Nhãn                                                        |
| `key`         | string  | Không    | Để trống → server tự sinh `sk-<64 hex>`. Không được trùng.  |
| `enabled`     | bool    | Không    | Mặc định `true`                                             |
| `tokenLimit`  | int64   | Không    | 0 = không giới hạn                                          |
| `creditLimit` | float64 | Không    | 0 = không giới hạn                                          |
| `expiresAt`   | int64   | Không    | Unix giây; 0/bỏ trống = vĩnh viễn                          |

```bash
curl -X POST http://localhost:8080/admin/api/api-keys \
  -H "X-Admin-Password: $ADMIN_PW" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "ci-bot",
    "enabled": true,
    "tokenLimit": 1000000,
    "expiresAt": 1772928000
  }'
```

Response `200` (**key thật nằm ở `key`, chỉ hiện lần này**):
```json
{
  "success": true,
  "id": "b3f1...",
  "key": "sk-abc...1234",
  "apiKey": { "...apiKeyView..." }
}
```

Lỗi `400`: `{"error":"api key already exists"}` (trùng `key`) hoặc `{"error":"api key value must not be empty"}`.

### 3. Xem 1 key — `GET /admin/api/api-keys/{id}`

Trả về `apiKeyView`. `404` nếu không tìm thấy.

### 4. Sửa key — `PUT /admin/api/api-keys/{id}`

Chỉ các field **có mặt trong body** mới bị đổi (patch). Các field `*` nhận `null`/vắng = giữ nguyên.

| Field         | Kiểu     | Ghi chú                                            |
|---------------|----------|----------------------------------------------------|
| `name`        | string   | Đổi nhãn                                           |
| `key`         | string   | Đổi giá trị key (không được trùng key khác)        |
| `enabled`     | bool     | Bật/tắt                                            |
| `tokenLimit`  | int64    | Đặt giới hạn token                                 |
| `creditLimit` | float64  | Đặt giới hạn credit                                |
| `expiresAt`   | int64    | Đặt hạn mới; **gửi `0` để xoá hạn (sống vĩnh viễn)** |

```bash
# Gia hạn key thêm tới mốc mới
curl -X PUT http://localhost:8080/admin/api/api-keys/b3f1... \
  -H "X-Admin-Password: $ADMIN_PW" \
  -H "Content-Type: application/json" \
  -d '{"expiresAt": 1775520000}'

# Vô hiệu hoá key
curl -X PUT http://localhost:8080/admin/api/api-keys/b3f1... \
  -H "X-Admin-Password: $ADMIN_PW" \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}'
```

Response `200`: `{"success": true, "apiKey": { ... }}`. `404` nếu id không tồn tại.

### 5. Xoá key — `DELETE /admin/api/api-keys/{id}`

```bash
curl -X DELETE http://localhost:8080/admin/api/api-keys/b3f1... \
  -H "X-Admin-Password: $ADMIN_PW"
```

Response `200`: `{"success": true}`. Idempotent (xoá id không tồn tại vẫn `success`).

### 6. Reset usage — `POST /admin/api/api-keys/{id}/reset-usage`

Đưa `tokensUsed`/`creditsUsed`/`requestsCount` về 0 (không đụng `expiresAt`).

```bash
curl -X POST http://localhost:8080/admin/api/api-keys/b3f1.../reset-usage \
  -H "X-Admin-Password: $ADMIN_PW"
```

---

## Dùng key vừa tạo để gọi model

Key tạo ở trên dùng cho endpoint proxy `/v1/*` (khác admin API), truyền qua **một trong hai**:

- `Authorization: Bearer sk-...`
- `X-Api-Key: sk-...`

Việc kiểm tra key chỉ bật khi `requireApiKey = true`. Khi key bị disabled, hết hạn, hoặc quá giới hạn:

| Tình huống          | HTTP | Thông điệp                    |
|---------------------|------|-------------------------------|
| Sai/thiếu key       | 401  | `Invalid or missing API key`  |
| Key disabled        | 401  | `API key disabled`            |
| Key hết hạn         | 401  | `API key expired`             |
| Quá token limit     | 429  | `token limit exceeded`        |
| Quá credit limit    | 429  | `credit limit exceeded`       |

---

## Ví dụ tool tự động tạo key (bash)

```bash
#!/usr/bin/env bash
set -euo pipefail

ADMIN_PW="${ADMIN_PW:?set ADMIN_PW}"
BASE="${BASE:-http://localhost:8080}"
NAME="${1:-auto-key}"
DAYS="${2:-30}"   # số ngày sống

# Hết hạn vào 00:00 của ngày (hôm nay + DAYS + 1) — sống trọn ngày cuối
EXPIRES=$(date -d "today + $((DAYS + 1)) days 00:00:00" +%s)

resp=$(curl -sS -X POST "$BASE/admin/api/api-keys" \
  -H "X-Admin-Password: $ADMIN_PW" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"$NAME\",\"enabled\":true,\"expiresAt\":$EXPIRES}")

echo "$resp" | jq -r '.key'   # in ra key thật để lưu lại
```

Gọi: `ADMIN_PW=xxx ./create-key.sh my-bot 30`
