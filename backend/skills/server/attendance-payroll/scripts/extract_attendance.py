#!/usr/bin/env python3
"""把考勤 PDF/图片整理成按页 PNG，供当前多模态对话直接识图。

不调用任何模型、不读取 API Key。识别由正在跑这次任务的多模态模型完成。
"""

from __future__ import annotations

import argparse
import json
import shutil
import sys
from pathlib import Path

ERROR_INPUT = 2
MAX_PDF_PAGES = 50
IMAGE_SUFFIXES = {".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp"}


def fail(code: int, message: str) -> None:
    print(message, file=sys.stderr)
    raise SystemExit(code)


def prepare_pages(sources: list[Path], output_dir: Path) -> list[str]:
    output_dir.mkdir(parents=True, exist_ok=True)
    pages: list[str] = []
    for index, source in enumerate(sources, 1):
        if not source.is_file():
            fail(ERROR_INPUT, f"attendance file not found: {source}")
        suffix = source.suffix.lower()
        if suffix == ".pdf":
            from pdf_prepare import render_pdf

            page_dir = output_dir / f"doc-{index}"
            page_dir.mkdir(parents=True, exist_ok=True)
            rendered = render_pdf(str(source), str(page_dir), MAX_PDF_PAGES)
            pages.extend(rendered)
            continue
        if suffix in IMAGE_SUFFIXES:
            target = output_dir / f"doc-{index}{suffix}"
            shutil.copy2(source, target)
            pages.append(str(target))
            continue
        fail(ERROR_INPUT, f"unsupported attendance file type: {suffix or source.name}")
    if not pages:
        fail(ERROR_INPUT, "no attendance pages were produced")
    return pages


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Render attendance PDF/images into page PNGs for the current multimodal model.",
    )
    parser.add_argument("--input", nargs="+", required=True, help="Attendance PDF or image paths")
    parser.add_argument("--output-dir", required=True, help="Directory to write page images")
    args = parser.parse_args()
    sources = [Path(raw).expanduser().resolve() for raw in args.input]
    output_dir = Path(args.output_dir).expanduser().resolve()
    pages = prepare_pages(sources, output_dir)
    print(json.dumps({"ok": True, "pages": pages, "count": len(pages)}, ensure_ascii=False))


if __name__ == "__main__":
    sys.path.insert(0, str(Path(__file__).resolve().parent))
    main()
