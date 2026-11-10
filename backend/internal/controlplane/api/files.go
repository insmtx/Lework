package api

import (
	"net/http"
	"path/filepath"
	"strings"
)

// maxFileReadBytes 是浏览器一次能读取的原始字节数。agent 还会做同样的
// 上限；这里提前设置，避免前端请求一个超出单帧限制的响应。
const maxFileReadBytes int64 = 512 << 10

// fileListParams 与 agentd/handler.FileListParams 对应。
type fileListParams struct {
	Directory string `json:"directory"`
	Path      string `json:"path"`
}

// fileReadParams 与 agentd/handler.FileReadParams 对应。
type fileReadParams struct {
	Directory string `json:"directory"`
	Path      string `json:"path"`
	MaxBytes  int64  `json:"max_bytes"`
}

// handleFileList 返回节点工作目录下的一级文件列表。
func (a *API) handleFileList(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	directory, path, ok := fileQuery(w, r)
	if !ok {
		return
	}
	if !a.requireNode(w, r, nodeID) {
		return
	}

	result, err := a.callAgent(r.Context(), nodeID, "file.list",
		fileListParams{Directory: directory, Path: path})
	if err != nil {
		a.writeAgentError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, result)
}

// handleFileContent 读取一个文件；路径必须是 workspace 内的相对路径。
func (a *API) handleFileContent(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	directory, path, ok := fileQuery(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(path) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "path is required")
		return
	}
	if !a.requireNode(w, r, nodeID) {
		return
	}

	result, err := a.callAgent(r.Context(), nodeID, "file.read",
		fileReadParams{Directory: directory, Path: path, MaxBytes: maxFileReadBytes})
	if err != nil {
		a.writeAgentError(w, err)
		return
	}
	writeRawJSON(w, http.StatusOK, result)
}

// fileQuery validates the shared directory/path query pair.
func fileQuery(w http.ResponseWriter, r *http.Request) (directory, path string, ok bool) {
	query := r.URL.Query()
	directory = query.Get("directory")
	if directory == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "directory is required")
		return "", "", false
	}
	if !filepath.IsAbs(directory) {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "directory must be absolute")
		return "", "", false
	}
	return directory, query.Get("path"), true
}

// requireNode distinguishes an unregistered node (404) from a registered but
// offline node (409, produced by callAgent).
func (a *API) requireNode(w http.ResponseWriter, r *http.Request, nodeID string) bool {
	if nodeID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "node is required")
		return false
	}
	_, exists, err := a.findNode(r.Context(), nodeID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return false
	}
	if !exists {
		writeAPIError(w, http.StatusNotFound, "not_found", "node not found")
		return false
	}
	return true
}
