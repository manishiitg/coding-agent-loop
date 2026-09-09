package services

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Additional Google Workspace services a Gmail connection (in practice a
// general Google account connection — the type is still named GmailConnection
// for a minimal diff) can be authorized for, beyond Gmail send/read.
//
// Unlike Gmail, where send is the whole point and read is a narrow opt-in,
// these services have no single obvious operation: the agent drives gogcli
// directly (see virtual-tools' google_cli_tool.go), so the grant that matters
// is per-service read-vs-write, mirroring gogcli's own --readonly flag rather
// than a fixed scope list per operation.

// GoogleServiceGrant is one service a connection is authorized for.
type GoogleServiceGrant struct {
	// Service is a key into googleServiceCatalog (e.g. "drive", "sheets").
	Service string `json:"service"`
	// Write requests the mutating scope. Off by default: read-only is the
	// safer grant, and matches gogcli's own --readonly enforcement — a
	// read-only grant runs every invocation for this service with
	// --readonly, so even a misbehaving agent call cannot mutate it.
	Write bool `json:"write,omitempty"`
}

type googleServiceDef struct {
	DisplayName string
	ReadScope   string
	WriteScope  string
}

// googleServiceCatalog is the single source of truth for which services this
// connect flow offers and the OAuth scope each grant level requests. Adding a
// service gogcli already supports is one entry here — no other file needs to
// know the scope URI.
var googleServiceCatalog = map[string]googleServiceDef{
	"drive": {
		DisplayName: "Drive",
		ReadScope:   "https://www.googleapis.com/auth/drive.readonly",
		WriteScope:  "https://www.googleapis.com/auth/drive",
	},
	"sheets": {
		DisplayName: "Sheets",
		ReadScope:   "https://www.googleapis.com/auth/spreadsheets.readonly",
		WriteScope:  "https://www.googleapis.com/auth/spreadsheets",
	},
	"docs": {
		DisplayName: "Docs",
		ReadScope:   "https://www.googleapis.com/auth/documents.readonly",
		WriteScope:  "https://www.googleapis.com/auth/documents",
	},
	"slides": {
		DisplayName: "Slides",
		ReadScope:   "https://www.googleapis.com/auth/presentations.readonly",
		WriteScope:  "https://www.googleapis.com/auth/presentations",
	},
	"calendar": {
		DisplayName: "Calendar",
		ReadScope:   "https://www.googleapis.com/auth/calendar.readonly",
		WriteScope:  "https://www.googleapis.com/auth/calendar",
	},
}

// GoogleServiceCatalog lists the services the connect UI may offer, keyed by
// the identifier a GoogleServiceGrant.Service names, to display name.
func GoogleServiceCatalog() map[string]string {
	out := make(map[string]string, len(googleServiceCatalog))
	for key, def := range googleServiceCatalog {
		out[key] = def.DisplayName
	}
	return out
}

// normalizeGoogleServiceGrants drops unknown services and duplicates (last
// write wins), and sorts for a stable persisted order.
func normalizeGoogleServiceGrants(in []GoogleServiceGrant) []GoogleServiceGrant {
	byService := make(map[string]GoogleServiceGrant, len(in))
	for _, g := range in {
		service := strings.ToLower(strings.TrimSpace(g.Service))
		if _, ok := googleServiceCatalog[service]; !ok {
			continue
		}
		byService[service] = GoogleServiceGrant{Service: service, Write: g.Write}
	}
	if len(byService) == 0 {
		return nil
	}
	out := make([]GoogleServiceGrant, 0, len(byService))
	for _, g := range byService {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Service < out[j].Service })
	return out
}

// GoogleServiceScopeURI resolves one service+level to the scope URI Google
// would need to have granted, plus its display name. Exported for callers
// that need to check whether one specific grant is actually reflected in a
// connection's live scopes (see list_gmail_connections), as opposed to
// GoogleServiceScopeURIs, which builds the full list for a (re)connect
// request.
func GoogleServiceScopeURI(service string, write bool) (scope, displayName string, ok bool) {
	def, ok := googleServiceCatalog[strings.ToLower(strings.TrimSpace(service))]
	if !ok {
		return "", "", false
	}
	if write {
		return def.WriteScope, def.DisplayName, true
	}
	return def.ReadScope, def.DisplayName, true
}

// GoogleServiceScopeURIs resolves a connection's extra-service grants to the
// OAuth scopes to request alongside Gmail's own scopes. Exported for the
// gmail_oauth_routes.go call site, which builds the scope list for a
// (re)connect flow from the stored connection's grants.
func GoogleServiceScopeURIs(grants []GoogleServiceGrant) []string {
	var out []string
	for _, g := range grants {
		def, ok := googleServiceCatalog[strings.ToLower(strings.TrimSpace(g.Service))]
		if !ok {
			continue
		}
		if g.Write {
			out = append(out, def.WriteScope)
		} else {
			out = append(out, def.ReadScope)
		}
	}
	return out
}

// GoogleCLIAccess is what a caller needs to run one gogcli invocation on
// behalf of a connection: which binary, which token, and which services (and
// at what access level) that connection is actually authorized for.
type GoogleCLIAccess struct {
	GogPath string
	Token   string
	// Grants maps service -> write-allowed. A service absent from this map is
	// not authorized at all for this connection.
	Grants map[string]bool
}

// GoogleCLIAccessForConnection resolves the token and grant set for one
// connection (or the default connection, when id is empty), for the generic
// Google CLI tool (virtual-tools' google_cli_tool.go) to shell out with.
//
// Deliberately independent of GmailConfig.UseGogBackend: that flag picks
// which backend serves Gmail send/status, but Drive/Sheets/Slides/etc. have
// no gws equivalent, so this always resolves to gog.
func (g *GmailService) GoogleCLIAccessForConnection(ctx context.Context, connectionID string) (GoogleCLIAccess, error) {
	connectionID = strings.TrimSpace(connectionID)
	var conn GmailConnection
	var ok bool
	if connectionID == "" {
		conn, ok = g.DefaultConnection()
		if !ok {
			return GoogleCLIAccess{}, fmt.Errorf("no default Google account connection is configured — connect one in workflow bots settings")
		}
	} else {
		conn, ok = g.GetConnection(connectionID)
		if !ok {
			return GoogleCLIAccess{}, fmt.Errorf("Google account connection %q not found", connectionID)
		}
	}
	if !conn.Enabled {
		return GoogleCLIAccess{}, fmt.Errorf("Google account connection %q (%s) is disabled — reconnect it before use", conn.ID, conn.DisplayName)
	}
	if len(conn.Services) == 0 {
		return GoogleCLIAccess{}, fmt.Errorf("connection %q (%s) is not authorized for any service beyond Gmail — enable one in workflow bots settings", conn.ID, conn.DisplayName)
	}

	token, err := accessTokenForConnection(ctx, conn.ID, conn.ClientName)
	if err != nil {
		return GoogleCLIAccess{}, err
	}

	cfg := g.GetConfig()
	gogPath := "gog"
	if cfg != nil {
		if v := strings.TrimSpace(cfg.GogPath); v != "" {
			gogPath = v
		}
	}

	grants := make(map[string]bool, len(conn.Services))
	for _, grant := range conn.Services {
		grants[grant.Service] = grant.Write
	}
	return GoogleCLIAccess{GogPath: gogPath, Token: token, Grants: grants}, nil
}

// sortedGoogleServiceNames lists catalog keys for an error message enumerating
// valid choices.
func sortedGoogleServiceNames() []string {
	out := make([]string, 0, len(googleServiceCatalog))
	for key := range googleServiceCatalog {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// googleCLIDisallowedArgs are flags the caller must not set: identity and
// enforcement are this function's job, not the agent's. Checked as a whole
// argument or as an "=value" prefix (e.g. --access-token=...).
var googleCLIDisallowedArgs = []string{"--access-token", "--account", "-a", "--client", "--home"}

// googleCLITimeout bounds one invocation so a hung gogcli process (network
// partition, an interactive prompt it's waiting on) cannot stall the calling
// agent turn indefinitely.
const googleCLITimeout = 60 * time.Second

// googleCLIOutputLimit truncates output before it reaches the agent's
// context — a `drive files list` on a large folder or a `sheets values get`
// on a big range can otherwise return megabytes.
const googleCLIOutputLimit = 20000

// RunGoogleCLI executes one gogcli invocation on behalf of a connection.
//
// args must start with the target service (e.g. "drive", "sheets") and must
// not include --access-token/--account/--client/--home — those are added
// here, from the resolved connection's own credential, so the caller (the
// google_workspace_cli agent tool) never sees or chooses the token. When the
// connection's grant for that service is read-only, --readonly is appended
// too, so gogcli itself refuses any mutating call rather than trusting the
// caller not to attempt one.
func RunGoogleCLI(ctx context.Context, connectionID string, args []string) (string, error) {
	trimmed := make([]string, 0, len(args))
	for _, a := range args {
		if v := strings.TrimSpace(a); v != "" {
			trimmed = append(trimmed, v)
		}
	}
	if len(trimmed) == 0 {
		return "", fmt.Errorf("args must start with a service name, e.g. [\"drive\",\"files\",\"list\",\"--json\"]")
	}
	for _, a := range trimmed {
		lower := strings.ToLower(a)
		for _, bad := range googleCLIDisallowedArgs {
			if lower == bad || strings.HasPrefix(lower, bad+"=") {
				return "", fmt.Errorf("the %q flag is set automatically for the authorized connection and must not be passed", bad)
			}
		}
	}

	service := strings.ToLower(trimmed[0])
	if _, ok := googleServiceCatalog[service]; !ok {
		return "", fmt.Errorf("unknown Google service %q — the first argument must be one of: %s", service, strings.Join(sortedGoogleServiceNames(), ", "))
	}

	svc := GetGmailService()
	if svc == nil {
		return "", fmt.Errorf("Google account connections are not configured on this server")
	}
	access, err := svc.GoogleCLIAccessForConnection(ctx, connectionID)
	if err != nil {
		return "", err
	}
	writeAllowed, granted := access.Grants[service]
	if !granted {
		return "", fmt.Errorf("this connection is not authorized for %q — enable it in workflow bots settings and reconnect", service)
	}

	finalArgs := append([]string{"--home", gogHomeDir()}, trimmed...)
	finalArgs = append(finalArgs, "--access-token", access.Token)
	if !writeAllowed {
		finalArgs = append(finalArgs, "--readonly")
	}

	cmdCtx, cancel := context.WithTimeout(ctx, googleCLITimeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, access.GogPath, finalArgs...)
	out, runErr := cmd.CombinedOutput()
	text := string(out)
	if len(text) > googleCLIOutputLimit {
		text = text[:googleCLIOutputLimit] + "\n...[truncated]"
	}
	if runErr != nil {
		return text, fmt.Errorf("gog exited with an error: %w", runErr)
	}
	return text, nil
}
