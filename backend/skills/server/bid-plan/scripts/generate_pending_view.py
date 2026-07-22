#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
from pathlib import Path


def escape(value: object) -> str:
    return str(value if value is not None else "").replace("|", "\\|").replace("\n", "<br>")


def main() -> int:
    parser = argparse.ArgumentParser(description="根据 pending.json 生成待补充清单 Markdown")
    parser.add_argument("--project-root", required=True, type=Path)
    args = parser.parse_args()
    root = args.project_root.resolve()
    payload = json.loads((root / "work/项目/pending.json").read_text(encoding="utf-8"))
    lines = [
        "# 待补充清单",
        "",
        "| 编号 | 待补事项 | 类型 | 目标位置 | 影响 | 提交前必须补齐 | 状态 |",
        "|---|---|---|---|---|---|---|",
    ]
    for item in payload.get("items", []):
        lines.append("| " + " | ".join([
            escape(item.get("pending_id")),
            escape(item.get("item")),
            escape(item.get("type")),
            escape(", ".join(item.get("target_ids", []))),
            escape(item.get("impact")),
            "是" if item.get("must_resolve_before_submission") else "否",
            escape(item.get("status")),
        ]) + " |")
    if not payload.get("items"):
        lines.append("| — | 当前无待补事项 | — | — | — | 否 | resolved |")
    output = root / "work/项目/待补充清单.md"
    output.write_text("\n".join(lines) + "\n", encoding="utf-8")
    print(output)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
