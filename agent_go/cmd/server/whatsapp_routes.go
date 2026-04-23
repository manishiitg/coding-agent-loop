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
	waRouter.HandleFunc("/session", whatsappUnpairHandler(svc)).Methods("DELETE", "OPTIONS")
	waRouter.HandleFunc("/routing", whatsappGetRoutingHandler(svc)).Methods("GET")
	waRouter.HandleFunc("/routing", whatsappPutRoutingHandler(svc)).Methods("PUT", "OPTIONS")
	log.Printf("[WHATSAPP] Registered routes: GET /api/whatsapp/pair, GET /api/whatsapp/status, DELETE /api/whatsapp/session, GET|PUT /api/whatsapp/routing")
}

// whatsappPairHandler serves the current pairing QR code as a PNG image.
// Query params:
//
//	size — pixel dimension (default 384, clamped to [128, 1024])
//
// Auth behaviour: requesting the QR claims ownership of the pairing for the
// authenticated user. Every incoming WhatsApp message will then route to
// that user's per-user chat history / memory. If the pairing is already
// bound to a different user, we return 409 so the UI can prompt "unpair
// first". Unauthenticated calls are allowed (for dev / local), but in that
// case no owner binding is created and incoming messages get rejected by
// the bot manager until someone claims ownership via an authed request.
//
// Returns 404 when there's no active QR (paired already, or StartListening
// hasn't been called yet).
func whatsappPairHandler(svc *slackservice.WhatsAppService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if user := GetUserFromContext(r.Context()); user != nil && user.UserID != "" {
			if err := svc.ClaimOwnership(user.UserID, user.Email, user.Username); err != nil {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
		}
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

// whatsappUnpairHandler drops the current pairing: disconnects the client,
// deletes the session DB, and restarts the service so a fresh QR is
// available immediately from GET /api/whatsapp/pair. Idempotent — calling
// twice in a row is safe.
func whatsappUnpairHandler(svc *slackservice.WhatsAppService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := svc.Unpair(r.Context()); err != nil {
			http.Error(w, "unpair failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
}

// whatsappGetRoutingHandler returns the current slug → workflow mapping
// used for in-message @<slug> routing. Response shape:
//
//	{"routing": {"<slug>": {"workflow_id":"…", "workspace_path":"…", "workshop_mode":"…"}, ...}}
func whatsappGetRoutingHandler(svc *slackservice.WhatsAppService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"routing": svc.GetRouting(),
		})
	}
}

// whatsappPutRoutingHandler replaces the entire routing map. Clients should
// PUT the full desired state; partial edits are handled client-side. Request
// body matches the GET response shape. Returns 400 on invalid slug names.
func whatsappPutRoutingHandler(svc *slackservice.WhatsAppService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Routing slackservice.WhatsAppRouting `json:"routing"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := svc.SetRouting(body.Routing); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"routing": svc.GetRouting(),
		})
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
		// Owner binding — who owns this pairing and gets the incoming chats.
		// Nil means the pairing is unclaimed (rare edge case where someone
		// paired without hitting the authed pair endpoint). Surfaced so the
		// UI can show "Paired by <email>" and warn when the current user is
		// viewing someone else's pairing.
		if owner := svc.GetOwner(); owner != nil {
			resp["owner_user_id"] = owner.UserID
			resp["owner_email"] = owner.Email
			resp["owner_username"] = owner.Username
			resp["owner_paired_at"] = owner.PairedAt.UTC().Format(time.RFC3339)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}
