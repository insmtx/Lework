#!/usr/bin/env python3
"""仅在独立终审通过后发布唯一正式交付物。"""
from __future__ import annotations

import argparse
import json
import shutil
import tempfile
from pathlib import Path


def project_root(value: str) -> Path:
    root = Path(value).expanduser().resolve()
    project = root / "work" / "项目" / "project.json"
    if not project.is_file():
        raise FileNotFoundError(f"缺少项目身份文件: {project}")
    payload = json.loads(project.read_text(encoding="utf-8"))
    if Path(payload.get("project_root", "")).resolve() != root:
        raise ValueError("project.json 与指定项目根不一致")
    return root


def publish(root: Path) -> None:
    review = root / "work" / "独立复核" / "最终审查报告.md"
    candidate = root / "work" / "投标文件" / "候选投标文件.docx"
    pending = root / "work" / "项目" / "待补充清单.md"
    if not review.is_file() or "审查结论：通过" not in review.read_text(encoding="utf-8"):
        raise ValueError("最终审查报告没有通过结论，不能发布")
    if not candidate.is_file() or not pending.is_file():
        raise FileNotFoundError("缺少候选 DOCX 或待补充清单")

    project = json.loads((root / "work" / "项目" / "project.json").read_text(encoding="utf-8"))
    output = root / "output"
    output.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="publish-", dir=root / "work" / ".tmp") as temporary:
        staging = Path(temporary)
        docx = staging / f"{project.get('project_name') or '投标项目'}-投标文件.docx"
        checklist = staging / "待补充清单.md"
        shutil.copy2(candidate, docx)
        shutil.copy2(pending, checklist)
        for old in output.iterdir():
            if old.is_file():
                old.unlink()
        shutil.move(str(docx), output / docx.name)
        shutil.move(str(checklist), output / checklist.name)


def main() -> int:
    parser = argparse.ArgumentParser(description="发布已通过独立终审的投标文件")
    parser.add_argument("--project-root", required=True)
    args = parser.parse_args()
    try:
        publish(project_root(args.project_root))
        return 0
    except Exception as exc:
        print(f"ERROR: {exc}")
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
