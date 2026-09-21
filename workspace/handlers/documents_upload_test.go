package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

func uploadRequest(t *testing.T, size int) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("folder_path", "/"); err != nil {
		t.Fatal(err)
	}
	file, err := writer.CreateFormFile("file", "upload.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(make([]byte, size)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestUploadFileTenMegabyteLimit(t *testing.T) {
	oldDocsDir := viper.Get("docs-dir")
	viper.Set("docs-dir", t.TempDir())
	t.Cleanup(func() { viper.Set("docs-dir", oldDocsDir) })

	for _, tc := range []struct {
		name string
		size int
		want int
	}{
		{name: "at limit", size: 10 * 1024 * 1024, want: http.StatusOK},
		{name: "over limit", size: 10*1024*1024 + 1, want: http.StatusRequestEntityTooLarge},
		{name: "oversized request", size: 11 * 1024 * 1024, want: http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Request = uploadRequest(t, tc.size)
			UploadFile(ctx)
			if response.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", response.Code, tc.want, response.Body.String())
			}
		})
	}
}
