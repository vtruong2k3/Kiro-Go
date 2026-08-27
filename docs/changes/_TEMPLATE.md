---
status: draft
last_verified: YYYY-MM-DD
owner: <tên>
related_files:
  - <đường dẫn file code đã sửa>
related_tests:
  - <đường dẫn file test>
---

# YYYY-MM-DD — <tên tính năng / bug>

> Copy file này thành `docs/changes/YYYY-MM-DD-<tên>.md`. Giữ `status: draft`; người dùng duyệt xong mới đổi `current`. Không ghi secret thật — chỉ placeholder.

## Triệu chứng

<Người dùng / hệ thống thấy gì? Thông báo lỗi, hành vi sai.>

## Cách tái hiện

1. <bước 1>
2. <bước 2>

## Nguyên nhân gốc

<Tại sao lỗi xảy ra / tại sao cần thay đổi. Trích dẫn `file:dòng` nếu liên quan.>

## Giải pháp

<Đã sửa/thêm gì, cách tiếp cận, và tại sao chọn cách này thay vì cách khác.>

## File đã thay đổi

- `<file>` — <mô tả ngắn thay đổi>

## API / luồng dữ liệu bị ảnh hưởng

<Endpoint, request/response, schema DB, hoặc luồng dữ liệu nào thay đổi. Nếu không có: "Không".>

## Test

<Test nào được thêm/sửa, cách chạy.>

## Vấn đề còn lại

<Rủi ro, edge case chưa xử lý, việc tiếp theo. Nếu không có: "Không".>

## Xác minh

- File đã kiểm tra: <danh sách>
- Lệnh đã chạy: vd `go test ./auth ./proxy`
- Kết quả: <pass/fail — không dán output chứa secret>
- Thời gian: YYYY-MM-DD HH:MM