package proxy

// static.go serves the admin web assets under /admin/ with gzip compression and
// cache headers. It replaces the bare http.ServeFile calls so text assets are
// compressed on the wire and the browser can cache immutable-ish files.
//
// Design notes:
//   - Compression is done by gzipping the file into an in-memory buffer and
//     handing that to http.ServeContent via a bytes.Reader. This keeps the
//     Content-Length correct (the gzipped size) and preserves ServeContent's
//     Last-Modified / If-Modified-Since (304) handling for free — which a
//     naive gzip.Writer wrapper around ServeFile would break.
//   - Only compressible text types are gzipped; woff2/ico/png are already
//     compressed and pass through untouched.
//   - Cache-Control is short and paired with the ?v=<timestamp> cache-busting
//     the HTML already appends, so a redeploy still busts caches.

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
)

// staticContentTypes maps file extensions to their Content-Type. Extensions not
// listed fall back to Go's sniffing inside http.ServeContent.
var staticContentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".mjs":  "text/javascript; charset=utf-8",
	".json": "application/json; charset=utf-8",
	".svg":  "image/svg+xml",
	".ico":  "image/x-icon",
	".png":  "image/png",
	".woff2": "font/woff2",
	".woff": "font/woff",
}

// staticCompressible is the set of extensions worth gzipping. Binary assets
// (woff2/ico/png) are already compressed and are intentionally excluded.
var staticCompressible = map[string]bool{
	".html": true,
	".css":  true,
	".js":   true,
	".mjs":  true,
	".json": true,
	".svg":  true,
}

// staticCacheControl returns the Cache-Control value for a given extension.
// HTML is revalidated every load (it is the cache-bust entry point); other
// assets get a short max-age since the HTML appends ?v=<timestamp> to them.
func staticCacheControl(ext string) string {
	switch ext {
	case ".html":
		return "no-cache"
	case ".woff2", ".woff", ".ico", ".png":
		return "public, max-age=604800" // 7 days — fonts/icons rarely change
	default:
		return "public, max-age=3600" // 1 hour — busted by ?v= on redeploy
	}
}

func (h *Handler) serveAdminPage(w http.ResponseWriter, r *http.Request) {
	h.serveStatic(w, r, "web/index.html")
}

func (h *Handler) serveStaticFile(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/admin/")
	// Guard against path traversal: clean the relative path and reject anything
	// that escapes the web/ root. path.Clean collapses ".." segments; a leading
	// ".." after cleaning means the request tried to escape.
	clean := path.Clean("/" + rel) // always rooted, strips ../
	fp := "web" + clean            // clean starts with "/"
	h.serveStatic(w, r, fp)
}

// serveStatic writes the file at fp with the right Content-Type, Cache-Control,
// and gzip when the client accepts it and the type is compressible.
func (h *Handler) serveStatic(w http.ResponseWriter, r *http.Request, fp string) {
	f, err := os.Open(fp)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		http.NotFound(w, r)
		return
	}

	ext := strings.ToLower(path.Ext(fp))
	if ct := staticContentTypes[ext]; ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", staticCacheControl(ext))

	if staticCompressible[ext] && clientAcceptsGzip(r) {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		if _, err := io.Copy(gz, f); err != nil {
			http.Error(w, "read error", http.StatusInternalServerError)
			return
		}
		if err := gz.Close(); err != nil {
			http.Error(w, "compress error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		// ServeContent sets Content-Length from the reader and handles 304 via modtime.
		http.ServeContent(w, r, fp, stat.ModTime(), bytes.NewReader(buf.Bytes()))
		return
	}

	http.ServeContent(w, r, fp, stat.ModTime(), f)
}

// clientAcceptsGzip reports whether the request's Accept-Encoding allows gzip.
func clientAcceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		if strings.EqualFold(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]), "gzip") {
			return true
		}
	}
	return false
}
