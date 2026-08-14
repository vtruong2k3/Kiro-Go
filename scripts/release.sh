#!/usr/bin/env bash
# release.sh — build + GitHub Release + npm publish, không cần GitHub Actions
#
# Usage:
#   GITHUB_TOKEN=ghp_xxx NPM_TOKEN=npm_xxx bash scripts/release.sh 1.2.0
#   hoặc set sẵn trong ~/.bashrc:
#     export GITHUB_TOKEN=ghp_xxx
#     export NPM_TOKEN=npm_xxx
#   rồi chạy:
#     bash scripts/release.sh 1.2.0

set -euo pipefail

REPO="vtruong2k3/Kiro-Go"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RELEASE_DIR="${ROOT}/release"

# ── 1. Đọc version ────────────────────────────────────────────────────────────
VERSION="${1:-}"
if [[ -z "${VERSION}" ]]; then
  echo "❌ Cần truyền version. Ví dụ: bash scripts/release.sh 1.2.0"
  exit 1
fi
TAG="v${VERSION}"

# ── 2. Kiểm tra token ─────────────────────────────────────────────────────────
if [[ -z "${GITHUB_TOKEN:-}" ]]; then
  echo "❌ Cần set GITHUB_TOKEN (GitHub PAT với quyền repo/Contents:write)"
  echo "   Tạo tại: https://github.com/settings/tokens/new"
  exit 1
fi
if [[ -z "${NPM_TOKEN:-}" ]]; then
  echo "❌ Cần set NPM_TOKEN"
  exit 1
fi

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🚀 Release Kiro Proxy ${TAG}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# ── 3. Bump version trong tất cả file ─────────────────────────────────────────
echo ""
echo "📝 [1/5] Bump version → ${VERSION}..."

# config/config.go
sed -i "s/const Version = \"[^\"]*\"/const Version = \"${VERSION}\"/" "${ROOT}/config/config.go"
echo "   ✅ config/config.go"

# cli/package.json
node -e "
  const fs = require('fs');
  const p = JSON.parse(fs.readFileSync('${ROOT}/cli/package.json', 'utf8'));
  p.version = '${VERSION}';
  fs.writeFileSync('${ROOT}/cli/package.json', JSON.stringify(p, null, 2) + '\n');
"
echo "   ✅ cli/package.json"

# version.json
node -e "
  const fs = require('fs');
  const v = JSON.parse(fs.readFileSync('${ROOT}/version.json', 'utf8'));
  v.version = '${VERSION}';
  fs.writeFileSync('${ROOT}/version.json', JSON.stringify(v, null, 2) + '\n');
"
echo "   ✅ version.json"

# ── 4. Build frontend SPA (nếu có thay đổi) ───────────────────────────────────
if [[ ! -f "${ROOT}/web/dist/index.html" ]]; then
  echo ""
  echo "🎨 Build frontend SPA..."
  cd "${ROOT}/web/frontend"
  pnpm install --frozen-lockfile
  pnpm build
  test -f "${ROOT}/web/dist/index.html"
  echo "   ✅ SPA built"
else
  echo ""
  echo "🎨 SPA dist đã có, bỏ qua build frontend"
fi

# ── 5. Build Go binary cho 6 platform ─────────────────────────────────────────
echo ""
echo "🔨 [2/5] Build Go binary cho 6 platform..."
mkdir -p "${RELEASE_DIR}"
cd "${ROOT}"

build_platform() {
  local GOOS=$1 GOARCH=$2 OUT=$3
  CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" \
    go build -trimpath -ldflags="-s -w" -o "${RELEASE_DIR}/${OUT}" .
  echo "   ✅ ${GOOS}/${GOARCH} → ${OUT}"
}

build_platform linux   amd64 kiro-go-linux-amd64
build_platform linux   arm64 kiro-go-linux-arm64
build_platform darwin  amd64 kiro-go-darwin-amd64
build_platform darwin  arm64 kiro-go-darwin-arm64
build_platform windows amd64 kiro-go-windows-amd64.exe
build_platform windows arm64 kiro-go-windows-arm64.exe

# ── 6. SHA256SUMS ─────────────────────────────────────────────────────────────
echo ""
echo "🔐 [3/5] Tạo SHA256SUMS..."
cd "${RELEASE_DIR}"
sha256sum kiro-go-linux-amd64 kiro-go-linux-arm64 \
          kiro-go-darwin-amd64 kiro-go-darwin-arm64 \
          kiro-go-windows-amd64.exe kiro-go-windows-arm64.exe > SHA256SUMS
echo "   ✅ SHA256SUMS"

# ── 7. GitHub Release ─────────────────────────────────────────────────────────
echo ""
echo "📤 [4/5] Tạo GitHub Release ${TAG}..."

AUTH_HEADER="Authorization: Bearer ${GITHUB_TOKEN}"
API="https://api.github.com"
UPLOAD_API="https://uploads.github.com"

# Push tag
cd "${ROOT}"
git tag "${TAG}" 2>/dev/null || echo "   (tag đã tồn tại, tiếp tục)"
git push origin "${TAG}" 2>&1 | sed 's/^/   /'

# Tạo release
RELEASE_JSON=$(curl -sf -X POST \
  -H "${AUTH_HEADER}" -H "Content-Type: application/json" \
  "${API}/repos/${REPO}/releases" \
  -d "{
    \"tag_name\": \"${TAG}\",
    \"name\": \"Kiro Proxy ${TAG}\",
    \"body\": \"See [CHANGELOG](https://github.com/${REPO}/blob/main/version.json) for details.\",
    \"draft\": false,
    \"prerelease\": false
  }" 2>&1) || true

RELEASE_ID=$(echo "${RELEASE_JSON}" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])" 2>/dev/null || true)

# Nếu release đã tồn tại, lấy ID
if [[ -z "${RELEASE_ID}" ]]; then
  echo "   Release đã tồn tại, lấy ID..."
  RELEASE_ID=$(curl -sf \
    -H "${AUTH_HEADER}" \
    "${API}/repos/${REPO}/releases/tags/${TAG}" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
fi
echo "   ✅ Release ID: ${RELEASE_ID}"

# Upload tất cả file
echo "   Uploading assets..."
for FILE in kiro-go-linux-amd64 kiro-go-linux-arm64 kiro-go-darwin-amd64 kiro-go-darwin-arm64 kiro-go-windows-amd64.exe kiro-go-windows-arm64.exe SHA256SUMS; do
  echo -n "      ${FILE}... "
  HTTP=$(curl -sf -o /dev/null -w "%{http_code}" \
    -X POST \
    -H "${AUTH_HEADER}" -H "Content-Type: application/octet-stream" \
    "${UPLOAD_API}/repos/${REPO}/releases/${RELEASE_ID}/assets?name=${FILE}" \
    --data-binary "@${RELEASE_DIR}/${FILE}")
  [[ "${HTTP}" == "201" ]] && echo "✅" || echo "❌ HTTP ${HTTP}"
done

# ── 8. Publish npm ─────────────────────────────────────────────────────────────
echo ""
echo "📦 [5/5] Publish proxy-kiro@${VERSION} lên npm..."
cd "${ROOT}/cli"
npm publish \
  --registry=https://registry.npmjs.org \
  "--//registry.npmjs.org/:_authToken=${NPM_TOKEN}" \
  2>&1 | sed 's/^/   /'

# ── 9. Done ───────────────────────────────────────────────────────────────────
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🎉 Release ${TAG} hoàn tất!"
echo "   GitHub : https://github.com/${REPO}/releases/tag/${TAG}"
echo "   npm    : https://www.npmjs.com/package/proxy-kiro/v/${VERSION}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
