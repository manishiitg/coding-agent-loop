package server

import (
	"reflect"
	"testing"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpclient"
)

// Server release catalogs used to omit common integrations present locally.
// Exercise the real config decoder and keep the expanded entries in sync.
func TestConsumerCatalogsStayInSync(t *testing.T) {
	base, err := mcpclient.LoadConfig("../../configs/mcp_servers_clean.json", loggerv2.NewNoop())
	if err != nil {
		t.Fatal(err)
	}
	added := []string{"Todoist", "Asana", "ClickUp", "Atlassian", "Dropbox", "Miro", "Figma"}
	for _, path := range []string{"../../../deploy/aws-ec2/server/mcp_servers_video_studio.json", "../../../deploy/cf/mcp-servers-cf.json"} {
		catalog, err := mcpclient.LoadConfig(path, loggerv2.NewNoop())
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range append([]string{"Notion", "Canva", "Airtable"}, added...) {
			entry, ok := catalog.MCPServers[name]
			if !ok || entry.URL == "" || entry.OAuth == nil || entry.OAuth.AuthURL == "" || entry.OAuth.TokenURL == "" {
				t.Errorf("%s: %s missing endpoint/auth metadata", path, name)
			}
		}
		for _, name := range added {
			if !reflect.DeepEqual(catalog.MCPServers[name], base.MCPServers[name]) {
				t.Errorf("%s: %s differs from shared catalog", path, name)
			}
		}
	}
	if base.MCPServers["Asana"].OAuth.RegistrationEndpoint != "" {
		t.Fatal("Asana v2 must not advertise an invented DCR endpoint")
	}
}
