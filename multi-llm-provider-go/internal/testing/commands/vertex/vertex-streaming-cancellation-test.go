package vertex

import (
	"log"
	"os"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"

	"github.com/manishiitg/multi-llm-provider-go/internal/testing"
	"github.com/manishiitg/multi-llm-provider-go/internal/testing/commands/shared"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var VertexStreamingCancellationTestCmd = &cobra.Command{
	Use:   "vertex-streaming-cancellation",
	Short: "Test Vertex AI streaming cancellation",
	Run:   runVertexStreamingCancellationTest,
}

func init() {
	VertexStreamingCancellationTestCmd.Flags().String("model", "", "Vertex AI model to test (e.g., gemini-1.5-pro)")
	VertexStreamingCancellationTestCmd.Flags().String("project-id", "", "GCP project ID (or set GCP_PROJECT_ID env var)")
	VertexStreamingCancellationTestCmd.Flags().String("location", "us-central1", "GCP location (or set GCP_LOCATION env var)")
}

func runVertexStreamingCancellationTest(cmd *cobra.Command, args []string) {
	_ = godotenv.Load("agent_go/.env")
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../.env")

	logFile := viper.GetString("log-file")
	logLevel := viper.GetString("log-level")
	testing.InitTestLogger(logFile, logLevel)
	logger := testing.GetTestLogger()

	projectID, _ := cmd.Flags().GetString("project-id")
	if projectID == "" {
		projectID = os.Getenv("GCP_PROJECT_ID")
	}
	if projectID == "" {
		log.Fatal("Project ID required: set --project-id flag or GCP_PROJECT_ID environment variable")
	}

	location, _ := cmd.Flags().GetString("location")
	if location == "" {
		location = os.Getenv("GCP_LOCATION")
		if location == "" {
			location = "us-central1"
		}
	}

	modelID, _ := cmd.Flags().GetString("model")
	if modelID == "" {
		modelID = os.Getenv("VERTEX_MODEL")
		if modelID == "" {
			log.Fatal("Model required: set --model flag or VERTEX_MODEL environment variable")
		}
	}

	// Set environment variables for Vertex adapter
	os.Setenv("GCP_PROJECT_ID", projectID)
	os.Setenv("GCP_LOCATION", location)

	llmInstance, err := llmproviders.InitializeLLM(llmproviders.Config{
		Provider:    llmproviders.ProviderVertex,
		ModelID:     modelID,
		Temperature: 0.7,
		Logger:      logger,
	})
	if err != nil {
		log.Fatalf("Failed to initialize Vertex LLM: %v", err)
	}

	shared.RunStreamingCancellationTest(llmInstance, modelID)
}

