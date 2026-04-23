package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	slackservice "mcp-agent-builder-go/agent_go/cmd/server/services"

	"github.com/gorilla/mux"
)

// WhatsAppRoutes wires up the HTTP endpoints for pairing and status of the
// WhatsApp bot connector. Both routes are idempotent GETs; the pair route
// serves a PNG until the user scans it and the pairing completes, at which
// point the connector transitions to "paired" and the route returns 404
// (nothing left to pair).
func WhatsAppRoutes(router *mux.Router, svc *slackservice.WhatsAppService) {
	if svc == nil {
		return
	}
	waRouter := router.PathPrefix("/api/whatsapp").Subrouter()
	waRouter.HandleFunc("/pair", whatsappPairHandler(svc)).Methods("GET")
	waRouter.HandleFunc("/status", whatsappStatusHandler(svc)).Methods("GET")
	log.Printf("[WHATSAPP] Registered routes: GET /api/whatsapp/pair, GET /api/whatsapp/status")
}

// whatsappPairHandler serves the current pairing QR code as a PNG image.
// Query params:
//
//	size — pixel dimension (default 384, clamped to [128, 1024])
//
// Returns 404 when there's no active QR (paired already, or StartListening
// hasn't been called yet).
func whatsappPairHandler(svc *slackservice.WhatsAppService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		size := 384
		if s := r.URL.Query().Get("size"); s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				if n < 128 {
					n = 128
				} else if n > 1024 {
					n = 1024
				}
				size = n
			}
		}
		png, err := svc.GetQRImagePNG(size)
		if err != nil {
			http.Error(w, "qr encode failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if png == nil {
			http.Error(w, "no pairing QR available — already paired, or service not started", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(png)
	}
}

// whatsappStatusHandler reports connector lifecycle state as JSON:
//
//	enabled   — StartListening has been called
//	paired    — a WhatsApp device identity is stored (scan was completed)
//	connected — whatsmeow is live on the WS right now
//	own_jid   — paired account's JID ("" when unpaired)
//	qr_expires_at — RFC3339 timestamp when the current QR expires (omitted
//	                when no QR is active)
func whatsappStatusHandler(svc *slackservice.WhatsAppService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"enabled":   svc.IsEnabled(),
			"paired":    svc.IsPaired(),
			"connected": svc.IsConnected(),
			"own_jid":   svc.OwnJID().String(),
		}
		if code, expires := svc.GetQR(); code != "" && !expires.IsZero() {
			resp["qr_available"] = true
			resp["qr_expires_at"] = expires.UTC().Format(time.RFC3339)
		} else {
			resp["qr_available"] = false
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}
