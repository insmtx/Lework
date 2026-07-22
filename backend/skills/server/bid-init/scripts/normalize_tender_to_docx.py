#!/usr/bin/env python3
"""将冻结的招标来源转换或规范化为工作区内的 DOCX。"""
from __future__ import annotations

import argparse
import json
import shutil
import tempfile
from datetime import datetime, timezone
from pathlib import Path

from convert_doc_to_docx import convert_file

try:
    import fitz
except ImportError:  # pragma: no cover
    fitz = None


OFFICE_EXTENSIONS = {".doc", ".rtf", ".odt"}


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


def under(root: Path, raw: str, label: str) -> Path:
    value = Path(raw)
    path = value.resolve() if value.is_absolute() else (root / value).resolve()
    try:
        path.relative_to(root)
    except ValueError as exc:
        raise ValueError(f"{label} 必须位于项目目录内: {path}") from exc
    return path


def tender_source(root: Path, raw: str) -> Path:
    source = under(root, raw, "来源文件")
    try:
        source.relative_to((root / "input").resolve())
    except ValueError as exc:
        raise ValueError("待转换来源必须来自已冻结的 input/ 目录") from exc
    if not source.is_file():
        raise FileNotFoundError(source)
    return source


def target_path(root: Path, source: Path, raw: str | None) -> Path:
    default = Path("work/招标/normalized") / f"{source.stem}.docx"
    target = under(root, raw or str(default), "规范 DOCX 输出")
    normalized = (root / "work" / "招标" / "normalized").resolve()
    try:
        target.relative_to(normalized)
    except ValueError as exc:
        raise ValueError("规范 DOCX 输出必须位于 work/招标/normalized/") from exc
    if target.exists():
        raise FileExistsError(f"输出已存在，拒绝覆盖: {target}")
    return target


def convert_office(source: Path, target: Path) -> tuple[str, str]:
    executable = convert_file(source, target)
    return f"libreoffice:{Path(executable).name}", "版式需在 Word/渲染检查中复核"


def rtf_escape(text: str) -> str:
    """以 RTF Unicode 转义保存 PDF 提取出的纯文本。"""
    pieces: list[str] = []
    for character in text:
        if character in {"\\", "{", "}"}:
            pieces.append("\\" + character)
        elif character == "\t":
            pieces.append("\\tab ")
        elif ord(character) <= 127:
            pieces.append(character)
        else:
            codepoint = ord(character)
            if codepoint > 32767:
                codepoint -= 65536
            pieces.append(f"\\u{codepoint}?")
    return "".join(pieces)


def pdf_text_to_rtf(pdf) -> str:
    """生成仅承载阅读文本的临时 RTF；原 PDF 版式不在此处复刻。"""
    body: list[str] = [r"{\rtf1\ansi\deff0{\fonttbl{\f0 Arial;}}\uc1\pard\f0\fs22 "]
    for page_index, page in enumerate(pdf):
        blocks = page.get_text("blocks")
        for block in sorted(blocks, key=lambda item: (item[1], item[0])):
            text = str(block[4]).strip()
            if text:
                body.append(rtf_escape(text).replace("\n", r"\par "))
                body.append(r"\par ")
        if page_index < len(pdf) - 1:
            body.append(r"\page ")
    body.append("}")
    return "".join(body)


def convert_pdf_text(source: Path, target: Path) -> tuple[str, str]:
    if fitz is None:
        raise RuntimeError("缺少 PyMuPDF，无法从 PDF 生成可读 DOCX")
    with fitz.open(source) as pdf, tempfile.TemporaryDirectory() as temporary:
        temporary_rtf = Path(temporary) / f"{source.stem}.rtf"
        temporary_rtf.write_text(pdf_text_to_rtf(pdf), encoding="ascii", errors="strict")
        executable = convert_file(temporary_rtf, target)
    return f"pymupdf-text+libreoffice:{Path(executable).name}", "仅保证可读文本与分页，不保留 PDF 原始表格/版式；不得用作格式模板提取来源"


def update_input_manifest(root: Path, entry: dict) -> Path:
    """把规范化结果合并回冻结输入清单，不生成单独的转换记录 JSON。"""
    path = root / "work" / "招标" / "input-manifest.json"
    if not path.is_file():
        raise FileNotFoundError(f"缺少输入冻结清单: {path}")
    payload = json.loads(path.read_text(encoding="utf-8"))
    matched = False
    for item in payload.get("files", []):
        if isinstance(item, dict) and item.get("stored_path") == entry["source"]:
            item.update(
                {
                    "normalized_docx": entry["normalized_docx"],
                    "normalization_engine": entry["engine"],
                    "normalization_note": entry["note"],
                    "normalized_at": entry["converted_at"],
                    "template_eligible": entry["template_eligible"],
                }
            )
            matched = True
            break
    if not matched:
        raise ValueError(f"来源未出现在输入冻结清单中: {entry['source']}")
    payload["updated_at"] = now_iso()
    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return path


def main() -> int:
    parser = argparse.ArgumentParser(description="将冻结的招标来源转换为工作区规范 DOCX")
    parser.add_argument("--project-root", required=True, type=Path)
    parser.add_argument("--input", required=True, help="input/ 下的招标来源，相对项目根的路径")
    parser.add_argument("--output", help="输出相对路径，默认 work/招标/normalized/<来源名>.docx")
    args = parser.parse_args()

    root = args.project_root.resolve()
    source = tender_source(root, args.input)
    target = target_path(root, source, args.output)
    extension = source.suffix.lower()
    if extension == ".docx":
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, target)
        engine, note = "copy-docx", "保留原 DOCX 包；仍须在模板阶段检查边界与版式"
        template_eligible = True
    elif extension in OFFICE_EXTENSIONS:
        engine, note = convert_office(source, target)
        template_eligible = True
    elif extension == ".pdf":
        engine, note = convert_pdf_text(source, target)
        template_eligible = False
    else:
        raise ValueError(f"不支持转换为 DOCX 的招标来源类型: {extension or '<无扩展名>'}")

    entry = {
        "source": str(source.relative_to(root)),
        "normalized_docx": str(target.relative_to(root)),
        "source_extension": extension,
        "engine": engine,
        "template_eligible": template_eligible,
        "note": note,
        "converted_at": now_iso(),
    }
    manifest = update_input_manifest(root, entry)
    print(json.dumps({"item": entry, "manifest": str(manifest.relative_to(root))}, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
