package workproduct

import (
	"embed"
	"fmt"
	"sync"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
)

//go:embed product.yaml prompts/system-prompt.md skills/*/SKILL.md
var productConfigFiles embed.FS

// ProductManifest is the shared product.yaml shape (pkg/agentprofiles).
type ProductManifest = agentprofiles.ProductManifest

var (
	productManifestOnce sync.Once
	productManifest     ProductManifest
	productManifestErr  error
)

// WorkManifest loads and validates product.yaml once.
func WorkManifest() (ProductManifest, error) {
	productManifestOnce.Do(func() {
		manifest, err := agentprofiles.LoadProductManifest(productConfigFiles, "product.yaml")
		if err != nil {
			productManifestErr = fmt.Errorf("Work %w", err)
			return
		}
		if manifest.Profile.ID != "work" || manifest.Profile.Scope != agentprofiles.ProfileScopeProject || manifest.UI.Surface != "work" {
			productManifestErr = fmt.Errorf("invalid Work product manifest")
			return
		}
		productManifest = manifest
	})
	return productManifest, productManifestErr
}

func renderProductPrompt() string {
	manifest := mustWorkManifest()
	prompt, err := manifest.RenderPrompt(productConfigFiles, manifest.Profile, nil)
	if err != nil {
		panic(fmt.Errorf("render Work prompt: %w", err))
	}
	return prompt
}

func mustWorkManifest() ProductManifest {
	manifest, err := WorkManifest()
	if err != nil {
		panic(err)
	}
	return manifest
}
