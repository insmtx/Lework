#!/usr/bin/env python3
"""使用 LibreOffice 将可编辑办公文档转换为 DOCX。

转换后自动修复 WPS .doc 产生的 w:sym 符号编码问题：
WPS 生成的 .doc 经 LibreOffice 转换后，部分中文会以 <w:sym> 而非
<w:t> 存储，导致 python-docx 等工具无法读取内容。本脚本在转换完成后
自动将所有 w:sym 解码为 w:t。
"""
from __future__ import annotations

import argparse
import shutil
import subprocess
from pathlib import Path

from fix_wsym_encoding import fix_wsym


def convert_file(source: Path, target: Path) -> str:
    source = source.resolve()
    target = target.resolve()
    if not source.is_file():
        raise FileNotFoundError(source)
    if target.exists():
        raise FileExistsError(f"输出已存在，拒绝覆盖: {target}")
    executable = shutil.which("libreoffice") or shutil.which("soffice")
    if not executable:
        raise RuntimeError("未找到 LibreOffice/soffice，无法转换 .doc/.rtf/.odt")
    target.parent.mkdir(parents=True, exist_ok=True)
    result = subprocess.run(
        [executable, "--headless", "--convert-to", "docx", "--outdir", str(target.parent), str(source)],
        check=False,
        text=True,
        capture_output=True,
    )
    generated = target.parent / f"{source.stem}.docx"
    if result.returncode != 0 or not generated.is_file():
        detail = (result.stderr or result.stdout or "转换器未生成 DOCX").strip()
        raise RuntimeError(f"LibreOffice 转换失败: {detail}")
    if generated != target:
        generated.replace(target)
    # 自动修复 WPS/LibreOffice 转换产生的 w:sym 编码问题
    sym_result = fix_wsym(target)
    if sym_result["fixed"]:
        print(f"  [wsym-fix] {sym_result['message']}")
    return executable


def main() -> int:
    parser = argparse.ArgumentParser(description="将 .doc、.rtf 或 .odt 转换为 DOCX")
    parser.add_argument("--input", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    executable = convert_file(args.input, args.output)
    print(f"OK: {args.input} -> {args.output} ({executable})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
