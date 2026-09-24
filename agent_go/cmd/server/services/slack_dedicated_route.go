package services

import (
	"context"
	"strings"
	"sync/atomic"
)

// DedicatedSlackRouteFunc resolves the destination a scoped Slack app
// serves. A connection scoped to a workflow or crew project is that
// destination's own bot: it answers for it in any channel it is invited to,
// and channel routes never apply to it. Unscoped (platform) connections are
// shared bots and keep channel routing.
//
// dedicated reports whether connectionID names a scoped connection. A
// dedicated connection whose destination no longer resolves returns a
// revoked route, never nil: falling through to the channel route would
// answer as another workflow's bot. connectionID "" is the root listener
// (the default connection).
type DedicatedSlackRouteFunc func(ctx context.Context, connectionID string) (route *ChannelRoute, dedicated bool)

var dedicatedSlackRoute atomic.Pointer[DedicatedSlackRouteFunc]

// SetDedicatedSlackRouteFunc installs the server-backed resolver. The
// server owns manifests and product ownership, which services cannot read.
func SetDedicatedSlackRouteFunc(fn DedicatedSlackRouteFunc) {
	if fn == nil {
		dedicatedSlackRoute.Store(nil)
		return
	}
	dedicatedSlackRoute.Store(&fn)
}

// DedicatedSlackRoute returns the scoped app's own route, or dedicated=false
// for shared apps (and when no resolver is installed).
func DedicatedSlackRoute(ctx context.Context, connectionID string) (*ChannelRoute, bool) {
	fn := dedicatedSlackRoute.Load()
	if fn == nil {
		return nil, false
	}
	route, dedicated := (*fn)(ctx, strings.TrimSpace(connectionID))
	if !dedicated {
		return nil, false
	}
	if route == nil {
		return RevokedSlackRoute(), true
	}
	return route, true
}

// RevokedSlackRoute is the sentinel the bot manager refuses loudly on
// mention ("no longer configured") instead of falling back to generic chat.
func RevokedSlackRoute() *ChannelRoute {
	return &ChannelRoute{BotGrant: revokedBotGrant}
}

const revokedBotGrant = "__revoked__"

// IsRevokedSlackRoute reports the RevokedSlackRoute sentinel.
func IsRevokedSlackRoute(route ChannelRoute) bool {
	return route.BotGrant == revokedBotGrant
}

// RouteWorkspaceUserID exposes the product-route owner rule (explicit
// owner, else the "_users/<id>/" workspace prefix, else fallback).
func RouteWorkspaceUserID(route ChannelRoute, fallback string) string {
	return routeWorkspaceUserID(route, fallback)
}

// ResolveSlackRoute is the single inbound routing rule: a dedicated app
// serves its own destination; a shared app follows the channel route.
func ResolveSlackRoute(ctx context.Context, connectionID string, channelRoute func() *ChannelRoute) *ChannelRoute {
	if route, dedicated := DedicatedSlackRoute(ctx, connectionID); dedicated {
		return route
	}
	if channelRoute == nil {
		return nil
	}
	return channelRoute()
}
