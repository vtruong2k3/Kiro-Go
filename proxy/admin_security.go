package proxy

import (
	"encoding/json"
	"kiro-go/config"
	"net/http"
	"strings"
)

func (h *Handler) apiListBlockedIPs(w http.ResponseWriter, r *http.Request) {
	list := config.ListBlockedIPs()
	if list == nil {
		list = []config.BlockedIPEntry{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"blockedIPs": list})
}

func (h *Handler) apiBlockIP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP     string `json:"ip"`
		Reason string `json:"reason,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}
	if err := config.BlockIP(req.IP, req.Reason); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (h *Handler) apiUnblockIP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}
	if err := config.UnblockIP(req.IP); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (h *Handler) apiGetSecuritySettings(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"trustProxyHeaders": config.GetTrustProxyHeaders(),
	})
}

func (h *Handler) apiUpdateSecuritySettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TrustProxyHeaders *bool `json:"trustProxyHeaders,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}
	if req.TrustProxyHeaders != nil {
		if err := config.SetTrustProxyHeaders(*req.TrustProxyHeaders); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
	}
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (h *Handler) apiGetApiKeyIPs(w http.ResponseWriter, r *http.Request, id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing id"})
		return
	}
	if config.GetApiKeyEntry(id) == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "API key not found"})
		return
	}
	ips := []keyIPStat{}
	var rpm int64
	if h != nil && h.ipTrack != nil {
		ips = h.ipTrack.snapshot(id)
		rpm = h.ipTrack.keyRPMValue(id)
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ips":         ips,
		"uniqueCount": len(ips),
		"rpm":         rpm,
	})
}
