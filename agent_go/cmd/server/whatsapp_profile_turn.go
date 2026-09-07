package server

import (
	"context"
	"fmt"
	"log"
	"path"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
)

// A product profile can be the default destination of an account's WhatsApp
// pairing: a message in the paired chat that names no @<slug> workflow route
// runs as a turn in that profile's own conversation — the one the product's
// app shows — with the same prompt, sandbox and runtime binding the app's own
// turns get. The product opts in through product.yaml
// (runtime.capabilities.whatsapp); the connector, pairing and routing stay
// the platform's.

// profileTakesWhatsApp reports whether the profile declared the capability.
func profileTakesWhatsApp(profile agentprofiles.Profile) bool {
	requirement := strings.TrimSpace(string(profile.Runtime.Capabilities.WhatsApp))
	return requirement != "" && requirement != string(agentprofiles.CapabilityDisabled)
}

// whatsappUploadFolderFor keeps a product's attachment folder inside the
// profile's own workspace (its fixed root, or the projects root of a keyed
// profile), where the profile's folder guard lets it read. An empty request
// means the platform's per-user chat uploads.
func whatsappUploadFolderFor(profile agentprofiles.Profile, requested string) (string, error) {
	requested = strings.Trim(strings.TrimSpace(requested), "/")
	if requested == "" {
		return "", nil
	}
	root := strings.Trim(strings.TrimSpace(profile.Runtime.Workspace.Root), "/")
	if root == "" {
		root = strings.Trim(strings.TrimSpace(profile.Runtime.Workspace.ProjectsRoot), "/")
	}
	if root == "" {
		return "", fmt.Errorf("%s has no workspace root to keep WhatsApp attachments in", profile.Name)
	}
	clean := path.Clean(requested)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("upload folder %q must be a folder inside %s", requested, root)
	}
	if clean == root || strings.HasPrefix(clean, root+"/") {
		return clean, nil
	}
	return path.Join(root, clean), nil
}

// whatsappDefaultProfileResolver is the WhatsAppDefaultProfileResolver for
// this server: the profile must exist for the user and declare the whatsapp
// capability.
func (api *StreamingAPI) whatsappDefaultProfileResolver(ctx context.Context, userID, profileID, uploadFolder string) (string, error) {
	if api.agentProfiles == nil {
		return "", fmt.Errorf("agent profiles are unavailable")
	}
	profile, err := api.agentProfiles.Resolve(strings.TrimSpace(profileID), 0, userID)
	if err != nil {
		return "", fmt.Errorf("agent profile %q not found", profileID)
	}
	if !profileTakesWhatsApp(profile) {
		return "", fmt.Errorf("%s does not take WhatsApp messages (product.yaml runtime.capabilities.whatsapp)", profile.Name)
	}
	return whatsappUploadFolderFor(profile, uploadFolder)
}

// whatsappEngineFor picks the engine a WhatsApp turn runs on: the one the
// conversation is already bound to, else the profile's default option, so
// the first turn from WhatsApp binds the conversation exactly as the app's
// first turn would.
func whatsappEngineFor(profile agentprofiles.Profile, conversation ProductConversationRecord) (agentprofiles.ProviderOption, bool) {
	options := profile.Runtime.ProviderOptions
	if bound := strings.TrimSpace(conversation.Provider); bound != "" {
		for _, option := range options {
			if strings.EqualFold(strings.TrimSpace(option.Provider), bound) {
				return option, true
			}
		}
	}
	for _, option := range options {
		if option.Default {
			return option, true
		}
	}
	if len(options) > 0 {
		return options[0], true
	}
	return agentprofiles.ProviderOption{}, false
}

// whatsappConversationBinding picks the conversation a WhatsApp turn runs
// in. The account's primary phone talks in a singleton profile's main
// conversation — the one its app shows. An extra phone (another parent's)
// gets that profile's own separate conversation, keyed by the device, so one
// parent's replies never appear on the other's phone. A keyed profile's
// conversation (an activity) is the same from every phone.
func whatsappConversationBinding(ctx context.Context, userID string, profile agentprofiles.Profile, conversationKey, deviceSlot string) (productConversationBinding, error) {
	deviceSlot = strings.TrimSpace(deviceSlot)
	if deviceSlot != "" && strings.EqualFold(strings.TrimSpace(profile.Runtime.Conversation.Mode), agentprofiles.ConversationModeSingleton) {
		return resolveIsolatedProductBinding(ctx, userID, profile, "whatsapp-"+deviceSlot, profile.Name+" (WhatsApp "+deviceSlot+")")
	}
	return resolveProductConversationBinding(ctx, userID, profile, conversationKey)
}

func whatsappWorkspaceUserID(userID string) string {
	if !IsMultiUserMode() {
		return GetDefaultUserID()
	}
	return userID
}

// whatsappProfileRouter is the connector's ProfileRouteResolver: an @token
// that is not a workflow slug is offered to the pairing's default product,
// whose registered router may map it to one of the product's own profiles.
func (api *StreamingAPI) whatsappProfileRouter(ctx context.Context, userID, token string) (*services.ProfileRoute, error) {
	if api.whatsappManager == nil || api.agentProfiles == nil {
		return nil, nil
	}
	svc, err := api.whatsappManager.ServiceForUser(ctx, userID, "", "")
	if err != nil {
		return nil, nil
	}
	defaultProfileID, _ := svc.DefaultProfile()
	if defaultProfileID == "" {
		return nil, nil
	}
	workspaceUserID := whatsappWorkspaceUserID(userID)
	route, ok, err := api.agentProfiles.ResolveChannelRoute(ctx, defaultProfileID, workspaceUserID, token)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	target, err := api.agentProfiles.Resolve(strings.TrimSpace(route.ProfileID), 0, workspaceUserID)
	if err != nil {
		return nil, fmt.Errorf("@%s points at a chat that does not exist", token)
	}
	uploadFolder, err := whatsappUploadFolderFor(target, route.UploadFolder)
	if err != nil {
		return nil, err
	}
	return &services.ProfileRoute{
		ProfileID:       target.ID,
		ConversationKey: strings.TrimSpace(route.ConversationKey),
		UploadFolder:    uploadFolder,
		Label:           strings.TrimSpace(route.Label),
	}, nil
}

// botProfileTurn is the bot manager's ProfileTurnFunc. handled is false when
// the pairing has no default profile; an error means it has one that cannot
// take the message, which the bot manager reports back in the chat.
func (api *StreamingAPI) botProfileTurn(ctx context.Context, userID string, msg services.BotIncomingMessage, threadID services.ThreadID) (map[string]interface{}, string, bool, error) {
	if msg.Platform != "whatsapp" || api.whatsappManager == nil || api.agentProfiles == nil {
		return nil, "", false, nil
	}
	svc, err := api.whatsappManager.ServiceForUser(ctx, userID, "", "")
	if err != nil {
		log.Printf("[WHATSAPP] default profile lookup for user=%s: %v", userID, err)
		return nil, "", false, nil
	}
	profileID, _ := svc.DefaultProfile()
	if profileID == "" {
		return nil, "", false, nil
	}
	// The pairing's owner is the request principal; durable product data
	// belongs to the configured default owner on a single-user deployment,
	// exactly as the app's own profile chat resolves it.
	workspaceUserID := whatsappWorkspaceUserID(userID)
	conversationKey := ""
	if msg.PresetProfile != nil {
		// An @token the default product resolved to one of its own profiles
		// (a keyed one, e.g. a tutor's per-activity conversation).
		profileID = strings.TrimSpace(msg.PresetProfile.ProfileID)
		conversationKey = strings.TrimSpace(msg.PresetProfile.ConversationKey)
	}
	profile, err := api.agentProfiles.Resolve(profileID, 0, workspaceUserID)
	if err != nil {
		return nil, "", false, fmt.Errorf("the chat %q is gone — unpair and pair again from the product", profileID)
	}
	if msg.PresetProfile == nil && !profileTakesWhatsApp(profile) {
		return nil, "", false, fmt.Errorf("%s no longer takes WhatsApp messages", profile.Name)
	}
	productInteractions.Note(ctx, workspaceUserID, profile.Product)
	binding, err := whatsappConversationBinding(ctx, workspaceUserID, profile, conversationKey, msg.DeviceSlot)
	if err != nil {
		return nil, "", false, fmt.Errorf("resolve %s conversation: %w", profile.Name, err)
	}
	conversation, err := defaultProductConversationRegistryStore().resolveOrCreate(ctx, workspaceUserID, profile, binding, "")
	if err != nil {
		return nil, "", false, fmt.Errorf("open %s conversation: %w", profile.Name, err)
	}
	input := AgentProfileChatRequest{Message: msg.Text}
	if option, ok := whatsappEngineFor(profile, conversation); ok {
		input.Engine = option.ID
		if strings.TrimSpace(conversation.Provider) != "" {
			input.ModelID = conversation.ModelID
			input.ReasoningEffort = conversation.ReasoningEffort
		}
	}
	query, err := queryRequestForAgentProfileChat(profile, input, conversation)
	if err != nil {
		return nil, "", false, err
	}
	if strings.TrimSpace(query.Provider) != "" {
		bound, restartNeeded, err := defaultProductConversationRegistryStore().bindRuntime(ctx, workspaceUserID, profile, conversation.ConversationKey, query.Provider, query.ModelID, query.ReasoningEffort)
		if err != nil {
			return nil, "", false, err
		}
		if !strings.EqualFold(bound, query.Provider) {
			return nil, "", false, fmt.Errorf("this chat runs on %s", providerOptionLabelForProvider(profile.Runtime.ProviderOptions, bound))
		}
		if restartNeeded {
			closeAllCodingCLIInteractiveSessionsForOwner(conversation.SessionID, "whatsapp turn: model or reasoning effort changed")
		}
	}
	reqMap, err := queryRequestToMap(query)
	if err != nil {
		return nil, "", false, err
	}
	// The channel prompt (WhatsApp's markup subset) applies to this turn only;
	// the app's own turns in the same conversation send no bot_platform.
	reqMap["bot_platform"] = "whatsapp"
	reqMap["triggered_by"] = "bot:whatsapp"
	if threadID.ChannelID != "" {
		reqMap["bot_channel_id"] = threadID.ChannelID
	}
	return reqMap, conversation.SessionID, true, nil
}
