#!/usr/bin/env bash
# create_release.sh — tạo GitHub Release v1.1.8 và upload tất cả binary
# Usage: GITHUB_TOKEN=ghp_xxx bash scripts/create_release.sh

set -euo pipefail

REPO="vtruong2k3/Kiro-Go"
VERSION="1.1.8"
TAG="v${VERSION}"
RELEASE_DIR="$(cd "$(dirname "$0")/../release" && pwd)"

if [[ -z "${GITHUB_TOKEN:-}" ]]; then
  echo "❌ Cần set GITHUB_TOKEN. Ví dụ:"
  echo "   GITHUB_TOKEN=ghp_xxx bash scripts/create_release.sh"
  exit 1
fi

AUTH="Authorization: Bearer ${GITHUB_TOKEN}"
API="https://api.github.com"

echo "🔍 Kiểm tra tag ${TAG} đã tồn tại chưa..."
if git tag -l "${TAG}" | grep -q "${TAG}"; then
  echo "   Tag ${TAG} đã tồn tại local"
else
  echo "   Tạo tag ${TAG}..."
  git tag "${TAG}"
fi

echo ""
echo "📤 Push tag ${TAG} lên GitHub..."
git push origin "${TAG}" 2>&1 || echo "   (tag đã có trên remote, bỏ qua)"

echo ""
echo "🚀 Tạo GitHub Release ${TAG}..."
RELEASE_RESPONSE=$(curl -sf -X POST \
  -H "${AUTH}" \
  -H "Content-Type: application/json" \
  "${API}/repos/${REPO}/releases" \
  -d "{
    \"tag_name\": \"${TAG}\",
    \"name\": \"Kiro Proxy ${TAG}\",
    \"body\": \"## What's new in ${TAG}\n\n- Auto update-check: notifies users when a newer npm version is available\n- CLI now shows a notice box when running an outdated version\",
    \"draft\": false,
    \"prerelease\": false
  }" 2>&1)

RELEASE_ID=$(echo "${RELEASE_RESPONSE}" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['id'])" 2>/dev/null || true)

if [[ -z "${RELEASE_ID}" ]]; then
  # Release có thể đã tồn tại, lấy ID của release hiện có
  echo "   Lấy release ID từ tag hiện có..."
  RELEASE_RESPONSE=$(curl -sf \
    -H "${AUTH}" \
    "${API}/repos/${REPO}/releases/tags/${TAG}")
  RELEASE_ID=$(echo "${RELEASE_RESPONSE}" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
fi

echo "   Release ID: ${RELEASE_ID}"
UPLOAD_URL="https://uploads.github.com/repos/${REPO}/releases/${RELEASE_ID}/assets"

echo ""
echo "📦 Upload binaries..."
cd "${RELEASE_DIR}"

for FILE in kiro-go-linux-amd64 kiro-go-linux-arm64 kiro-go-darwin-amd64 kiro-go-darwin-arm64 kiro-go-windows-amd64.exe kiro-go-windows-arm64.exe SHA256SUMS; do
  if [[ ! -f "${FILE}" ]]; then
    echo "   ⚠️  Không tìm thấy ${FILE}, bỏ qua"
    continue
  fi
  echo -n "   Uploading ${FILE}... "
  HTTP_CODE=$(curl -sf -o /dev/null -w "%{http_code}" \
    -X POST \
    -H "${AUTH}" \
    -H "Content-Type: application/octet-stream" \
    "${UPLOAD_URL}?name=${FILE}" \
    --data-binary "@${FILE}" 2>&1)
  if [[ "${HTTP_CODE}" == "201" ]]; then
    echo "✅"
  else
    echo "❌ HTTP ${HTTP_CODE}"
  fi
done

echo ""
echo "🎉 Xong! GitHub Release ${TAG}: https://github.com/${REPO}/releases/tag/${TAG}"
