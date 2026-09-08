package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"

	"github.com/gorilla/mux"
)

// Multi-account Gmail: CRUD over sending identities, under the same
// /api/human-feedback/gmail prefix the single-account endpoints already use.
//
// One connection = one isolated gws config directory = one authenticated Google
// account. These endpoints manage the registry; they never handle credentials.
// Authenticating a connection means running `gws auth login` against its config
// directory, which is why config_home is exposed: an operator needs to know
// which directory to point the login at.

// GmailConnectionResponse is the wire shape of one connection.
//
// Built by explicit projection rather than marshalling services.GmailConnection,
// so a field added to the model later cannot leak through this API by default.
// Paths are exposed; their contents never are, and no token, secret, or
// credential material appears here at all.
type GmailConnectionResponse struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email,omitempty"`
	ConfigHome  string `json:"config_home,omitempty"`
	// ClientName is the named OAuth client (see /oauth-clients) this
	// connection authorizes under. Empty only for a connection that
	// predates the named-client registry and has not yet been migrated.
	ClientName string `json:"client_name,omitempty"`
	// AllowReadAccess is whether this connection was authorized with
	// gmail.readonly in addition to gmail.send. Send-only is the default.
	AllowReadAccess bool `json:"allow_read_access"`
	// HasCredentialsFile reports whether a key file is pinned, without naming
	// it — the path is operator detail the picker does not need.
	HasCredentialsFile bool   `json:"has_credentials_file,omitempty"`
	Status             string `json:"status,omitempty"`
	Enabled            bool   `json:"enabled"`
	IsDefault          bool   `json:"is_default"`

	// Auth is live state, cached per connection so listing N accounts does not
	// spawn N subprocesses. Checking=true means "ask again shortly".
	Auth services.GmailAuthStatus `json:"auth"`

	// Ready is the bottom line for this connection: enabled, authenticated, and
	// holding a Gmail send scope.
	Ready bool `json:"ready"`

	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// GmailConnectionsResponse is the list payload.
type GmailConnectionsResponse struct {
	Connections []GmailConnectionResponse `json:"connections"`
	// DefaultConnectionID is empty when no default is set, which is a
	// configuration error the UI should surface: sends with no explicit
	// connection will fail rather than pick an account.
	DefaultConnectionID string `json:"default_connection_id,omitempty"`
}

// GmailConnectionRequest is the create/update body. Fields the server discovers
// (email, status, scopes) are deliberately not accepted from a client.
type GmailConnectionRequest struct {
	DisplayName     string `json:"display_name,omitempty"`
	ConfigHome      string `json:"config_home,omitempty"`
	CredentialsFile string `json:"credentials_file,omitempty"`
	// ClientName is required on create — see services.GmailService.CreateConnection.
	ClientName string `json:"client_name,omitempty"`
	// AllowReadAccess opts the connection into gmail.readonly on top of the
	// always-requested gmail.send. Omitted/false is send-only, the default —
	// see services.GmailConnection.AllowReadAccess.
	AllowReadAccess bool  `json:"allow_read_access,omitempty"`
	Enabled         *bool `json:"enabled,omitempty"`
}

// GmailConnectionTestRequest optionally overrides the test recipient.
type GmailConnectionTestRequest struct {
	To string `json:"to,omitempty"`
}

// GmailConnectionRoutes wires the connection registry API.
func GmailConnectionRoutes(router *mux.Router, api *StreamingAPI) {
	r := router.PathPrefix("/api/human-feedback/gmail/connections").Subrouter()
	r.HandleFunc("", listGmailConnectionsHandler(api)).Methods("GET")
	r.HandleFunc("", createGmailConnectionHandler(api)).Methods("POST", "OPTIONS")
	r.HandleFunc("/{id}", getGmailConnectionHandler(api)).Methods("GET")
	r.HandleFunc("/{id}", updateGmailConnectionHandler(api)).Methods("PATCH", "POST", "OPTIONS")
	r.HandleFunc("/{id}", deleteGmailConnectionHandler(api)).Methods("DELETE", "OPTIONS")
	r.HandleFunc("/{id}/default", setDefaultGmailConnectionHandler(api)).Methods("POST", "OPTIONS")
	r.HandleFunc("/{id}/test", testGmailConnectionSendHandler(api)).Methods("POST", "OPTIONS")
}

// projectGmailConnection is the single place a connection becomes JSON.
func projectGmailConnection(svc *services.GmailService, conn services.GmailConnection, defaultID string) GmailConnectionResponse {
	auth, resolved := svc.AuthStatusForConnection(conn.ID)
	if !resolved {
		// The registry could not answer for this connection. That is missing
		// information, not evidence of a broken account, so report it as
		// pending rather than letting a zero value render as an authoritative
		// "not authenticated" badge.
		auth = services.GmailAuthStatus{Checking: true, Detail: "checking Gmail authorization…"}
	}

	status := string(conn.Status)
	// Live auth beats the stored status, which is only ever a last-known value.
	if !auth.Checking {
		if auth.Authenticated && auth.HasGmailScope {
			status = string(services.GmailConnectionConnected)
		} else if auth.GwsInstalled {
			status = string(services.GmailConnectionNeedsReconnect)
		}
	}

	email := conn.Email
	if auth.Email != "" {
		email = auth.Email
	}

	out := GmailConnectionResponse{
		ID:                 conn.ID,
		DisplayName:        conn.DisplayName,
		Email:              email,
		ConfigHome:         conn.ConfigHome,
		ClientName:         conn.ClientName,
		AllowReadAccess:    conn.AllowReadAccess,
		HasCredentialsFile: strings.TrimSpace(conn.CredentialsFile) != "",
		Status:             status,
		Enabled:            conn.Enabled,
		IsDefault:          conn.ID == defaultID,
		Auth:               auth,
		Ready:              conn.Enabled && auth.Authenticated && auth.HasGmailScope,
	}
	if !conn.CreatedAt.IsZero() {
		out.CreatedAt = conn.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	if !conn.UpdatedAt.IsZero() {
		out.UpdatedAt = conn.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return out
}

func writeGmailConnection(w http.ResponseWriter, svc *services.GmailService, conn services.GmailConnection) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(projectGmailConnection(svc, conn, svc.GetConfig().DefaultConnectionID))
}

// gmailConnectionService resolves the service and the {id} path variable,
// writing the error response itself when either is unavailable.
func gmailConnectionService(w http.ResponseWriter, r *http.Request) (*services.GmailService, string, bool) {
	svc, err := ensureGmailService()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to initialize Gmail service: %v", err), http.StatusInternalServerError)
		return nil, "", false
	}
	return svc, strings.TrimSpace(mux.Vars(r)["id"]), true
}

func listGmailConnectionsHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, err := ensureGmailService()
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to initialize Gmail service: %v", err), http.StatusInternalServerError)
			return
		}
		// A host that was authenticated with `gws auth login` directly shows
		// up as the default account instead of "No sending accounts yet".
		if _, adopted, adoptErr := svc.AdoptHostAccount(r.Context()); adoptErr != nil {
			log.Printf("[GMAIL] host account adoption failed: %v", adoptErr)
		} else if adopted {
			log.Printf("[GMAIL] adopted the host's authenticated gws account as the default connection")
		}
		autoConfigureGmailIfAuthenticated(r.Context(), svc)
		defaultID := svc.GetConfig().DefaultConnectionID
		conns := svc.ListConnections()
		out := GmailConnectionsResponse{
			Connections:         make([]GmailConnectionResponse, 0, len(conns)),
			DefaultConnectionID: defaultID,
		}
		for _, c := range conns {
			out.Connections = append(out.Connections, projectGmailConnection(svc, c, defaultID))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}
}

func getGmailConnectionHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, id, ok := gmailConnectionService(w, r)
		if !ok {
			return
		}
		conn, found := svc.GetConnection(id)
		if !found {
			http.Error(w, fmt.Sprintf("gmail connection %q not found", id), http.StatusNotFound)
			return
		}
		writeGmailConnection(w, svc, conn)
	}
}

func createGmailConnectionHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		svc, err := ensureGmailService()
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to initialize Gmail service: %v", err), http.StatusInternalServerError)
			return
		}
		var req GmailConnectionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
			return
		}
		conn, err := svc.CreateConnection(r.Context(), services.GmailConnectionInput{
			DisplayName:     req.DisplayName,
			ConfigHome:      req.ConfigHome,
			CredentialsFile: req.CredentialsFile,
			ClientName:      req.ClientName,
			AllowReadAccess: req.AllowReadAccess,
			Enabled:         req.Enabled,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("[GMAIL] Created connection %s (%s) at %s", conn.ID, conn.DisplayName, conn.ConfigHome)
		w.WriteHeader(http.StatusCreated)
		writeGmailConnection(w, svc, conn)
	}
}

func updateGmailConnectionHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		svc, id, ok := gmailConnectionService(w, r)
		if !ok {
			return
		}
		var req GmailConnectionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
			return
		}
		conn, err := svc.UpdateConnection(r.Context(), id, services.GmailConnectionInput{
			DisplayName:     req.DisplayName,
			ConfigHome:      req.ConfigHome,
			CredentialsFile: req.CredentialsFile,
			ClientName:      req.ClientName,
			Enabled:         req.Enabled,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeGmailConnection(w, svc, conn)
	}
}

func deleteGmailConnectionHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		svc, id, ok := gmailConnectionService(w, r)
		if !ok {
			return
		}
		if err := svc.DeleteConnection(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		log.Printf("[GMAIL] Deleted connection %s", id)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"deleted": id})
	}
}

func setDefaultGmailConnectionHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		svc, id, ok := gmailConnectionService(w, r)
		if !ok {
			return
		}
		if err := svc.SetDefaultConnection(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		conn, _ := svc.GetConnection(id)
		writeGmailConnection(w, svc, conn)
	}
}

// testGmailConnectionSendHandler sends through one specific connection, so the
// user verifies the exact account they selected rather than the default.
func testGmailConnectionSendHandler(api *StreamingAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		svc, id, ok := gmailConnectionService(w, r)
		if !ok {
			return
		}
		conn, found := svc.GetConnection(id)
		if !found {
			writeGmailTest(w, false, fmt.Sprintf("gmail connection %q not found", id))
			return
		}

		to := ""
		if r.ContentLength > 0 {
			var req GmailConnectionTestRequest
			if json.NewDecoder(r.Body).Decode(&req) == nil {
				to = req.To
			}
		}

		msgID, err := svc.SendTestFromConnection(r.Context(), id, to)
		if err != nil {
			log.Printf("[GMAIL] test send via %s failed: %v", id, err)
			writeGmailTest(w, false, fmt.Sprintf("Test failed: %v", err))
			return
		}
		// Name the account in the confirmation: the whole point of a
		// per-connection test is knowing which identity actually sent.
		sender := conn.DisplayName
		if st, ok := svc.AuthStatusForConnection(id); ok && st.Email != "" {
			sender = fmt.Sprintf("%s (%s)", conn.DisplayName, st.Email)
		}
		writeGmailTest(w, true, fmt.Sprintf("Test email sent from %s (id: %s). Check the recipient inbox.", sender, msgID))
	}
}
