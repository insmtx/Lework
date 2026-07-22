#!/usr/bin/env python3
"""从冻结的 DOCX 来源中提取连续正文范围作为可填写底板。"""
from __future__ import annotations

import argparse
import copy
import json
import tempfile
import zipfile
from datetime import datetime, timezone
from pathlib import Path

try:
    from lxml import etree
except ImportError as exc:  # pragma: no cover
    raise SystemExit("需要 lxml：请先安装 skills/bid-template/requirements.txt 中的依赖") from exc


W_NS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
NS = {"w": W_NS}
SECT_PR = f"{{{W_NS}}}sectPr"


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


def under(root: Path, raw: str, label: str) -> Path:
    path = Path(raw)
    resolved = path.resolve() if path.is_absolute() else (root / path).resolve()
    try:
        resolved.relative_to(root)
    except ValueError as exc:
        raise ValueError(f"{label} 必须位于项目目录内: {resolved}") from exc
    return resolved


def source_path(root: Path, raw: str) -> Path:
    source = under(root, raw, "来源文件")
    try:
        source.relative_to((root / "input").resolve())
    except ValueError as exc:
        raise ValueError("来源文件必须是已冻结的 input/ 内 DOCX") from exc
    if not source.is_file() or not zipfile.is_zipfile(source):
        raise ValueError(f"来源文件不是可读取的 DOCX: {source}")
    return source


def output_path(root: Path, raw: str | None) -> Path:
    target = under(root, raw or "work/投标文件/模板/可填写底板.docx", "输出文件")
    template_dir = (root / "work" / "投标文件" / "模板").resolve()
    try:
        target.relative_to(template_dir)
    except ValueError as exc:
        raise ValueError("输出文件必须位于 work/投标文件/模板/") from exc
    if target.exists():
        raise FileExistsError(f"输出文件已存在，拒绝覆盖: {target}")
    return target


def body_children(source_xml: bytes) -> list[tuple[int, str, str]]:
    parser = etree.XMLParser(remove_blank_text=False, recover=False)
    root = etree.fromstring(source_xml, parser)
    body = root.find(".//w:body", namespaces=NS)
    if body is None:
        raise RuntimeError("DOCX 缺少 word/document.xml 中的 w:body")
    rows: list[tuple[int, str, str]] = []
    for index, child in enumerate(list(body)):
        kind = etree.QName(child).localname
        text = " ".join(part.strip() for part in child.xpath(".//w:t/text()", namespaces=NS) if part.strip())
        rows.append((index, kind, text[:160]))
    return rows


def extract_document_xml(source_xml: bytes, start: int, end: int) -> bytes:
    parser = etree.XMLParser(remove_blank_text=False, recover=False)
    root = etree.fromstring(source_xml, parser)
    body = root.find(".//w:body", namespaces=NS)
    if body is None:
        raise RuntimeError("DOCX 缺少 word/document.xml 中的 w:body")
    children = list(body)
    if start < 0 or end <= start or end > len(children):
        raise ValueError(f"正文范围无效: {start}:{end}，可用对象数为 {len(children)}")

    selected = [copy.deepcopy(node) for node in children[start:end]]
    for child in list(body):
        body.remove(child)

    selected_section_properties = [node for node in selected if node.tag == SECT_PR]
    for node in selected:
        if node.tag != SECT_PR:
            body.append(node)
    if selected_section_properties:
        body.append(selected_section_properties[-1])
    else:
        original_section_properties = next((node for node in reversed(children) if node.tag == SECT_PR), None)
        if original_section_properties is None:
            raise RuntimeError("DOCX 缺少正文级节属性")
        body.append(copy.deepcopy(original_section_properties))
    return etree.tostring(root, xml_declaration=True, encoding="UTF-8", standalone=False)


def extract(source: Path, target: Path, start: int, end: int) -> None:
    with tempfile.TemporaryDirectory() as temporary:
        temporary_root = Path(temporary)
        with zipfile.ZipFile(source, "r") as archive:
            archive.extractall(temporary_root)
        document_xml = temporary_root / "word" / "document.xml"
        if not document_xml.is_file():
            raise RuntimeError("DOCX 缺少 word/document.xml")
        document_xml.write_bytes(extract_document_xml(document_xml.read_bytes(), start, end))
        target.parent.mkdir(parents=True, exist_ok=True)
        with zipfile.ZipFile(target, "w", compression=zipfile.ZIP_DEFLATED) as archive:
            for file in sorted(temporary_root.rglob("*")):
                if file.is_file():
                    archive.write(file, file.relative_to(temporary_root))


def main() -> int:
    parser = argparse.ArgumentParser(description="从已冻结 DOCX 中提取连续正文范围为可填写底板")
    parser.add_argument("--project-root", required=True, type=Path)
    parser.add_argument("--input", required=True, help="项目 input/ 下的来源 DOCX，相对项目根的路径")
    parser.add_argument("--from", dest="start", type=int, help="正文对象起始索引，包含")
    parser.add_argument("--to", dest="end", type=int, help="正文对象结束索引，不包含")
    parser.add_argument("--output", help="项目内输出路径，默认 work/投标文件/模板/可填写底板.docx")
    parser.add_argument("--list-body", action="store_true", help="列出正文对象索引，供人工确认提取范围")
    args = parser.parse_args()

    root = args.project_root.resolve()
    source = source_path(root, args.input)
    with zipfile.ZipFile(source, "r") as archive:
        source_xml = archive.read("word/document.xml")

    if args.list_body:
        if args.start is not None or args.end is not None or args.output:
            parser.error("--list-body 不能与提取参数同时使用")
        print("索引\t类型\t文本摘要")
        for index, kind, text in body_children(source_xml):
            print(f"{index}\t{kind}\t{text}")
        return 0

    if args.start is None or args.end is None:
        parser.error("提取时必须同时提供 --from 和 --to")
    target = output_path(root, args.output)
    extract(source, target, args.start, args.end)
    record = {
        "schema_version": "bid-template-extraction/v1",
        "extracted_at": now_iso(),
        "source": str(source.relative_to(root)),
        "output": str(target.relative_to(root)),
        "body_range": {"from": args.start, "to": args.end},
        "note": "仅记录提取范围；项目填写说明仍是可编辑目标与保护范围的权威来源。",
    }
    print(json.dumps(record, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
