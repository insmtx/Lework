package opencode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ygpkg/yg-go/logs"
)

const (
	// envVersionGate 是应急开关：置 warn 时版本不符只告警，不阻断注册。
	envVersionGate = "LEROS_CLI_VERSION_GATE"
	// probeCacheFile 缓存成功探测的版本，避免每次容器启动都 exec 一次冷启动很慢的 CLI。
	probeCacheFile = "version-probe.json"
	// probeCacheTTL 让缓存最终过期，防止二进制被替换但 stat 没变（极罕见）时一直用旧值。
	probeCacheTTL = 7 * 24 * time.Hour
)

var (
	// quickProbeTimeout 是启动路径上的同步探测上限，必须有界且短：
	// worker 启动不能被一个冷启动的 CLI 拖住。
	quickProbeTimeout = 3 * time.Second
	// lazyProbeTimeout 是后台补探测的上限；探测结果只影响该 Runtime 的 Run，不影响启动。
	lazyProbeTimeout = 30 * time.Second
	// executeProbeWait 是 Run 开始前等待后台探测结果的最长时间，超时即放行，
	// 由 ACP 握手自身去暴露真正的不可用。
	executeProbeWait = 2 * time.Second
)

// probeCache 是 <dataDir>/version-probe.json 的结构。binary/size/mod_time 任一变化即失效，
// 避免镜像换了 CLI 但缓存还在。
type probeCache struct {
	Binary    string    `json:"binary"`
	Size      int64     `json:"size"`
	ModTime   int64     `json:"mod_time"`
	Version   string    `json:"version"`
	CheckedAt time.Time `json:"checked_at"`
}

// versionGate 承载“版本是否可用”的异步判定。
//
// 历史实现是 New 里同步探测、失败即返回 error，最终让整个 worker 退出；现在超时只会
// 让该 Runtime 进入待定状态，后台继续探测，最坏情况也只是这一次 Run 失败。
type versionGate struct {
	binary  string
	dataDir string

	mu       sync.Mutex
	resolved bool
	version  Version
	err      error
	done     chan struct{}
	// backgroundDone 在后台补探测结束时关闭，供测试等待，避免 goroutine 活过用例。
	backgroundDone chan struct{}
}

func newVersionGate(binary, dataDir string) *versionGate {
	gate := &versionGate{binary: binary, dataDir: dataDir, done: make(chan struct{})}
	if version, ok := gate.loadCache(); ok {
		gate.version = version
		gate.resolved = true
		close(gate.done)
		logs.Debugf("opencode version %s loaded from probe cache", version)
	}
	return gate
}

// cachedVersion 返回启动前已经从缓存解析出的版本。
func (gate *versionGate) cachedVersion() (Version, bool) {
	if gate == nil {
		return Version{}, false
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.version, gate.resolved
}

// resolve 写入首次判定结果；重复调用只保留第一次，避免后台探测覆盖启动判定。
func (gate *versionGate) resolve(version Version, err error) {
	if gate == nil {
		return
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.resolved {
		return
	}
	gate.version, gate.err, gate.resolved = version, err, true
	close(gate.done)
}

// wait 最多等待 d；返回 resolved=false 表示探测仍未结束，调用方可自行决定是否放行。
func (gate *versionGate) wait(d time.Duration) (Version, error, bool) {
	if gate == nil {
		return Version{}, nil, true
	}
	if d < 0 {
		d = 0
	}
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-gate.done:
		gate.mu.Lock()
		defer gate.mu.Unlock()
		return gate.version, gate.err, true
	case <-timer.C:
		return Version{}, nil, false
	}
}

// enforce 执行 2.1.x 门禁；应急开关开启时降级为告警。
func (gate *versionGate) enforce(version Version) error {
	err := version.RequireACPCompatible()
	if err == nil {
		return nil
	}
	if versionGateWarn() {
		logs.Warnf("opencode version gate downgraded to warning: %v", err)
		return nil
	}
	return err
}

// startBackgroundProbe 在启动探测超时后继续用较长超时补一次，成功则补写缓存。
func (gate *versionGate) startBackgroundProbe() {
	if gate == nil {
		return
	}
	gate.backgroundDone = make(chan struct{})
	go func() {
		defer close(gate.backgroundDone)
		ctx, cancel := context.WithTimeout(context.Background(), lazyProbeTimeout)
		defer cancel()

		version, err := ProbeVersion(ctx, gate.binary, lazyProbeTimeout)
		if err != nil {
			// 保持“待定”：不放行也不否定，Run 侧会告警后继续，让 ACP 握手暴露真实问题。
			logs.Warnf("opencode version probe still failing in background: %v", err)
			return
		}
		if gateErr := gate.enforce(version); gateErr != nil {
			logs.Warnf("opencode backend version check failed: %v", gateErr)
			gate.resolve(version, gateErr)
			return
		}
		gate.resolve(version, nil)
		if err := gate.saveCache(version); err != nil {
			logs.Debugf("opencode version probe cache write skipped: %v", err)
		}
	}()
}

// versionGateWarn 报告是否把版本门禁降级为告警。
func versionGateWarn() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envVersionGate))) {
	case "warn", "off":
		return true
	default:
		return false
	}
}

func (gate *versionGate) cachePath() string {
	dir := strings.TrimSpace(gate.dataDir)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, probeCacheFile)
}

// loadCache 只在 binary 的 path/size/mtime 与缓存一致且未过期时返回 true。
func (gate *versionGate) loadCache() (Version, bool) {
	path := gate.cachePath()
	if path == "" {
		return Version{}, false
	}
	info, err := os.Stat(gate.binary)
	if err != nil {
		return Version{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Version{}, false
	}
	var cached probeCache
	if err := json.Unmarshal(data, &cached); err != nil {
		return Version{}, false
	}
	if cached.Binary != gate.binary ||
		cached.Size != info.Size() ||
		cached.ModTime != info.ModTime().UnixNano() {
		return Version{}, false
	}
	if cached.CheckedAt.IsZero() || time.Since(cached.CheckedAt) > probeCacheTTL {
		return Version{}, false
	}
	version, ok := ParseVersion(cached.Version)
	if !ok {
		return Version{}, false
	}
	return version, true
}

func (gate *versionGate) saveCache(version Version) error {
	path := gate.cachePath()
	if path == "" {
		return nil
	}
	info, err := os.Stat(gate.binary)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(gate.dataDir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(probeCache{
		Binary:    gate.binary,
		Size:      info.Size(),
		ModTime:   info.ModTime().UnixNano(),
		Version:   version.String(),
		CheckedAt: time.Now(),
	})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
