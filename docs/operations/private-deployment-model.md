# 私有化模型部署（vLLM + Qwen3.6-27B）

本文档说明私有化场景下大模型的本地部署方案。私有化要求模型数据不出内网，采用 **vLLM + NVIDIA GPU** 承载 `Qwen3.6-27B`，对外暴露 OpenAI 兼容接口，由 Leros 的 `llm.baseUrl` 指向该服务。本文以 `Qwen3.6-27B` 为例。

## 1. 部署形态

```
Leros Server / Worker
        │ llm.provider=openai, llm.baseUrl=http://vllm:8080/v1, llm.model=Qwen3.6-27B
        ▼
   vLLM (OpenAI 兼容 API, 端口 8080)
        │
        └─ Qwen3.6-27B 权重（内网私有化）
```

- vLLM 提供 `/v1/chat/completions` 等 OpenAI 兼容接口，Leros 直接以 `openai` provider 对接。
- 模型权重与推理服务完全在内网，数据不下外网。

## 2. 硬件要求与并发权衡

`Qwen/Qwen3.6-27B`（ModelScope）BF16 权重约 **51.1 GiB**（checkpoint 51.75 GiB，15 个 safetensors 文件，架构 `Qwen3_5ForConditionalGeneration`）。

### 2.1 单卡 A100-80G 实测基线

以下来自单卡 A100-80G 的实际部署日志（vLLM 0.22.1）：

| 项 | 实测值 |
|----|--------|
| 模型加载占用 | 51.1 GiB |
| 可用 KV Cache | 19.15 GiB |
| KV Cache 容量 | 301,314 tokens |
| `max_model_len`（131072 / 128K）时 | **最大并发 2.30×** |

> 单张 **80 GB 卡即可承载该模型**（BF16 全精度，`gpu_memory_utilization 0.925`），但显存大头被模型权重占去，KV Cache 有限。**在给定显存下，上下文越长则并发越低**——128K 只剩约 2.3 路并发。

### 2.2 上下文 × 并发权衡

KV Cache 是提高并发的关键，而它受限于"总显存 − 模型权重 − 开销"。**降低上下文可释放出更多 KV Cache 以提升并发**：

| 上下文 | 说明 |
|--------|------|
| 128K（131072） | 单卡 A100-80G 下最大并发仅约 2.3× |
| 64K | 单卡并发约提升 1 倍左右，仍偏少 |
| 32K / 16K | 可换得更明显并发提升，适合多数业务（长文档、代码) |
| 8K 或更低 | 单卡并发最高，适合高并发短对话场景 |

> 建议：**先按业务实际需要的最大输入长度定上下文**，不要盲目开满 128K。并发优先时降低上下文，以 KV Cache 换并发。`max_model_len`（vLLM）与 `llm.limit.context`（Leros）需一致。

### 2.3 显卡配置（多卡换取更高并发）

多卡（张量并行）除降低单卡显存压力外，还能扩展总 KV Cache 以支撑更高并发。显卡数量须为 2 的幂（`--tensor-parallel-size`）；可用如下梯度：

| 配置 | 总显存 | 用途 |
|------|:---:|------|
| 1 × 80 GB（A100/A800-80G / H100） | 80 GB | 单点，并发受限（128K→2.3×），适合低并发私有化 |
| 2 × 48 GB（RTX PRO 5000） | 96 GB | 双卡，可支撑较小上下文 |
| 4 × 32 GB（RTX 5090） | 128 GB | 低成本高显存方案；消费级卡无 NVLink，多卡走 PCIe |
| 2 × 80 GB（A100/A800-80G / H100） | 160 GB | 双倍 KV Cache，并发与上下文兼顾 |
| 2 × 96 GB（RTX PRO 6000） | 192 GB | 高并发推荐（单机两张专业卡） |
| 4 × 48 GB（RTX PRO 5000） | 192 GB | 高并发 + 长上下文 |
| 4 × 80 GB（H100 / A100-80G） | 320 GB | 高并发 + 长上下文 |

> 5090 为**消费级**显卡，无 NVLink，仅支持 PCIe 互联，多卡张量并行（`--tensor-parallel-size` 须为 2 的幂，如 4/8）性能弱于数据中心卡（H100/A100 的 NVLink），适合追求低成本、对并发/长文要求不高的场景。

> **不做量化**，均以 BF16 全精度保证质量。显卡数量须为 2 的幂（1 / 2 / 4 / 8）——张量并行度必须为 2 的幂；单卡（tensor-parallel-size 1）为特殊形态，可用于低并发场景。多卡并行下即使个别卡故障，vLLM 也可退化为剩余卡以更小上下文继续服务。卡间建议 `NVLink`/密集互联；无 NVLink 用 PCIe 则并发与长文性能下降。

### 2.4 非 GPU 资源

| 项 | 最小 | 推荐 |
|----|------|------|
| CPU | 8 核 | 16 核 |
| 内存 | 32 GB | 64 GB（含 KV Cache 预留） |
| 磁盘 | 系统 100 GB + 权重 60 GB（SSD） | 同左 |
| 网络 | 千兆（单机） | 多卡并行时卡间高带宽互联 |

## 3. 部署方式

vLLM 可独立部署于 k3s 节点、独立 GPU 服务器，或作为 k3s 内的一个 Deployment（需节点暴露 GPU 与 NVMe 可用内存）。以下以独立 GPU 服务器 + Docker 为例，也可在 k3s 节点上用 `nvidia` runtime 部署。

> 若复用 k3s 节点，需安装 NVIDIA `nvidia-container-toolkit` 并配置 `nvidia` runtime / device plugin，且该节点不会被 Leros 调度器用作普通 worker（模型与任务混部需评估显存与性能）。

### 3.1 拉取模型权重

该模型为 `Qwen/Qwen3.6-27B`（ModelScope，架构 `Qwen3_5ForConditionalGeneration`），权重约 55.6 GB。下载到 GPU 机器的权重目录：

```bash
# ModelScope（推荐，国际/国内均可直连）
pip install modelscope
modelscope download --model Qwen/Qwen3.6-27B \
  --local_dir ~/models/Qwen3.6-27B

# HuggingFace 镜像
HF_ENDPOINT=https://hf-mirror.com \
  huggingface-cli download Qwen/Qwen3.6-27B \
  --local-dir ~/models/Qwen3.6-27B
```

> 私有化首次需在可联网环境准备好权重，再同步至内网 GPU 机器，避免模型数据外泄。ModelScope 的模型目录/命名空间需与 `--model` 指向一致。

### 3.2 启动 vLLM（OpenAI 兼容）

**单卡 A100-80G 实测参数**（直接可用的参考命令）：

```bash
vllm serve /model/Qwen3___6-27B/ \
  --tensor-parallel-size 1 \
  --trust-remote-code \
  --served-model-name Qwen3.6-27B \
  --gpu-memory-utilization 0.925 \
  --default-chat-template-kwargs '{"enable_thinking": false}' \
  --max-model-len=65536 \
  --enable-prefix-caching \
  --enable-auto-tool-choice \
  --tool-call-parser qwen3_coder \
  --port 8080 \
  --dtype bfloat16
```

容器化方式（等价，Docker 映射）：

```bash
docker run -d --name vllm-qwen27b \
  --runtime nvidia \
  -e NVIDIA_VISIBLE_DEVICES=0 \
  -e VLLM_WORKER_MULTIPROC_METHOD=spawn \
  -v ~/models:/models \
  -p 8080:8080 \
  vllm/vllm-openai:latest \
  --model /models/Qwen3.6-27B \
  --tensor-parallel-size 1 \
  --trust-remote-code \
  --served-model-name Qwen3.6-27B \
  --gpu-memory-utilization 0.925 \
  --default-chat-template-kwargs '{"enable_thinking": false}' \
  --max-model-len 65536 \
  --enable-prefix-caching \
  --enable-auto-tool-choice \
  --tool-call-parser qwen3_coder \
  --dtype bfloat16 \
  --port 8080
```

**并发优先（推荐）**：在同样单卡 80 GB 下降低 `--max-model-len` 换取更多 KV Cache/并发：

```bash
  --max-model-len 32768     # 32K：并发较 64K 显著提升
  # 或
  --max-model-len 16384     # 16K：更高并发，适合短对话/高并发业务
```

> `--port 8080` 为 vLLM API 端口；映射到宿主机时 `-p 0.0.0.0:<映射端口>:8080`。健康检查用 `curl http://<host>:<映射端口>/health`。

关键参数：

| 参数 | 说明 |
|------|------|
| `--tensor-parallel-size 1` | 并行度，须为 2 的幂（1/2/4/8），等于卡数 |
| `--trust-remote-code` | 允许加载模型自定义代码 |
| `--served-model-name Qwen3.6-27B` | 对外注册的模型名，应与 Leros `llm.model` 一致 |
| `--gpu-memory-utilization 0.925` | GPU 显存占用比例（实测 0.925 可用） |
| `--default-chat-template-kwargs '{"enable_thinking": false}'` | 关闭模型思考模式（thinking），加快响应并省显存 |
| `--max-model-len` | 上下文长度，越大并发越低；并发优先可降到 32K/16K |
| `--enable-prefix-caching` | 前缀缓存，降低重复前缀计算、提升有效吞吐 |
| `--enable-auto-tool-choice` | 启用自动工具选择（函数调用） |
| `--tool-call-parser qwen3_coder` | 工具调用解析器（Qwen 系列） |
| `--port 8080` | vLLM API 端口（默认 8000，此处分端口 8080） |
| `--dtype bfloat16` | 全精度 BF16，不做量化 |

健康检查：

```bash
curl http://<vllm-host>:8080/health
curl http://<vllm-host>:8080/v1/models
```
应返回含 `Qwen3.6-27B` 的模型列表。

### 3.3 接入 Leros

vLLM 无鉴权时，`llm.apiKey` 填任意非空占位串即可。在 `my-values.yaml` 中：

```yaml
llm:
  provider: openai
  model: Qwen3.6-27B           # 与 --served-model-name 一致
  baseUrl: "http://<vllm-host>:8080/v1"
  apiKey: "not-needed"
  # 上下文与 vLLM --max-model-len 一致；并发优先时降低（同 2.2 表）
  limit:
    context: 65536           # 64K，按需调整，如 32768 / 16384
    output: 8192             # 单次输出上限，按任务规模调整；越大越占 KV Cache
```

## 4. 多模态 / 视觉

Leros `llm.vision` 指示默认模型是否支持图片输入。Qwen3.6-27B（文本版）不支持视觉，需视觉能力时应改用 Qwen3.6-VL 系列并在 `llm.vision: true`，相应调整权重与镜像。

## 5. 备选与扩展

| 场景 | 方案 |
|------|------|
| 无 GPU / 仅连通性验证 | vLLM CPU 模式或 Ollama（性能很低，仅验证 `llm` 配置连通，且需足够大内存承载权重 51.1 GiB） |
| 统一模型网关 | 在 Leros 与 vLLM 之间加 ModelRouter/网关，统一多模型路由与熔断 |
| 视觉/多模态 | 改 `Qwen3.6-VL`，`llm.vision: true` |

## 6. 核对清单

- [ ] 权重已准备并只在内网传播
- [ ] vLLM `--served-model-name` 与 `llm.model` 一致
- [ ] `llm.baseUrl` 指向 vLLM `:8080/v1`，内网可访问
- [ ] `curl :8080/v1/models` 返回预期模型
- [ ] `--max-model-len` 与 `llm.limit.context` 一致；上下文与并发已按业务权衡
- [ ] 发起一次 Leros 任务，确认走本地模型成功
