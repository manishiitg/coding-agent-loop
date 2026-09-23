package server

import (
	"log"
	"net/http"
	"net/http/httptest"
	"time"
)

// warmLLMConfigCaches runs the model-picker endpoints once at startup so the
// coding-CLI version/auth probes they share are cached before the first page
// load. Without it, the first load after every restart/deploy waited 1-5s on
// /api/llm-config/{defaults,providers} and /api/published-llms.
func (api *StreamingAPI) warmLLMConfigCaches() {
	started := time.Now()
	for _, warm := range []struct {
		path    string
		handler http.HandlerFunc
	}{
		{"/api/llm-config/providers", api.handleGetProviderManifest},
		{"/api/llm-config/defaults", api.handleGetLLMDefaults},
		{"/api/published-llms", api.handleLoadPublishedLLMs},
	} {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					log.Printf("[LLM_CONFIG_WARM] %s panicked: %v", warm.path, recovered)
				}
			}()
			warm.handler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, warm.path, nil))
		}()
	}
	log.Printf("[LLM_CONFIG_WARM] model-picker caches warmed in %s", time.Since(started).Round(time.Millisecond))
}
