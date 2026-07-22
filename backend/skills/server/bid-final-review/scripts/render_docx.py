#!/usr/bin/env python3
"""Render DOCX to PDF/PNG with Word-native preference and isolated LibreOffice fallback."""
from __future__ import annotations

import argparse
import platform
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

import fitz



def project_root_for_output(path: Path) -> Path:
    target = path.expanduser().resolve()
    for candidate in [target, *target.parents]:
        if (candidate / "workspace.yaml").is_file():
            try:
                target.relative_to(candidate)
                return candidate
            except ValueError:
                continue
    raise ValueError(f"渲染输出必须位于有效项目工作区内: {target}")


def render_with_libreoffice(docx: Path, pdf: Path, tmp_parent: Path, timeout: int) -> tuple[bool, str]:
    soffice = shutil.which("libreoffice") or shutil.which("soffice")
    if not soffice:
        return False, "LibreOffice/soffice not found"
    profile = Path(tempfile.mkdtemp(prefix="lo-profile-", dir=tmp_parent))
    try:
        result = subprocess.run(
            [
                soffice,
                f"-env:UserInstallation={profile.resolve().as_uri()}",
                "--headless",
                "--convert-to",
                "pdf",
                "--outdir",
                str(pdf.parent),
                str(docx),
            ],
            check=False,
            text=True,
            capture_output=True,
            timeout=timeout,
        )
        generated = pdf.parent / f"{docx.stem}.pdf"
        if generated.is_file() and generated.resolve() != pdf.resolve():
            shutil.move(generated, pdf)
        message = "\n".join(part for part in (result.stdout.strip(), result.stderr.strip()) if part)
        return result.returncode == 0 and pdf.is_file() and pdf.stat().st_size > 0, message
    except subprocess.TimeoutExpired:
        return False, f"LibreOffice 渲染超时（{timeout}s）"
    finally:
        shutil.rmtree(profile, ignore_errors=True)


def render_with_word_windows(docx: Path, pdf: Path, timeout: int) -> tuple[bool, str]:
    powershell = shutil.which("powershell") or shutil.which("pwsh")
    if not powershell:
        return False, "PowerShell not found"
    script = r'''
$ErrorActionPreference = "Stop"
$word = New-Object -ComObject Word.Application
$word.Visible = $false
try {
  $doc = $word.Documents.Open($args[0], $false, $true)
  $doc.ExportAsFixedFormat($args[1], 17)
  $doc.Close($false)
} finally {
  $word.Quit()
}
'''
    try:
        result = subprocess.run(
            [powershell, "-NoProfile", "-NonInteractive", "-Command", script, str(docx), str(pdf)],
            check=False,
            text=True,
            capture_output=True,
            timeout=timeout,
        )
        message = "\n".join(part for part in (result.stdout.strip(), result.stderr.strip()) if part)
        return result.returncode == 0 and pdf.is_file() and pdf.stat().st_size > 0, message
    except subprocess.TimeoutExpired:
        return False, f"Microsoft Word 渲染超时（{timeout}s）"


def render_with_word_macos(docx: Path, pdf: Path, timeout: int) -> tuple[bool, str]:
    osascript = shutil.which("osascript")
    word_app = Path("/Applications/Microsoft Word.app")
    if not osascript or not word_app.exists():
        return False, "Microsoft Word for macOS or osascript not found"
    script = r'''
on run argv
  set inputPath to item 1 of argv
  set outputPath to item 2 of argv
  tell application "Microsoft Word"
    set display alerts to none
    open POSIX file inputPath
    save as active document file name outputPath file format format PDF
    close active document saving no
  end tell
end run
'''
    try:
        result = subprocess.run(
            [osascript, "-e", script, str(docx), str(pdf)],
            check=False,
            text=True,
            capture_output=True,
            timeout=timeout,
        )
        message = "\n".join(part for part in (result.stdout.strip(), result.stderr.strip()) if part)
        return result.returncode == 0 and pdf.is_file() and pdf.stat().st_size > 0, message
    except subprocess.TimeoutExpired:
        return False, f"Microsoft Word 渲染超时（{timeout}s）"


def render_with_word(docx: Path, pdf: Path, timeout: int) -> tuple[bool, str]:
    system = platform.system().lower()
    if system == "windows":
        return render_with_word_windows(docx, pdf, timeout)
    if system == "darwin":
        return render_with_word_macos(docx, pdf, timeout)
    return False, "当前平台不支持 Microsoft Word 原生渲染"


def render_png_pages(pdf: Path, output_dir: Path, dpi: int) -> list[Path]:
    for old in output_dir.glob("page-*.png"):
        old.unlink()
    document = fitz.open(pdf)
    scale = dpi / 72.0
    matrix = fitz.Matrix(scale, scale)
    pages: list[Path] = []
    for index, page in enumerate(document, start=1):
        output = output_dir / f"page-{index:04d}.png"
        page.get_pixmap(matrix=matrix, alpha=False).save(output)
        pages.append(output)
    return pages


def main() -> int:
    parser = argparse.ArgumentParser(description="Render DOCX to PDF and optional PNG pages")
    parser.add_argument("docx", type=Path)
    parser.add_argument("output_dir", type=Path)
    parser.add_argument("--png", action="store_true")
    parser.add_argument("--dpi", type=int, default=144)
    parser.add_argument("--timeout", type=int, default=180)
    parser.add_argument("--engine", choices=["auto", "word", "libreoffice"], default="auto")
    args = parser.parse_args()

    try:
        root = project_root_for_output(args.output_dir)
    except ValueError as exc:
        parser.error(str(exc))
    docx = args.docx.expanduser().resolve()
    if not docx.is_file():
        parser.error(f"DOCX 不存在: {docx}")
    args.output_dir.mkdir(parents=True, exist_ok=True)
    pdf = args.output_dir / f"{docx.stem}.pdf"
    if pdf.exists():
        pdf.unlink()
    tmp_parent = root / "work/.tmp"
    tmp_parent.mkdir(parents=True, exist_ok=True)

    attempts: list[tuple[str, bool, str]] = []
    if args.engine in {"auto", "word"}:
        ok, message = render_with_word(docx, pdf, args.timeout)
        attempts.append(("word", ok, message))
        if args.engine == "word" and not ok:
            print(message, file=sys.stderr)
            return 2
    if not pdf.is_file() and args.engine in {"auto", "libreoffice"}:
        ok, message = render_with_libreoffice(docx, pdf, tmp_parent, args.timeout)
        attempts.append(("libreoffice", ok, message))
    if not pdf.is_file() or pdf.stat().st_size == 0:
        for engine, _, message in attempts:
            if message:
                print(f"[{engine}] {message}", file=sys.stderr)
        return 2

    selected = next((engine for engine, ok, _ in attempts if ok), args.engine)
    pages: list[Path] = []
    if args.png:
        pages = render_png_pages(pdf, args.output_dir, args.dpi)
        if not pages:
            print("PDF 转 PNG 后没有页面文件", file=sys.stderr)
            return 3
    print(f"engine={selected}")
    print(pdf)
    if pages:
        print(f"pages={len(pages)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
