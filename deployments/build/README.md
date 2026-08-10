# 构建说明

本目录存放 Leros 各组件的 Dockerfile。worker-base 镜像支持两个版次，方便 SaaS 精简部署与私有化全量部署共用同一份 Dockerfile。

## Dockerfile 列表

| Dockerfile | 作用 |
|---|---|
| `Dockerfile.base` | 通用 base 镜像（leros 服务进程的最小运行时） |
| `Dockerfile.leros` | leros 服务器镜像 |
| `Dockerfile.leros-dev` | 开发版 leros（含调试工具） |
| `Dockerfile.web` | 前端镜像 |
| `Dockerfile.worker` | worker 镜像（FROM `leros-worker-base`） |
| `Dockerfile.worker-base` | worker 基础镜像：Ubuntu + LibreOffice + 字体 + Python/Node 工具链，详见下方 |

## worker-base 两个版次

通过顶层 `ARG EDITION` 切换，默认 `saas`。

| 组件 | saas（SaaS 精简版，默认） | private（私有化完整版） |
|---|:---:|:---:|
| Ubuntu 24.04 + apt 基础包 | ✅ | ✅ |
| LibreOffice（apt 稳定版） | ✅ | ✅ |
| CJK 字体 + Windows 字体别名表 | ✅ | ✅ |
| Python 3.12 + pip 镜像 | ✅ | ✅ |
| Node.js 22.14 + claude-code/codex/opencode | ✅ | ✅ |
| Python 文档库（python-docx、lxml、openpyxl、xlsxwriter、pandas、python-pptx、PyMuPDF、pypdf、pdfplumber、pdfminer.six、reportlab、Pillow） | ✅ | ✅ |
| Node 文档组件（docx、PptxGenJS、pdf-lib、sharp） | ✅ | ✅ |
| TeX Live（XeLaTeX/LuaLaTeX/pdfLaTeX + 中文字体） | ❌ | ✅ |
| Poppler、Ghostscript、Pandoc | ❌ | ✅ |
| ImageMagick、Inkscape、Matplotlib | ❌ | ✅ |
| FFmpeg | ❌ | ✅ |
| Tesseract OCR（eng + chi_sim + osd） | ❌ | ✅ |
| Playwright + Chromium | ❌ | ✅ |

> SaaS 精简版的选择依据：上述重型组件（TeX Live ~1.5–2GB、Playwright Chromium ~300MB、FFmpeg ~200MB 等）对 SaaS 投标/Excel/Word 主线非必需；SaaS 优先镜像小、启动快。私有化客户常需要自由生成 PDF（含中文字体排版）、识别扫描件，因此走 private。

### 构建命令

```bash
# SaaS 精简版（默认）：tag -> :saas
make docker-build-worker-base

# 私有化完整版：tag -> :private
make docker-build-worker-base-private

# 推送镜像
make docker-push-worker-base           # saas -> :saas
make docker-push-worker-base-private   # private -> :private

# 也可显式指定版本：
make docker-build-worker-base WORKER_BASE_EDITION=private
```

> **不打 `:latest` 别名**：saas/private 都只按版次打 tag，避免仓库里同一 tag 指向不同内容造成歧义。
> `Dockerfile.worker` 默认 `FROM ...leros-worker-base:saas`；私有化部署需改 `WORKER_BASE_IMAGE=...:private`。

### 版本管理

所有 worker-base 顶层 ARG 默认值、Python 库、Node 组件的版本号集中记录在 `worker-base.versions.txt`。
修改版本时请同步更新：

1. `Dockerfile.worker-base` 顶部的 `ARG` 默认值
2. `worker-base.versions.txt` 中的版本号

### worker 镜像如何引用 base

`Dockerfile.worker` 默认 `FROM registry.yygu.cn/insmtx/leros-worker-base:private`（通过 `ARG WORKER_BASE_IMAGE`，构建时 `--build-arg WORKER_BASE_IMAGE=...:saas` 切换）。
SaaS 部署构建 worker 时：

```bash
make docker-build-worker WORKER_BASE_IMAGE=registry.yygu.cn/insmtx/leros-worker-base:saas
```
