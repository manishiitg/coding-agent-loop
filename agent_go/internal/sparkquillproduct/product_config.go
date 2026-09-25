// Package sparkquillproduct is SparkQuill as a platform product: one
// product.yaml with a parent profile and a child profile, the family's
// skills, and the runtime that turns a family's saved state into the
// prompt variables both profiles need.
//
// SparkQuill ran as its own server (cmd/family-server) until September 2026;
// that binary is gone and this package is the product. A pre-platform
// install's ~/.sunlit-learning is copied in by migrate.go.
package sparkquillproduct

import (
	"embed"
	"fmt"
	"sync"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
)

// ProductName is the product tag every SparkQuill profile carries.
const ProductName = "sparkquill"

//go:embed product.yaml prompts/*.md
var productConfigFiles embed.FS

// SkillFiles holds the family's SKILL.md bundles, embedded once here and read by both the platform and
// the standalone family server.
//
//go:embed skills
var SkillFiles embed.FS

// ProductManifest is the shared product.yaml shape (pkg/agentprofiles).
type ProductManifest = agentprofiles.ProductManifest

var (
	productManifestOnce sync.Once
	productManifest     ProductManifest
	productManifestErr  error
)

// SparkQuillManifest loads and validates product.yaml once.
func SparkQuillManifest() (ProductManifest, error) {
	productManifestOnce.Do(func() {
		manifest, err := agentprofiles.LoadProductManifest(productConfigFiles, "product.yaml")
		if err != nil {
			productManifestErr = fmt.Errorf("SparkQuill %w", err)
			return
		}
		if manifest.Profile.ID != ParentProfileID || manifest.UI.Surface != ProductName {
			productManifestErr = fmt.Errorf("invalid SparkQuill product manifest")
			return
		}
		if _, ok := manifest.FindProfile(ChildProfileID); !ok {
			productManifestErr = fmt.Errorf("invalid SparkQuill product manifest: child profile %q missing", ChildProfileID)
			return
		}
		productManifest = manifest
	})
	return productManifest, productManifestErr
}

func mustSparkQuillManifest() ProductManifest {
	manifest, err := SparkQuillManifest()
	if err != nil {
		panic(err)
	}
	return manifest
}
