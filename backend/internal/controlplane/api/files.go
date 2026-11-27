package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ygpkg/yg-go/logs"
)

const (
	// maxFileReadBytes 是浏览器一次能读取的原始字节数。agent 还会做同样的
	// 上限；这里提前设置，避免前端请求一个超出单帧限制的响应。
	maxFileReadBytes int64 = 512 << 10

	// fileDownloadChunkBytes 是流式下载每次向节点读取的字节数。1 MiB 的
	// 二进制经 JSON 编码最坏约 6 MiB，仍低于 8 MiB 的协议帧上限。
	fileDownloadChunkBytes int64 = 1 << 20

	// maxFileDownloadBytes 拦住误点日志或镜像文件的情况，避免把手机流量
	// 和磁盘打满。
	maxFileDownloadBytes int64 = 100 << 20
)

// windowsAbsPath 让 Windows 盘符路径通过绝对路径校验：控制面跑在 Linux
// 上时 filepath.IsAbs 不认 C:\repo, 但节点本身是 Windows。
var windowsAbsPath = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

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
	Offset    int64  `json:"offset,omitempty"`
}

// fileReadResult 与 agentd/handler.FileReadResult 对应，只保留流式下载用到的
// 字段；content 是 utf-8 文本或 base64。
type fileReadResult struct {
	Encoding  string `json:"encoding"`
	Content   string `json:"content"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
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

// handleFileDownload 把节点文件按块读出来，边收边写成 HTTP 响应体。大文件
// 不能一次读完：agent 的响应受 8 MiB 协议帧限制。响应开始后再失败就没法改
// 状态码，只能断开连接让客户端看到下载不完整。
func (a *API) handleFileDownload(w http.ResponseWriter, r *http.Request) {
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

	flusher, _ := w.(http.Flusher)
	var (
		offset      int64
		total       int64
		wroteHeader bool
	)
	for {
		result, err := a.callAgent(r.Context(), nodeID, "file.read", fileReadParams{
			Directory: directory,
			Path:      path,
			MaxBytes:  fileDownloadChunkBytes,
			Offset:    offset,
		})
		if err != nil {
			if !wroteHeader {
				a.writeAgentError(w, err)
				return
			}
			logs.Warnf("controlplane: file download interrupted for node %s path %q at %d: %v",
				nodeID, path, offset, err)
			return
		}

		var chunk fileReadResult
		if err := json.Unmarshal(result, &chunk); err != nil {
			if !wroteHeader {
				writeAPIError(w, http.StatusBadGateway, "agent_error", "agent returned invalid file content")
				return
			}
			logs.Warnf("controlplane: file download decode failed for node %s path %q: %v", nodeID, path, err)
			return
		}

		data, err := chunk.bytes()
		if err != nil {
			if !wroteHeader {
				writeAPIError(w, http.StatusBadGateway, "agent_error", "agent returned invalid file content")
				return
			}
			logs.Warnf("controlplane: file download decode failed for node %s path %q: %v", nodeID, path, err)
			return
		}

		if !wroteHeader {
			if chunk.Size > maxFileDownloadBytes {
				writeAPIError(w, http.StatusRequestEntityTooLarge, "file_too_large",
					fmt.Sprintf("file exceeds %d bytes", maxFileDownloadBytes))
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			if chunk.Size > 0 {
				w.Header().Set("Content-Length", strconv.FormatInt(chunk.Size, 10))
			}
			w.WriteHeader(http.StatusOK)
			wroteHeader = true
			total = chunk.Size
		}

		if len(data) == 0 {
			return
		}
		if _, err := w.Write(data); err != nil {
			logs.Warnf("controlplane: file download write failed for node %s path %q: %v", nodeID, path, err)
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
		offset += int64(len(data))
		if offset > maxFileDownloadBytes {
			// 节点没报总大小时首块检查拦不住，这里按已写字节数硬停。
			logs.Warnf("controlplane: file download exceeded %d bytes for node %s path %q",
				maxFileDownloadBytes, nodeID, path)
			return
		}
		if !chunk.Truncated {
			return
		}
		if total > 0 && offset >= total {
			return
		}
	}
}

// bytes 把 utf-8 文本或 base64 内容还原成原始字节。
func (r fileReadResult) bytes() ([]byte, error) {
	if r.Encoding == "base64" {
		return base64.StdEncoding.DecodeString(r.Content)
	}
	return []byte(r.Content), nil
}

// fileQuery validates the shared directory/path query pair.
func fileQuery(w http.ResponseWriter, r *http.Request) (directory, path string, ok bool) {
	query := r.URL.Query()
	directory = query.Get("directory")
	if directory == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "directory is required")
		return "", "", false
	}
	if !absoluteDirectory(directory) {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "directory must be absolute")
		return "", "", false
	}
	return directory, query.Get("path"), true
}

// absoluteDirectory 接受 POSIX 绝对路径和 Windows 盘符/UNC 路径，节点平台
// 由 agent 自己决定，控制面无法在请求时得知。
func absoluteDirectory(path string) bool {
	return filepath.IsAbs(path) || windowsAbsPath.MatchString(path) || strings.HasPrefix(path, `\\`)
}

// requireNode distinguishes an unregistered node (404) from a registered but
// offline node (409). 离线判定与列表接口共用同一个心跳窗口，因此跨实例也
// 不会把连在别处的节点当成不可用。
func (a *API) requireNode(w http.ResponseWriter, r *http.Request, nodeID string) bool {
	if nodeID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "node is required")
		return false
	}
	view, exists, err := a.findNode(r.Context(), nodeID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return false
	}
	if !exists {
		writeAPIError(w, http.StatusNotFound, "not_found", "node not found")
		return false
	}
	if !view.Connected {
		writeAPIError(w, http.StatusConflict, "node_offline", "node is offline")
		return false
	}
	return true
}
