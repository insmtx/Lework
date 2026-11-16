# 统一 COS 发布与移动端应用内升级 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把桌面端、Android、服务端产物统一发布到 COS `application/`，并让移动端和桌面端都能从 `RELEASE.txt` 发现新版本、完成升级。

**Architecture:** GitHub Actions 的 `release.yml` 合并桌面构建 matrix，最后一个 `publish` job 等全部平台成功后上传 COS、写 `RELEASE.txt`。移动端用固定 COS 地址检查版本、下载 APK 并调起系统安装器；桌面端先读 `RELEASE.txt`，再把 `electron-updater` 的 feed 指向版本目录。

**Tech Stack:** Go 1.25、Expo SDK 58 / React Native 0.88、Electron 39 / electron-updater 6、GitHub Actions、腾讯云 COS / coscmd。

**Spec:** `docs/design/2026-11-16-cos-unified-release-and-mobile-update-design.md`

## Global Constraints

- 发布相关改动全部在 GitHub Actions；不要修改 `.gitlab-ci.yml`，GitLab 只同步代码。
- 固定发布源：`https://leros-1395325824.cos.ap-beijing.myqcloud.com/application`。
- COS bucket `leros-1395325824`，region `ap-beijing`；凭据来自 Organization secrets `COS_SECRET_ID` / `COS_SECRET_KEY`。
- 版本号只来自 tag `vX.Y.Z`；只有稳定 tag（不含 `-`）更新 `RELEASE.txt`。
- Android 包名 `com.insmtx.lework`；只支持 Android，不做静默安装。
- 移动端测试：`pnpm --filter @lework/mobile test`；类型检查：`pnpm --filter @lework/mobile typecheck`。
- 桌面端测试：`pnpm --filter @lework/desktop test`；类型检查：`pnpm --filter @lework/desktop typecheck`。
- 前端整体检查（提交前）：`cd frontend && pnpm typecheck`。
- commit message 用中文，格式 `type(scope): subject`；只提交被 git 跟踪的文件。

## Review Focus

- `RELEASE.txt` 为空、是 HTML 错误页、或内容非法时：客户端必须保持静默，不崩溃、不弹出错误版本。Task 2 / Task 6 的解析测试覆盖。
- 远端版本等于、低于本机，或是预发布版本时：不得提示更新。Task 2 / Task 6 的比较测试覆盖。
- 版本号存在但 APK / `latest*.yml` 返回 404（平台产物尚未发布）：静默等待，不进入错误状态。Task 2 的 HEAD 测试与 Task 7 的集成逻辑覆盖。
- 下载中断、安装权限被拒、签名冲突时：给出可读错误，不出现永久 loading 或丢状态。Task 3 的状态机与 Task 4 的安装 intent 测试覆盖。
- 桌面端不支持的平台/架构、私有化构建、元数据 404 时：不显示误导性的“有新版本”。Task 6 的 `desktopTarget` 测试与现有私有化测试覆盖。

---

### Task 1: COS 发布脚本与版本工具

**Files:**
- Create: `scripts/release/version.mjs`
- Create: `scripts/release/version.test.mjs`
- Create: `scripts/release/publish-cos.sh`
- Test: `scripts/release/publish-cos.test.sh`

**Interfaces:**
- Produces:
  - `compareVersions(left: string, right: string): number`（1 / 0 / -1，非法版本抛错）
  - `isStableVersion(value: string): boolean`
  - CLI：`node scripts/release/version.mjs gt <left> <right>` 输出 `true` / `false`
  - `publish-cos.sh <local-version-dir> <version>`：把本地目录按相对路径上传到 `${RELEASE_PREFIX}/${version}/`，可选更新 `RELEASE.txt`。

- [ ] **Step 1: 写失败的版本工具测试**

```js
// scripts/release/version.test.mjs
import assert from "node:assert/strict";
import test from "node:test";
import { compareVersions, isStableVersion } from "./version.mjs";

test("compares stable versions", () => {
  assert.equal(compareVersions("0.8.0", "0.7.9"), 1);
  assert.equal(compareVersions("0.8.0", "0.8.0"), 0);
  assert.equal(compareVersions("0.7.9", "0.8.0"), -1);
});

test("prerelease sorts below its stable release", () => {
  assert.equal(compareVersions("0.8.0", "0.8.0-rc.1"), 1);
  assert.equal(compareVersions("0.8.0-rc.2", "0.8.0-rc.1"), 1);
});

test("rejects invalid versions", () => {
  assert.throws(() => compareVersions("latest", "0.8.0"));
});

test("detects stable versions", () => {
  assert.equal(isStableVersion("0.8.0"), true);
  assert.equal(isStableVersion("0.8.0-rc.1"), false);
  assert.equal(isStableVersion("v0.8.0"), false);
});
```

- [ ] **Step 2: 运行测试确认失败**

Run: `node --test scripts/release/version.test.mjs`
Expected: FAIL，`Cannot find module './version.mjs'`。

- [ ] **Step 3: 实现版本工具**

实现 `version.mjs`：解析可选 `v` 前缀、点分数字与 `-prerelease`；预发布比较规则与桌面端 `isVersionNewer` 一致（数字段按数值，非数字按字符串，数字段低于非数字段）。CLI 用 `node:url` 判断是否直接执行。

- [ ] **Step 4: 写失败的发布脚本 dry-run 测试**

```bash
# scripts/release/publish-cos.test.sh
set -euo pipefail
tmp="$(mktemp -d)"
mkdir -p "$tmp/android" "$tmp/server"
printf apk > "$tmp/android/lework-android-0.8.0.apk"
printf tar > "$tmp/server/lework_0.8.0_linux_amd64.tar.gz"

out="$(DRY_RUN=1 UPDATE_RELEASE_FILE=1 RELEASE_TXT_EXISTING=0.7.9 \
  RELEASE_PREFIX=application \
  bash scripts/release/publish-cos.sh "$tmp" 0.8.0)"

printf '%s' "$out" | grep -q "coscmd upload"
printf '%s' "$out" | grep -q "application/0.8.0/android/lework-android-0.8.0.apk"
printf '%s' "$out" | grep -q "application/0.8.0/server/lework_0.8.0_linux_amd64.tar.gz"
printf '%s' "$out" | grep -q "application/RELEASE.txt"
```

再补一个断言：`RELEASE_TXT_EXISTING=0.8.0` 时不出现 `application/RELEASE.txt`；`VERSION=0.8.0-rc.1` 时也不出现。

- [ ] **Step 5: 运行脚本测试确认失败**

Run: `bash scripts/release/publish-cos.test.sh`
Expected: FAIL，`publish-cos.sh: No such file`。

- [ ] **Step 6: 实现 `publish-cos.sh`**

要求：

- `set -euo pipefail`；参数为 `<local-version-dir> <version>`。
- 环境变量：`RELEASE_PREFIX`（默认 `application`）、`COS_SECRET_ID`、`COS_SECRET_KEY`、`COS_BUCKET`、`COS_REGION`、`UPDATE_RELEASE_FILE`（默认 `0`）、`DRY_RUN`（默认 `0`）、`RELEASE_TXT_EXISTING`（测试用）。
- 非 dry-run 时先 `coscmd config -a ... -s ... -b ... -r ...`；`DRY_RUN=1` 不要求 `COS_*` 凭据。
- 版本取自第二个参数 `$2`；遍历本地目录所有文件，按相对路径上传到 `${RELEASE_PREFIX}/$2/<relative>`。
- `UPDATE_RELEASE_FILE=1` 且版本稳定时：读取现有 `RELEASE.txt`（非 dry-run 用 `curl -fsS "${PUBLIC_BASE}/${RELEASE_PREFIX}/RELEASE.txt?t=$(date +%s)"`，404 视为空），仅当新版本更大时上传临时文件到 `${RELEASE_PREFIX}/RELEASE.txt`。
- `DRY_RUN=1` 只打印 `coscmd upload <local> <remote>`，不访问网络。

- [ ] **Step 7: 运行测试确认通过**

Run: `node --test scripts/release/version.test.mjs && bash scripts/release/publish-cos.test.sh`
Expected: 全部 PASS，无输出错误。

- [ ] **Step 8: 提交**

```bash
git add scripts/release/version.mjs scripts/release/version.test.mjs \
  scripts/release/publish-cos.sh scripts/release/publish-cos.test.sh
git commit -m "feat(release): 增加 COS 统一发布脚本"
```

---

### Task 2: 移动端版本比较与更新检查

**Files:**
- Create: `frontend/apps/mobile/src/update/version.ts`
- Create: `frontend/apps/mobile/src/update/version.test.ts`
- Create: `frontend/apps/mobile/src/update/source.ts`
- Create: `frontend/apps/mobile/src/update/source.test.ts`
- Create: `frontend/apps/mobile/src/update/check.ts`
- Create: `frontend/apps/mobile/src/update/check.test.ts`

**Interfaces:**
- Produces:
  - `compareVersions(left: string, right: string): number`
  - `isVersionNewer(candidate: string, current: string): boolean`
  - `MOBILE_UPDATE_BASE_URL: string`（可用 `EXPO_PUBLIC_LEWORK_UPDATE_BASE_URL` 在本地覆盖）
  - `MOBILE_APP_ID = "com.insmtx.lework"`
  - `parseReleaseVersion(body: string): string`
  - `releaseTxtUrl(baseUrl?: string, now?: number): string`
  - `apkUrl(version: string, baseUrl?: string): string`
  - `checkForUpdate(options: { currentVersion: string; fetchImpl?: typeof fetch; baseUrl?: string; now?: () => number }): Promise<CheckResult>`
  - `CheckResult = { kind: "update"; version: string; apkUrl: string } | { kind: "none" } | { kind: "error"; message: string }`

- [ ] **Step 1: 写失败的版本比较测试**

```ts
// src/update/version.test.ts
import { describe, expect, it } from "vitest";
import { compareVersions, isVersionNewer } from "./version";

describe("version", () => {
  it("compares stable versions", () => {
    expect(compareVersions("0.8.0", "0.7.9")).toBe(1);
    expect(compareVersions("0.8.0", "0.8.0")).toBe(0);
    expect(compareVersions("0.7.9", "0.8.0")).toBe(-1);
  });

  it("treats prerelease as older than stable", () => {
    expect(isVersionNewer("0.8.0", "0.8.0-rc.1")).toBe(true);
    expect(isVersionNewer("0.8.0-rc.1", "0.8.0")).toBe(false);
    expect(isVersionNewer("0.8.0-rc.2", "0.8.0-rc.1")).toBe(true);
  });

  it("rejects invalid versions", () => {
    expect(() => compareVersions("latest", "0.8.0")).toThrow();
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

Run: `pnpm --filter @lework/mobile test src/update/version.test.ts`
Expected: FAIL，模块不存在。

- [ ] **Step 3: 实现 `version.ts`**

实现与桌面端 `isVersionNewer` 等价的逻辑；非法版本抛 `Error("无法比较版本号：...")`。

- [ ] **Step 4: 写失败的源/URL 测试**

```ts
// src/update/source.test.ts
import { describe, expect, it } from "vitest";
import {
  MOBILE_UPDATE_BASE_URL,
  apkUrl,
  parseReleaseVersion,
  releaseTxtUrl,
} from "./source";

describe("update source", () => {
  it("parses the first line of RELEASE.txt", () => {
    expect(parseReleaseVersion("0.8.0\n")).toBe("0.8.0");
    expect(parseReleaseVersion("\n 0.8.0 \n")).toBe("0.8.0");
  });

  it("rejects empty or html bodies", () => {
    expect(() => parseReleaseVersion("")).toThrow();
    expect(() => parseReleaseVersion("<html>error</html>")).toThrow();
  });

  it("builds cache-busted release and apk urls", () => {
    expect(MOBILE_UPDATE_BASE_URL).toBe(
      "https://leros-1395325824.cos.ap-beijing.myqcloud.com/application",
    );
    expect(releaseTxtUrl(MOBILE_UPDATE_BASE_URL, 1700000000000)).toBe(
      `${MOBILE_UPDATE_BASE_URL}/RELEASE.txt?t=1700000000000`,
    );
    expect(apkUrl("0.8.0")).toBe(
      `${MOBILE_UPDATE_BASE_URL}/0.8.0/android/lework-android-0.8.0.apk`,
    );
  });
});
```

- [ ] **Step 5: 实现 `source.ts` 并运行测试**

`MOBILE_UPDATE_BASE_URL` 默认固定为 COS 地址，允许 `process.env.EXPO_PUBLIC_LEWORK_UPDATE_BASE_URL` 覆盖（仅构建期，运行时不可被控制面地址改变）。

Run: `pnpm --filter @lework/mobile test src/update/source.test.ts`
Expected: PASS。

- [ ] **Step 6: 写失败的检查流程测试**

```ts
// src/update/check.test.ts
import { describe, expect, it } from "vitest";
import { checkForUpdate } from "./check";

function response(status: number, body = ""): Response {
  return new Response(body, { status });
}

describe("checkForUpdate", () => {
  it("returns update when remote is newer and apk exists", async () => {
    const calls: string[] = [];
    const fetchImpl = (async (input: RequestInfo | URL, init?: RequestInit) => {
      calls.push(`${init?.method ?? "GET"} ${String(input)}`);
      if (init?.method === "HEAD") return response(200);
      return response(200, "0.8.0\n");
    }) as typeof fetch;

    await expect(
      checkForUpdate({ currentVersion: "0.7.0", fetchImpl, now: () => 123 }),
    ).resolves.toEqual({
      kind: "update",
      version: "0.8.0",
      apkUrl:
        "https://leros-1395325824.cos.ap-beijing.myqcloud.com/application/0.8.0/android/lework-android-0.8.0.apk",
    });
    expect(calls).toEqual([
      "GET https://leros-1395325824.cos.ap-beijing.myqcloud.com/application/RELEASE.txt?t=123",
      "HEAD https://leros-1395325824.cos.ap-beijing.myqcloud.com/application/0.8.0/android/lework-android-0.8.0.apk",
    ]);
  });

  it("returns none for same version, missing apk, or invalid body", async () => {
    const same = (async () => response(200, "0.8.0\n")) as typeof fetch;
    await expect(
      checkForUpdate({ currentVersion: "0.8.0", fetchImpl: same }),
    ).resolves.toEqual({ kind: "none" });

    const missing = (async (_input: RequestInfo | URL, init?: RequestInit) =>
      init?.method === "HEAD" ? response(404) : response(200, "0.8.0\n")) as typeof fetch;
    await expect(
      checkForUpdate({ currentVersion: "0.7.0", fetchImpl: missing }),
    ).resolves.toEqual({ kind: "none" });

    const html = (async () => response(200, "<html>oops</html>")) as typeof fetch;
    await expect(
      checkForUpdate({ currentVersion: "0.7.0", fetchImpl: html }),
    ).resolves.toEqual({ kind: "error", message: expect.any(String) });
  });

  it("returns error on http failure or network rejection", async () => {
    const http500 = (async () => response(500)) as typeof fetch;
    await expect(
      checkForUpdate({ currentVersion: "0.7.0", fetchImpl: http500 }),
    ).resolves.toEqual({ kind: "error", message: expect.stringContaining("500") });

    const offline = (async () => {
      throw new Error("offline");
    }) as typeof fetch;
    await expect(
      checkForUpdate({ currentVersion: "0.7.0", fetchImpl: offline }),
    ).resolves.toEqual({ kind: "error", message: expect.stringContaining("offline") });
  });
});
```

- [ ] **Step 7: 实现 `check.ts` 并运行全部移动端更新测试**

`checkForUpdate` 必须：GET `RELEASE.txt`（带时间戳）、解析、比较；有新版本再 HEAD APK；APK 非 2xx 返回 `none`；任何解析/网络/HTTP 失败返回 `error`。不得读取 `settings.baseUrl`。

Run: `pnpm --filter @lework/mobile test src/update`
Expected: 全部 PASS。

- [ ] **Step 8: 提交**

```bash
git add frontend/apps/mobile/src/update
git commit -m "feat(mobile): 增加更新源与版本检查"
```

---

### Task 3: 移动端更新状态机

**Files:**
- Create: `frontend/apps/mobile/src/update/state.ts`
- Test: `frontend/apps/mobile/src/update/state.test.ts`

**Interfaces:**
- Produces:
  - `UpdateState`（`idle | checking | available | downloading | downloaded | installing | error`）
  - `UpdateAction`（`check-started`、`update-available`、`no-update`、`download-started`、`download-progress`、`download-finished`、`install-started`、`failed`、`dismissed`）
  - `reduceUpdateState(state: UpdateState, action: UpdateAction): UpdateState`
  - `isUpdateVisible(state: UpdateState): boolean`

- [ ] **Step 1: 写失败的状态机测试**

```ts
// src/update/state.test.ts
import { describe, expect, it } from "vitest";
import { isUpdateVisible, reduceUpdateState, type UpdateState } from "./state";

const idle: UpdateState = { phase: "idle" };

describe("reduceUpdateState", () => {
  it("walks check -> available -> downloading -> downloaded", () => {
    let state = reduceUpdateState(idle, { type: "check-started" });
    expect(state.phase).toBe("checking");

    state = reduceUpdateState(state, {
      type: "update-available",
      version: "0.8.0",
      apkUrl: "https://example.com/app.apk",
    });
    expect(state).toMatchObject({ phase: "available", version: "0.8.0" });

    state = reduceUpdateState(state, { type: "download-started" });
    expect(state).toMatchObject({ phase: "downloading", progress: 0 });

    state = reduceUpdateState(state, { type: "download-progress", progress: 42.6 });
    expect(state).toMatchObject({ phase: "downloading", progress: 43 });

    state = reduceUpdateState(state, { type: "download-finished", fileUri: "file:///a.apk" });
    expect(state).toEqual({
      phase: "downloaded",
      version: "0.8.0",
      apkUrl: "https://example.com/app.apk",
      fileUri: "file:///a.apk",
    });
  });

  it("clamps progress and keeps version on failure", () => {
    let state = reduceUpdateState(idle, {
      type: "update-available",
      version: "0.8.0",
      apkUrl: "https://example.com/app.apk",
    });
    state = reduceUpdateState(state, { type: "download-started" });
    state = reduceUpdateState(state, { type: "download-progress", progress: 150 });
    expect(state).toMatchObject({ phase: "downloading", progress: 100 });

    state = reduceUpdateState(state, { type: "failed", message: "网络中断" });
    expect(state).toMatchObject({
      phase: "error",
      message: "网络中断",
      version: "0.8.0",
      apkUrl: "https://example.com/app.apk",
    });
  });

  it("returns to idle on no-update and dismissed", () => {
    expect(reduceUpdateState({ phase: "checking" }, { type: "no-update" })).toEqual(idle);
    expect(reduceUpdateState({ phase: "downloaded", version: "0.8.0", apkUrl: "u", fileUri: "f" }, { type: "dismissed" })).toEqual(idle);
  });
});

describe("isUpdateVisible", () => {
  it("shows update states only", () => {
    expect(isUpdateVisible({ phase: "available", version: "0.8.0", apkUrl: "u" })).toBe(true);
    expect(isUpdateVisible({ phase: "downloading", version: "0.8.0", apkUrl: "u", progress: 1 })).toBe(true);
    expect(isUpdateVisible({ phase: "idle" })).toBe(false);
    expect(isUpdateVisible({ phase: "checking" })).toBe(false);
    expect(isUpdateVisible({ phase: "installing", version: "0.8.0", fileUri: "f" })).toBe(false);
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

Run: `pnpm --filter @lework/mobile test src/update/state.test.ts`
Expected: FAIL，模块不存在。

- [ ] **Step 3: 实现 `state.ts`**

按测试实现；进度四舍五入并夹在 0–100；`failed` 保留出错前的 `version` / `apkUrl` / `fileUri`；`dismissed` 回到 `idle`。

- [ ] **Step 4: 运行测试确认通过**

Run: `pnpm --filter @lework/mobile test src/update/state.test.ts`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add frontend/apps/mobile/src/update/state.ts frontend/apps/mobile/src/update/state.test.ts
git commit -m "feat(mobile): 增加更新状态机"
```

---

### Task 4: 移动端下载与安装适配器

**Files:**
- Create: `frontend/apps/mobile/src/update/download.ts`
- Test: `frontend/apps/mobile/src/update/download.test.ts`
- Create: `frontend/apps/mobile/src/update/install.ts`
- Test: `frontend/apps/mobile/src/update/install.test.ts`
- Test: `frontend/apps/mobile/src/update/config.test.ts`
- Modify: `frontend/apps/mobile/package.json`
- Modify: `frontend/apps/mobile/app.json`

**Interfaces:**
- Produces:
  - `updateApkPath(cacheDirectory: string, version: string): string`
  - `clampProgress(percent: number): number`
  - `downloadApk(options: { url: string; destination: string; onProgress?: (percent: number) => void }): Promise<string>`
  - `buildInstallIntent(contentUri: string): { action: string; params: { data: string; type: string; flags: number } }`
  - `installApk(fileUri: string): Promise<void>`

- [ ] **Step 1: 写失败的纯函数测试**

```ts
// src/update/download.test.ts
import { describe, expect, it } from "vitest";
import { clampProgress, updateApkPath } from "./download";

describe("download helpers", () => {
  it("builds a versioned cache path", () => {
    expect(updateApkPath("/cache/", "0.8.0")).toBe("/cache/lework-update-0.8.0.apk");
    expect(updateApkPath("/cache", "0.8.0")).toBe("/cache/lework-update-0.8.0.apk");
  });

  it("clamps progress", () => {
    expect(clampProgress(-5)).toBe(0);
    expect(clampProgress(42.6)).toBe(43);
    expect(clampProgress(150)).toBe(100);
  });
});
```

```ts
// src/update/install.test.ts
import { describe, expect, it } from "vitest";
import { buildInstallIntent } from "./install";

describe("buildInstallIntent", () => {
  it("opens the package installer with a content uri", () => {
    expect(buildInstallIntent("content://com.insmtx.lework.fileprovider/cache/a.apk")).toEqual({
      action: "android.intent.action.VIEW",
      params: {
        data: "content://com.insmtx.lework.fileprovider/cache/a.apk",
        type: "application/vnd.android.package-archive",
        flags: 1,
      },
    });
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

Run: `pnpm --filter @lework/mobile test src/update/download.test.ts src/update/install.test.ts`
Expected: FAIL，模块不存在。

- [ ] **Step 3: 实现下载与安装适配器**

- `downloadApk` 使用 `expo-file-system/legacy` 的 `createDownloadResumable`，进度回调经 `clampProgress`；下载结果状态非 2xx 或缺少 URI 时抛错；失败时 `deleteAsync(destination, { idempotent: true })`。
- `installApk` 用 `FileSystem.getContentUriAsync(fileUri)`，再 `IntentLauncher.startActivityAsync(intent.action, intent.params)`。
- `buildInstallIntent` 返回上面的纯对象。

- [ ] **Step 4: 写失败的应用配置测试**

```ts
// src/update/config.test.ts
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const appUrl = new URL("../../app.json", import.meta.url);
const pkgUrl = new URL("../../package.json", import.meta.url);

describe("mobile update app config", () => {
  it("requests install packages permission", () => {
    const app = JSON.parse(readFileSync(appUrl, "utf8"));
    expect(app.expo.android.permissions).toContain(
      "android.permission.REQUEST_INSTALL_PACKAGES",
    );
  });

  it("declares the expo modules used by the updater", () => {
    const pkg = JSON.parse(readFileSync(pkgUrl, "utf8"));
    expect(pkg.dependencies["expo-application"]).toBeTruthy();
    expect(pkg.dependencies["expo-file-system"]).toBeTruthy();
    expect(pkg.dependencies["expo-intent-launcher"]).toBeTruthy();
  });
});
```

- [ ] **Step 5: 添加依赖与权限**

从 `frontend` 目录执行：

```bash
pnpm --filter @lework/mobile add expo-application@~58.1.2 expo-file-system@~58.0.13 expo-intent-launcher@~58.0.3
```

在 `app.json` 的 `expo.android` 下增加：

```json
"permissions": ["android.permission.REQUEST_INSTALL_PACKAGES"]
```

- [ ] **Step 6: 运行测试与类型检查**

Run: `pnpm --filter @lework/mobile test src/update && pnpm --filter @lework/mobile typecheck`
Expected: PASS。

- [ ] **Step 7: 提交**

```bash
git add frontend/apps/mobile/package.json frontend/pnpm-lock.yaml \
  frontend/apps/mobile/app.json frontend/apps/mobile/src/update
git commit -m "feat(mobile): 增加 APK 下载与安装适配器"
```

---

### Task 5: 移动端更新入口与设置页

**Files:**
- Create: `frontend/apps/mobile/src/update/schedule.ts`
- Test: `frontend/apps/mobile/src/update/schedule.test.ts`
- Create: `frontend/apps/mobile/src/update/format.ts`
- Test: `frontend/apps/mobile/src/update/format.test.ts`
- Create: `frontend/apps/mobile/src/update/update-context.tsx`
- Create: `frontend/apps/mobile/src/update/update-gate.tsx`
- Modify: `frontend/apps/mobile/app/_layout.tsx`
- Modify: `frontend/apps/mobile/app/settings/index.tsx`
- Modify: `docs/operations/mobile.md`

**Interfaces:**
- Produces:
  - `UPDATE_CHECK_INTERVAL_MS = 30 * 60 * 1000`
  - `UPDATE_RESUME_CHECK_MS = 5 * 60 * 1000`
  - `UPDATE_SNOOZE_MS = 5 * 60 * 1000`
  - `shouldCheckOnResume(lastCheckedAt: number | null, now: number): boolean`
  - `describeVersion(currentVersion: string | null, state: UpdateState): string`
  - `UpdateProvider`、`useUpdate()`（返回 `state`、`currentVersion`、`manualCheck`、`install`、`dismiss`、`retry`）

- [ ] **Step 1: 写失败的调度与文案测试**

```ts
// src/update/schedule.test.ts
import { describe, expect, it } from "vitest";
import { UPDATE_RESUME_CHECK_MS, shouldCheckOnResume } from "./schedule";

describe("shouldCheckOnResume", () => {
  it("checks when there is no previous check", () => {
    expect(shouldCheckOnResume(null, 1_000)).toBe(true);
  });
  it("waits five minutes between resume checks", () => {
    expect(shouldCheckOnResume(1_000, 1_000 + UPDATE_RESUME_CHECK_MS - 1)).toBe(false);
    expect(shouldCheckOnResume(1_000, 1_000 + UPDATE_RESUME_CHECK_MS)).toBe(true);
  });
});
```

```ts
// src/update/format.test.ts
import { describe, expect, it } from "vitest";
import { describeVersion } from "./format";

describe("describeVersion", () => {
  it("shows the current version", () => {
    expect(describeVersion("0.8.0", { phase: "idle" })).toBe("0.8.0");
    expect(describeVersion(null, { phase: "idle" })).toBe("未知");
  });
  it("shows update state when available", () => {
    expect(
      describeVersion("0.8.0", { phase: "available", version: "0.9.0", apkUrl: "u" }),
    ).toBe("0.9.0 可更新");
    expect(
      describeVersion("0.8.0", { phase: "downloading", version: "0.9.0", apkUrl: "u", progress: 10 }),
    ).toBe("0.9.0 下载中");
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

Run: `pnpm --filter @lework/mobile test src/update/schedule.test.ts src/update/format.test.ts`
Expected: FAIL。

- [ ] **Step 3: 实现调度常量、文案与更新上下文**

`update-context.tsx` 要求：

- 启动后 `setTimeout` 1 秒检查一次；前台 `setInterval` 30 分钟；`AppState` 变为 `active` 且 `shouldCheckOnResume` 为真时补查。
- `Application.applicationId !== MOBILE_APP_ID` 或 `Application.nativeApplicationVersion` 为空时禁用检查。
- 自动检查失败静默；手动检查失败用 `Alert.alert` 显示。
- 发现新版本后 dispatch `update-available`，随后自动开始下载。
- `install()` 调 `installApk`；`dismiss()` 隐藏并 5 分钟后重新提示；`retry()` 重新下载或重新检查。
- 所有 `fetch` / 下载 / 安装异常都 dispatch `failed`，不得让状态停在 `checking` / `downloading`。

- [ ] **Step 4: 实现更新 Modal**

`update-gate.tsx` 用 React Native `Modal` + `useTheme()`：

- `available` / `downloading`：标题「发现新版本 vX」，下载中显示进度条与百分比；按钮「稍后」「取消下载」。
- `downloaded`：标题「更新已下载」，按钮「立即安装」「稍后」。
- `error`：显示消息与「重试」「稍后」。
- `installing` 不显示 Modal（系统安装器已经在前台）。

- [ ] **Step 5: 接入根布局与设置页**

- 在 `app/_layout.tsx` 的 `ThemeProvider` 内包一层 `UpdateProvider`，在 `AppShell` 内渲染 `<UpdateGate />`。
- 在 `app/settings/index.tsx` 的菜单卡片末尾增加「版本」行，`value={describeVersion(currentVersion, state)}`，`onPress={manualCheck}`，`last`。

- [ ] **Step 6: 更新移动端文档**

在 `docs/operations/mobile.md` 增加「应用内升级」小节：检查时机、下载与安装流程、首次需要允许安装未知应用、签名一致要求，以及手工验收步骤。

- [ ] **Step 7: 运行测试与类型检查**

Run: `pnpm --filter @lework/mobile test && pnpm --filter @lework/mobile typecheck`
Expected: PASS。

- [ ] **Step 8: 手工验证**

在 Android 真机或模拟器上：

1. 安装旧版本 APK。
2. 把 `RELEASE.txt` 与新版 APK 按布局上传到测试前缀。
3. 启动 App：出现更新 Modal，下载进度前进。
4. 点「立即安装」：系统安装器打开；允许安装未知应用后完成覆盖安装。
5. 重启 App：版本号更新，不再提示。
6. 关闭网络后手动检查：显示可读错误，不崩溃。

- [ ] **Step 9: 提交**

```bash
git add frontend/apps/mobile/app/_layout.tsx frontend/apps/mobile/app/settings/index.tsx \
  frontend/apps/mobile/src/update docs/operations/mobile.md
git commit -m "feat(mobile): 增加应用内升级入口"
```

---

### Task 6: 桌面端更新源工具

**Files:**
- Create: `frontend/apps/desktop/src/main/update-source.ts`
- Test: `frontend/apps/desktop/src/main/__tests__/update-source.test.ts`
- Modify: `frontend/apps/desktop/src/main/linux-update.ts`
- Test: `frontend/apps/desktop/src/main/__tests__/linux-update.test.ts`

**Interfaces:**
- Produces:
  - `DESKTOP_UPDATE_BASE_URL: string`（可用 `LEWORK_UPDATE_BASE_URL` 在开发环境覆盖）
  - `releaseTxtUrl(baseUrl?: string, now?: number): string`
  - `parseReleaseVersion(body: string): string`
  - `DesktopTarget = { dir: string; metadataFile: "latest-mac.yml" | "latest.yml" | "latest-linux.yml" }`
  - `desktopTarget(platform: NodeJS.Platform, arch: string): DesktopTarget | null`
  - `feedUrl(baseUrl: string, version: string, target: DesktopTarget): string`
  - `metadataUrl(baseUrl: string, version: string, target: DesktopTarget, now?: number): string`
  - `LinuxUpdateMetadata` 增加可选 `path?: string`

- [ ] **Step 1: 写失败的更新源测试**

```ts
// src/main/__tests__/update-source.test.ts
import { describe, expect, it } from "vitest";
import {
  DESKTOP_UPDATE_BASE_URL,
  desktopTarget,
  feedUrl,
  metadataUrl,
  parseReleaseVersion,
  releaseTxtUrl,
} from "../update-source";

describe("desktop update source", () => {
  it("parses RELEASE.txt and rejects html", () => {
    expect(parseReleaseVersion("0.8.0\n")).toBe("0.8.0");
    expect(() => parseReleaseVersion("")).toThrow();
    expect(() => parseReleaseVersion("<html>error</html>")).toThrow();
  });

  it("maps supported platform/arch to version directories", () => {
    expect(desktopTarget("darwin", "arm64")).toEqual({
      dir: "darwin/arm64",
      metadataFile: "latest-mac.yml",
    });
    expect(desktopTarget("darwin", "x64")?.dir).toBe("darwin/x64");
    expect(desktopTarget("win32", "x64")).toEqual({
      dir: "win32/x64",
      metadataFile: "latest.yml",
    });
    expect(desktopTarget("linux", "x64")).toEqual({
      dir: "linux/x64",
      metadataFile: "latest-linux.yml",
    });
    expect(desktopTarget("linux", "arm64")).toBeNull();
    expect(desktopTarget("freebsd", "x64")).toBeNull();
  });

  it("builds cache-busted urls", () => {
    const target = desktopTarget("darwin", "arm64")!;
    const feed = `${DESKTOP_UPDATE_BASE_URL}/0.8.0/darwin/arm64`;
    expect(feedUrl(DESKTOP_UPDATE_BASE_URL, "0.8.0", target)).toBe(feed);
    expect(metadataUrl(DESKTOP_UPDATE_BASE_URL, "0.8.0", target, 123)).toBe(
      `${feed}/latest-mac.yml?t=123`,
    );
    expect(releaseTxtUrl(DESKTOP_UPDATE_BASE_URL, 123)).toBe(
      `${DESKTOP_UPDATE_BASE_URL}/RELEASE.txt?t=123`,
    );
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

Run: `pnpm --filter @lework/desktop test src/main/__tests__/update-source.test.ts`
Expected: FAIL。

- [ ] **Step 3: 实现 `update-source.ts`**

`desktopTarget` 只返回 GitHub 实际构建的四种组合：`darwin/arm64`、`darwin/x64`、`win32/x64`、`linux/x64`；其他返回 `null`。`DESKTOP_UPDATE_BASE_URL` 默认固定为 COS 地址，允许 `process.env.LEWORK_UPDATE_BASE_URL` 覆盖，便于本地指向测试前缀。

- [ ] **Step 4: 扩展 Linux 元数据解析**

在 `linux-update.ts` 的 `LinuxUpdateMetadata` 增加 `path?: string`，用现有 `readYamlScalar` 读取 `path`；在 `linux-update.test.ts` 增加断言：

```ts
expect(parseLinuxUpdateMetadata("version: 0.8.0\npath: Lework-0.8.0-linux-amd64.deb\n")).toEqual({
  version: "0.8.0",
  path: "Lework-0.8.0-linux-amd64.deb",
  releaseDate: undefined,
});
```

- [ ] **Step 5: 运行测试确认通过**

Run: `pnpm --filter @lework/desktop test src/main/__tests__/update-source.test.ts src/main/__tests__/linux-update.test.ts`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add frontend/apps/desktop/src/main/update-source.ts \
  frontend/apps/desktop/src/main/__tests__/update-source.test.ts \
  frontend/apps/desktop/src/main/linux-update.ts \
  frontend/apps/desktop/src/main/__tests__/linux-update.test.ts
git commit -m "feat(desktop): 增加 COS 版本目录解析"
```

---

### Task 7: 桌面端两段式更新

**Files:**
- Modify: `frontend/apps/desktop/src/main/auto-update.ts`
- Modify: `frontend/apps/desktop/src/shared/auto-update.ts`
- Modify: `frontend/packages/app-ui/components/layout/LeftRail.tsx`
- Test: `frontend/apps/desktop/src/main/__tests__/update-wiring.test.ts`
- Modify: `frontend/apps/desktop/electron-builder.yml`
- Modify: `frontend/apps/desktop/dev-app-update.yml`

**Interfaces:**
- Consumes: Task 6 的 `DESKTOP_UPDATE_BASE_URL`、`desktopTarget`、`feedUrl`、`metadataUrl`、`releaseTxtUrl`、`parseReleaseVersion`、`isVersionNewer`。
- Produces: `DesktopUpdateState.downloadUrl?: string`；Linux 可下载 `.deb` 的菜单项。

- [ ] **Step 1: 写失败的接线测试**

```ts
// src/main/__tests__/update-wiring.test.ts
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const root = resolve(__dirname, "../../..");

describe("desktop update wiring", () => {
  it("reads RELEASE.txt before asking electron-updater", () => {
    const source = readFileSync(resolve(root, "src/main/auto-update.ts"), "utf8");
    expect(source).toContain("releaseTxtUrl");
    expect(source).toContain("setFeedURL");
    expect(source).not.toContain("applework.insmtx.com");
  });

  it("keeps private deployments disabled", () => {
    const source = readFileSync(resolve(root, "src/main/auto-update.ts"), "utf8");
    expect(source).toContain('process.env.LEWORK_DEPLOYMENT_MODE === "private"');
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

Run: `pnpm --filter @lework/desktop test src/main/__tests__/update-wiring.test.ts`
Expected: FAIL（仍有旧域名 / 没有 `setFeedURL`）。

- [ ] **Step 3: 改造 `checkDesktopUpdates`**

按以下顺序实现：

1. 私有化构建直接返回。
2. `net.fetch(releaseTxtUrl(...), { cache: "no-store" })`，解析 `RELEASE.txt`。
3. 用 `isVersionNewer(latest, app.getVersion())` 判断；没有新版本时设为 `up-to-date`，不访问版本目录。
4. 计算 `desktopTarget(process.platform, process.arch)`；为 `null` 时设置 `unsupported`（平台不支持自动更新）。
5. macOS / Windows：先 `HEAD metadataUrl(...)`；非 2xx 时设为 `up-to-date`（产物尚未发布）；成功则 `updater.setFeedURL({ provider: "generic", url: feedUrl(...) })` 后 `checkForUpdates()`。`getUpdater()` 构造时仍可传 COS 基址作为占位，真正使用的 URL 由 `setFeedURL` 决定。
6. Linux：GET `metadataUrl(...)`，解析 `latest-linux.yml`；有新版本时设置为 `available`，`downloadUrl` 指向同目录的 `.deb` 文件，`canRestart: false`。
7. 任何网络/解析错误设置 `error`，保留原有 `lastCheckedAt` 行为。

- [ ] **Step 4: 增加 Linux 下载入口**

- 在 `shared/auto-update.ts` 的 `DesktopUpdateState` 增加 `downloadUrl?: string`。
- 在 `LeftRail.tsx` 的 `DesktopUpdateMenuSection` 中，当 `updateState.phase === "available" && updateState.downloadUrl` 时渲染一个「下载安装包」菜单项，调用 `desktopApi.openExternal(updateState.downloadUrl)`；把本地 `DesktopUpdateApi` 类型补上 `openExternal`。

- [ ] **Step 5: 清理旧更新源**

- `electron-builder.yml` 的 `publish.url` 改为 COS `application` 基址（保留 `publish` 段，electron-builder 仍需要它生成 `latest*.yml`）。
- `dev-app-update.yml` 的 `url` 同样改为 COS 基址。

- [ ] **Step 6: 运行测试与类型检查**

Run: `pnpm --filter @lework/desktop test && pnpm --filter @lework/desktop typecheck`
Expected: PASS。

- [ ] **Step 7: 手工验证**

用测试版本目录：

1. 在本机启动开发版桌面端。
2. 把 `RELEASE.txt` 指向一个更高版本，并准备该版本的 `latest-mac.yml` / `latest.yml`。
3. 检查更新：应进入 `available` 并开始下载。
4. 把 `RELEASE.txt` 指回当前版本：应显示「当前已经是最新版本」。
5. 删除版本目录后再检查：应保持静默/最新，而不是 `error`。
6. Linux：检查更新后菜单出现「下载安装包」，点击用系统浏览器打开 `.deb`。

- [ ] **Step 8: 提交**

```bash
git add frontend/apps/desktop/src/main/auto-update.ts \
  frontend/apps/desktop/src/shared/auto-update.ts \
  frontend/packages/app-ui/components/layout/LeftRail.tsx \
  frontend/apps/desktop/src/main/__tests__/update-wiring.test.ts \
  frontend/apps/desktop/electron-builder.yml frontend/apps/desktop/dev-app-update.yml
git commit -m "feat(desktop): 从 COS 版本目录检查更新"
```

---

### Task 8: 合并桌面发布并统一上传 COS

**Files:**
- Modify: `.github/workflows/release.yml`
- Delete: `.github/workflows/desktop-release.yml`
- Modify: `docs/operations/release.md`
- Modify: `docs/README.md`

**Interfaces:**
- Consumes: Task 1 的 `scripts/release/publish-cos.sh`。
- Produces: 单一 tag 工作流：`prepare → frontend/backend/android/desktop → publish`；`publish` 上传 COS 并写 `RELEASE.txt`。

- [ ] **Step 1: 把桌面构建并入 `release.yml`**

- 把 `desktop-release.yml` 的 `build` job 复制到 `release.yml`，保留 macOS 签名/notarize、Electron 缓存、重试与 `Set desktop version from tag` 步骤。
- 桌面 job 增加 `needs: prepare`，版本使用 `needs.prepare.outputs.version`；`workflow_dispatch` 时也用同一个版本。
- 删除 `desktop-release.yml`。

- [ ] **Step 2: 改造 `publish` job 的依赖与产物收集**

- `publish` 的 `needs` 增加 `desktop`。
- 下载 `release-*` 到 `release-assets/`，下载 `desktop-update-files-*` 到 `desktop-assets/`（保留每个 artifact 的目录名）。
- 按以下映射组装 staging：

```text
staging/<version>/android/<apk>
staging/<version>/server/<backend archives>
staging/<version>/darwin/arm64/<files from mac-arm64>
staging/<version>/darwin/x64/<files from mac-x64>
staging/<version>/win32/x64/<files from win-x64>
staging/<version>/linux/x64/<files from linux-x64>
staging/<version>/SHA256SUMS.txt
```

- 在 staging 根目录生成校验和，路径用相对路径。

- [ ] **Step 3: 上传 COS 并写 `RELEASE.txt`**

- 安装 `coscmd`（`python3 -m pip install --user coscmd`，把 `~/.local/bin` 加进 `PATH`）。
- 仅当 `startsWith(github.ref, 'refs/tags/')` 时执行：

```bash
UPDATE_RELEASE_FILE=1 RELEASE_PREFIX=application \
  bash scripts/release/publish-cos.sh "staging/$VERSION" "$VERSION"
```

- `COS_SECRET_ID` / `COS_SECRET_KEY` 引用 Organization secrets；`COS_BUCKET=leros-1395325824`、`COS_REGION=ap-beijing`、`RELEASE_PREFIX=application` 写在 workflow `env`。
- `workflow_dispatch` 不运行 `publish` job（保留现有 tag 条件），只保留构建 artifact。
- GitHub Release 创建/追加步骤保持现有行为。

- [ ] **Step 4: 处理外部下载页兼容（如需要）**

确认官网或外部下载页是否仍读 `application/LATEST.txt` / 根目录安装包。若有引用，在 `publish` job 额外上传一个 `LATEST.txt`（内容与 `RELEASE.txt` 相同）作为临时别名；确认无人读取后删除该步骤。不要同时维护两套安装包路径。

- [ ] **Step 5: 更新发布文档**

`docs/operations/release.md`：

- 发布表增加 COS 目录说明；删掉“桌面端由独立 workflow 发布”的段落。
- 说明只有 tag 发布写 COS / `RELEASE.txt`，`workflow_dispatch` 只出 artifact。
- 说明 `RELEASE.txt` 只由 `publish` job 写，稳定版本才推进。

`docs/README.md` 增加本计划的索引行。

- [ ] **Step 6: 验证 workflow**

Run: `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/release.yml`
Expected: 无输出（退出码 0）。

同时确认：

```bash
test ! -e .github/workflows/desktop-release.yml
grep -q "RELEASE.txt" docs/operations/release.md
```

- [ ] **Step 7: 提交**

```bash
git add .github/workflows/release.yml docs/operations/release.md docs/README.md \
  docs/design/2026-11-16-cos-unified-release-and-mobile-update-design.md \
  docs/design/2026-11-16-cos-unified-release-and-mobile-update-plan.md
git rm .github/workflows/desktop-release.yml
git commit -m "feat(ci): 统一发布桌面端与移动端到 COS"
```

---

## 收尾验证

- [ ] `node --test scripts/release/version.test.mjs`
- [ ] `bash scripts/release/publish-cos.test.sh`
- [ ] `pnpm --filter @lework/mobile test && pnpm --filter @lework/mobile typecheck`
- [ ] `pnpm --filter @lework/desktop test && pnpm --filter @lework/desktop typecheck`
- [ ] `cd frontend && pnpm typecheck`
- [ ] `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/release.yml`
- [ ] 用测试前缀跑一次 `publish-cos.sh` dry-run，确认目录与 `RELEASE.txt` 路径正确

    ```bash
    mkdir -p /tmp/lework-release/0.8.0/android
    touch /tmp/lework-release/0.8.0/android/lework-android-0.8.0.apk
    DRY_RUN=1 UPDATE_RELEASE_FILE=1 RELEASE_TXT_EXISTING=0.7.9 \
      bash scripts/release/publish-cos.sh /tmp/lework-release/0.8.0 0.8.0
    ```

- [ ] 发版前对上一版与新版 APK 执行 `apksigner verify --print-certs`，确认签名证书一致
- [ ] 真机完成一次「旧版 → 提示 → 下载 → 系统安装器 → 新版」全流程
