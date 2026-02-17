package server

import (
	_ "embed"
	"net/http"
	"strconv"
)

//go:embed embed_downloads/Chrome-CDP-macOS.zip
var chromeCdpZip []byte

func (api *StreamingAPI) handleChromeCdpDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="Chrome-CDP-macOS.zip"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(chromeCdpZip)))
	w.Write(chromeCdpZip)
}
