#!/usr/bin/env python3
"""Generate PDF-first Chinese government recognition policy documents."""

from __future__ import annotations

import argparse
import html
import json
import re
import shutil
import subprocess
import tempfile
from pathlib import Path
from typing import Any

from reportlab.lib import colors
from reportlab.lib.enums import TA_CENTER, TA_JUSTIFY, TA_LEFT, TA_RIGHT
from reportlab.lib.pagesizes import A4
from reportlab.lib.styles import ParagraphStyle, getSampleStyleSheet
from reportlab.lib.units import mm
from reportlab.platypus import (
    BaseDocTemplate,
    Frame,
    PageBreak,
    PageTemplate,
    Paragraph,
    Spacer,
    Table,
    TableStyle,
)
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.cidfonts import UnicodeCIDFont
from reportlab.pdfbase.ttfonts import TTFont


FONT_BODY = "CJK-Body"
FONT_BOLD = "CJK-Bold"
SKILL_DIR = Path(__file__).resolve().parents[1]
FONT_DIR = SKILL_DIR / "assets" / "fonts"
CHROME_CANDIDATES = [
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    "/Applications/Chromium.app/Contents/MacOS/Chromium",
    "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
]
PAGE_WIDTH, PAGE_HEIGHT = A4
LEFT_MARGIN = 28 * mm
RIGHT_MARGIN = 28 * mm
TOP_MARGIN = 37 * mm
BOTTOM_MARGIN = 22 * mm
BODY_WIDTH = 156 * mm
BODY_HEIGHT = 225 * mm


REQUIRED_FIELDS = [
    "authority",
    "authority_suffix",
    "doc_number",
    "notice_title",
    "policy_title",
    "issue_date",
    "chapters",
]


def register_fonts() -> None:
    global FONT_BODY, FONT_BOLD
    body_candidates = [
        FONT_DIR / "FangSong_GB2312.ttf",
        FONT_DIR / "FangSong.ttf",
        FONT_DIR / "仿宋_GB2312.ttf",
    ]
    bold_candidates = [
        FONT_DIR / "FZXBSJW.ttf",
        FONT_DIR / "方正小标宋简体.ttf",
        FONT_DIR / "SimHei.ttf",
    ]
    if not register_first_ttf(FONT_BODY, body_candidates):
        try:
            pdfmetrics.registerFont(UnicodeCIDFont("STSong-Light"))
            FONT_BODY = "STSong-Light"
        except Exception:
            pdfmetrics.registerFont(TTFont(FONT_BODY, "/System/Library/Fonts/Supplemental/Songti.ttc", subfontIndex=3))
    if not register_first_ttf(FONT_BOLD, bold_candidates):
        try:
            pdfmetrics.registerFont(TTFont(FONT_BOLD, "/System/Library/Fonts/STHeiti Medium.ttc"))
        except Exception:
            FONT_BOLD = FONT_BODY


def register_first_ttf(font_name: str, candidates: list[Path]) -> bool:
    for path in candidates:
        if not path.exists():
            continue
        try:
            pdfmetrics.registerFont(TTFont(font_name, str(path)))
            return True
        except Exception:
            continue
    return False


class NumberedDoc(BaseDocTemplate):
    def __init__(self, filename: str, styles: dict[str, ParagraphStyle]):
        self.styles_for_footer = styles
        frame = Frame(LEFT_MARGIN, BOTTOM_MARGIN, BODY_WIDTH, BODY_HEIGHT, id="normal")
        super().__init__(
            filename,
            pagesize=A4,
            leftMargin=LEFT_MARGIN,
            rightMargin=RIGHT_MARGIN,
            topMargin=TOP_MARGIN,
            bottomMargin=BOTTOM_MARGIN,
        )
        self.addPageTemplates([PageTemplate(id="normal", frames=[frame], onPage=self.footer)])

    def footer(self, canvas, doc) -> None:  # noqa: ANN001
        canvas.saveState()
        canvas.setFillColor(colors.white)
        canvas.rect(0, 0, PAGE_WIDTH, PAGE_HEIGHT, fill=1, stroke=0)
        canvas.setFillColor(colors.black)
        canvas.setFont(FONT_BODY, 11)
        canvas.drawCentredString(PAGE_WIDTH / 2, 13 * mm, f"— {doc.page} —")
        canvas.restoreState()


def make_styles() -> dict[str, ParagraphStyle]:
    base = getSampleStyleSheet()
    return {
        "red_head": ParagraphStyle(
            "red_head",
            parent=base["Normal"],
            fontName=FONT_BODY,
            fontSize=38,
            leading=46,
            alignment=TA_CENTER,
            textColor=colors.HexColor("#E60000"),
            spaceAfter=16,
            wordWrap="CJK",
        ),
        "doc_number": ParagraphStyle(
            "doc_number",
            parent=base["Normal"],
            fontName=FONT_BODY,
            fontSize=13,
            leading=20,
            alignment=TA_CENTER,
            spaceAfter=10,
            wordWrap="CJK",
        ),
        "notice_title": ParagraphStyle(
            "notice_title",
            parent=base["Normal"],
            fontName=FONT_BOLD,
            fontSize=19,
            leading=29,
            alignment=TA_CENTER,
            spaceBefore=32,
            spaceAfter=18,
            wordWrap="CJK",
        ),
        "policy_title": ParagraphStyle(
            "policy_title",
            parent=base["Normal"],
            fontName=FONT_BOLD,
            fontSize=19,
            leading=29,
            alignment=TA_CENTER,
            spaceAfter=16,
            wordWrap="CJK",
        ),
        "chapter": ParagraphStyle(
            "chapter",
            parent=base["Normal"],
            fontName=FONT_BOLD,
            fontSize=15,
            leading=25,
            alignment=TA_CENTER,
            spaceBefore=7,
            spaceAfter=3,
            wordWrap="CJK",
        ),
        "body": ParagraphStyle(
            "body",
            parent=base["Normal"],
            fontName=FONT_BODY,
            fontSize=14,
            leading=24,
            firstLineIndent=28,
            alignment=TA_LEFT,
            wordWrap="CJK",
        ),
        "body_no_indent": ParagraphStyle(
            "body_no_indent",
            parent=base["Normal"],
            fontName=FONT_BODY,
            fontSize=14,
            leading=24,
            alignment=TA_LEFT,
            wordWrap="CJK",
        ),
        "right": ParagraphStyle(
            "right",
            parent=base["Normal"],
            fontName=FONT_BODY,
            fontSize=14,
            leading=24,
            alignment=TA_RIGHT,
            wordWrap="CJK",
        ),
        "attachment_title": ParagraphStyle(
            "attachment_title",
            parent=base["Normal"],
            fontName=FONT_BOLD,
            fontSize=17,
            leading=27,
            alignment=TA_CENTER,
            spaceBefore=12,
            spaceAfter=16,
            wordWrap="CJK",
        ),
        "table": ParagraphStyle(
            "table",
            parent=base["Normal"],
            fontName=FONT_BODY,
            fontSize=11,
            leading=15,
            alignment=TA_CENTER,
            wordWrap="CJK",
        ),
        "table_header": ParagraphStyle(
            "table_header",
            parent=base["Normal"],
            fontName=FONT_BOLD,
            fontSize=11,
            leading=15,
            alignment=TA_CENTER,
            wordWrap="CJK",
        ),
        "table_left": ParagraphStyle(
            "table_left",
            parent=base["Normal"],
            fontName=FONT_BODY,
            fontSize=11,
            leading=15,
            alignment=TA_LEFT,
            wordWrap="CJK",
        ),
        "signature_label": ParagraphStyle(
            "signature_label",
            parent=base["Normal"],
            fontName=FONT_BODY,
            fontSize=13,
            leading=22,
            alignment=TA_RIGHT,
            wordWrap="CJK",
        ),
        "print_footer": ParagraphStyle(
            "print_footer",
            parent=base["Normal"],
            fontName=FONT_BODY,
            fontSize=12,
            leading=18,
            alignment=TA_LEFT,
            wordWrap="CJK",
        ),
        "print_footer_right": ParagraphStyle(
            "print_footer_right",
            parent=base["Normal"],
            fontName=FONT_BODY,
            fontSize=12,
            leading=18,
            alignment=TA_RIGHT,
            wordWrap="CJK",
        ),
    }


def para(text: str, style: ParagraphStyle, width: int | None = None) -> Paragraph:
    safe = str(text).replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")
    return Paragraph(safe, style)


def merge_data(input_data: dict[str, Any]) -> dict[str, Any]:
    data = dict(input_data)
    validate_input(data)
    return data


def validate_input(data: dict[str, Any]) -> None:
    missing = [field for field in REQUIRED_FIELDS if not data.get(field)]
    if missing:
        raise ValueError(
            "Missing required input fields: "
            + ", ".join(missing)
            + ". Draft the document content first, then pass it as JSON."
        )
    if not isinstance(data.get("chapters"), list):
        raise ValueError("Field 'chapters' must be a list of chapter objects.")
    if not isinstance(data.get("attachments", []), list):
        raise ValueError("Field 'attachments' must be a list.")


def clean_text(text: Any) -> str:
    value = str(text)
    if re.match(r"^\s*日期：", value):
        value = re.sub(r"(?<=\d)(?=[\u4e00-\u9fff])", " ", value)
        value = re.sub(r"(?<=[\u4e00-\u9fff])(?=\d)", " ", value)
        return value.strip()
    value = re.sub(r"\s+", " ", value).strip()
    value = re.sub(r"^(第[一二三四五六七八九十百]+条)\s*", r"\1  ", value)
    value = re.sub(r"^(\s*\d+)\.\s*", r"\1. ", value)
    value = re.sub(r"(?<=\d)(?=[\u4e00-\u9fff])", " ", value)
    value = re.sub(r"(?<=[\u4e00-\u9fff])(?=\d)", " ", value)
    return value


def esc(text: Any) -> str:
    return html.escape(clean_text(text), quote=True)


def add_notice(story: list[Any], data: dict[str, Any], styles: dict[str, ParagraphStyle]) -> None:
    story.append(Spacer(1, 22 * mm))
    story.append(para(f"{data['authority']}{data['authority_suffix']}", styles["red_head"], 18))
    story.append(para(data["doc_number"], styles["doc_number"]))
    story.append(Table([[""]], colWidths=[BODY_WIDTH], rowHeights=[1.2], style=[
        ("BACKGROUND", (0, 0), (-1, -1), colors.HexColor("#E60000")),
    ]))
    story.append(para(data["notice_title"], styles["notice_title"], 21))
    if data.get("addressees"):
        story.append(para(data["addressees"], styles["body_no_indent"]))
    if data.get("notice_body"):
        story.append(para(data["notice_body"], styles["body"], 31))
    story.append(Spacer(1, 12 * mm))
    story.append(para(data["authority"], styles["right"]))
    story.append(para(data["issue_date"], styles["right"]))
    story.append(PageBreak())


def add_policy(story: list[Any], data: dict[str, Any], styles: dict[str, ParagraphStyle]) -> None:
    story.append(para(data["policy_title"], styles["policy_title"], 18))
    chapter_nums = "一二三四五六七八九十"
    for idx, chapter in enumerate(data.get("chapters", []), start=1):
        prefix = f"第{chapter_nums[idx - 1] if idx <= 10 else idx}章"
        story.append(para(f"{prefix}  {chapter['title']}", styles["chapter"]))
        for article in chapter.get("articles", []):
            story.append(para(str(article), styles["body"], 31))
    attachment_names = attachment_title_list(data)
    if attachment_names:
        story.append(Spacer(1, 8 * mm))
        story.append(attachment_list_block(attachment_names, styles))
    story.append(PageBreak())


def attachment_title_list(data: dict[str, Any]) -> list[str]:
    names = []
    for item in data.get("attachments", []):
        if isinstance(item, dict):
            title = item.get("title")
            if title:
                names.append(str(title))
        else:
            names.append(str(item))
    return names


def attachment_list_block(names: list[str], styles: dict[str, ParagraphStyle]) -> Table:
    rows = []
    for idx, name in enumerate(names, start=1):
        label = "附件：" if idx == 1 else ""
        rows.append([
            para(label, styles["body_no_indent"]),
            para(f"{idx}. {name}", styles["body_no_indent"]),
        ])
    table = Table(rows, colWidths=[25 * mm, BODY_WIDTH - 25 * mm], hAlign="LEFT")
    table.setStyle(TableStyle([
        ("VALIGN", (0, 0), (-1, -1), "TOP"),
        ("LEFTPADDING", (0, 0), (-1, -1), 0),
        ("RIGHTPADDING", (0, 0), (-1, -1), 0),
        ("TOPPADDING", (0, 0), (-1, -1), 1),
        ("BOTTOMPADDING", (0, 0), (-1, -1), 1),
    ]))
    return table


def cell(text: str, styles: dict[str, ParagraphStyle], left: bool = False, header: bool = False) -> Paragraph:
    if header:
        return para(text, styles["table_header"])
    return para(text, styles["table_left" if left else "table"])


def styled_table(rows: list[list[Any]], widths: list[float], repeat: int = 0) -> Table:
    table = Table(rows, colWidths=widths, repeatRows=repeat)
    table.setStyle(TableStyle([
        ("FONTSIZE", (0, 0), (-1, -1), 12),
        ("GRID", (0, 0), (-1, -1), 0.6, colors.black),
        ("VALIGN", (0, 0), (-1, -1), "MIDDLE"),
        ("ALIGN", (0, 0), (-1, -1), "CENTER"),
        ("LEFTPADDING", (0, 0), (-1, -1), 5),
        ("RIGHTPADDING", (0, 0), (-1, -1), 5),
        ("TOPPADDING", (0, 0), (-1, -1), 10),
        ("BOTTOMPADDING", (0, 0), (-1, -1), 10),
    ]))
    return table


def signature_block(labels: list[str], styles: dict[str, ParagraphStyle], label_width: float = 42 * mm, blank_width: float = 28 * mm) -> Table:
    rows = [[para(label, styles["signature_label"]), ""] for label in labels]
    table = Table(rows, colWidths=[label_width, blank_width], hAlign="RIGHT")
    table.setStyle(TableStyle([
        ("VALIGN", (0, 0), (-1, -1), "MIDDLE"),
        ("LEFTPADDING", (0, 0), (-1, -1), 0),
        ("RIGHTPADDING", (0, 0), (-1, -1), 0),
        ("TOPPADDING", (0, 0), (-1, -1), 1),
        ("BOTTOMPADDING", (0, 0), (-1, -1), 1),
    ]))
    return table


def print_footer_block(data: dict[str, Any], styles: dict[str, ParagraphStyle]) -> Table:
    table = Table(
        [[para(data["print_office"], styles["print_footer"]), para(data["print_date"], styles["print_footer_right"])]],
        colWidths=[BODY_WIDTH / 2, BODY_WIDTH / 2],
        rowHeights=[9 * mm],
        hAlign="CENTER",
    )
    table.setStyle(TableStyle([
        ("LINEABOVE", (0, 0), (-1, 0), 0.8, colors.black),
        ("LINEBELOW", (0, 0), (-1, 0), 0.8, colors.black),
        ("VALIGN", (0, 0), (-1, -1), "MIDDLE"),
        ("LEFTPADDING", (0, 0), (-1, -1), 8),
        ("RIGHTPADDING", (0, 0), (-1, -1), 8),
        ("TOPPADDING", (0, 0), (-1, -1), 2),
        ("BOTTOMPADDING", (0, 0), (-1, -1), 2),
    ]))
    return table


def add_block(story: list[Any], block: dict[str, Any], styles: dict[str, ParagraphStyle]) -> None:
    block_type = block.get("type", "paragraphs")
    if block_type == "paragraphs":
        style_name = block.get("style", "body")
        style = styles.get(style_name, styles["body"])
        for text in block.get("items", []):
            story.append(para(str(text), style))
    elif block_type == "table":
        columns = block.get("columns", [])
        rows = block.get("rows", [])
        table_rows = []
        if columns:
            table_rows.append([cell(str(col), styles, header=True) for col in columns])
        for row in rows:
            table_rows.append([cell(str(value), styles) for value in row])
        if table_rows:
            widths = block.get("widths_mm")
            if widths:
                col_widths = [float(width) * mm for width in widths]
            else:
                col_widths = [BODY_WIDTH / len(table_rows[0])] * len(table_rows[0])
            story.append(styled_table(table_rows, col_widths, repeat=1 if columns else 0))
    elif block_type == "signature":
        labels = [str(label) for label in block.get("labels", [])]
        if labels:
            story.append(signature_block(labels, styles))
    elif block_type == "spacer":
        story.append(Spacer(1, float(block.get("height_mm", 8)) * mm))
    elif block_type == "page_break":
        story.append(PageBreak())
    else:
        raise ValueError(f"Unsupported attachment block type: {block_type}")


def add_attachments(story: list[Any], data: dict[str, Any], styles: dict[str, ParagraphStyle]) -> None:
    attachments = data.get("attachments", [])
    for number, item in enumerate(attachments, start=1):
        if not isinstance(item, dict):
            raise ValueError("Each attachment must be an object with title and optional blocks.")
        title = item.get("title")
        if not title:
            raise ValueError("Each attachment must include a title.")
        story.append(para(f"附件 {number}", styles["body_no_indent"]))
        story.append(para(str(title), styles["attachment_title"]))
        for block in item.get("blocks", []):
            if not isinstance(block, dict):
                raise ValueError("Attachment blocks must be objects.")
            add_block(story, block, styles)
        if number < len(attachments):
            story.append(PageBreak())


def font_url(name: str) -> str:
    return (FONT_DIR / name).resolve().as_uri()


def chrome_path() -> str | None:
    for candidate in CHROME_CANDIDATES:
        if Path(candidate).exists():
            return candidate
    for candidate in ["google-chrome", "chromium", "chromium-browser", "msedge"]:
        found = shutil.which(candidate)
        if found:
            return found
    return None


def render_html_document(data: dict[str, Any]) -> str:
    parts = [
        "<!doctype html><html><head><meta charset='utf-8'>",
        "<style>",
        f"""
@font-face {{
  font-family: 'GovFang';
  src: url('{font_url("FandolFang-Regular.otf")}') format('opentype');
}}
@font-face {{
  font-family: 'GovSong';
  src: url('{font_url("FandolSong-Regular.otf")}') format('opentype');
}}
@font-face {{
  font-family: 'GovHei';
  src: url('{font_url("FandolHei-Regular.otf")}') format('opentype');
}}
@page {{
  size: A4;
  margin: 37mm 28mm 22mm 28mm;
}}
* {{ box-sizing: border-box; }}
body {{
  margin: 0;
  color: #000;
  background: #fff;
  font-family: 'GovFang', 'Songti SC', serif;
  font-size: 16pt;
  line-height: 30pt;
  font-weight: 400;
  -webkit-print-color-adjust: exact;
  print-color-adjust: exact;
}}
html {{ background: #fff; }}
.page-break {{ break-after: page; page-break-after: always; }}
.notice-spacer {{ height: 36mm; }}
.red-head {{
  color: #e60000;
  font-family: 'GovSong', 'Songti SC', serif;
  font-size: 48pt;
  line-height: 58pt;
  text-align: center;
  margin: 0 0 24mm 0;
  font-weight: 400;
}}
.doc-number {{
  text-align: center;
  font-size: 14pt;
  line-height: 22pt;
  margin: 2mm 0 4mm 0;
}}
.red-line {{
  height: 1.1pt;
  background: #e60000;
  margin: 0 0 28mm 0;
}}
.notice-title {{
  font-family: 'GovHei', 'Heiti SC', sans-serif;
  font-size: 20pt;
  line-height: 32pt;
  text-align: center;
  font-weight: 400;
  margin: 0 0 12mm 0;
}}
.policy-title {{
  font-family: 'GovHei', 'Heiti SC', sans-serif;
  font-size: 20pt;
  line-height: 32pt;
  text-align: center;
  font-weight: 400;
  margin: 0 0 12mm 0;
}}
.chapter {{
  font-family: 'GovHei', 'Heiti SC', sans-serif;
  font-size: 16pt;
  line-height: 30pt;
  text-align: center;
  font-weight: 400;
  margin: 7mm 0 1mm 0;
}}
p {{
  margin: 0;
  text-align: justify;
  text-justify: inter-ideograph;
}}
.body {{ text-indent: 2em; }}
.no-indent {{ text-indent: 0; }}
.right {{ text-align: right; }}
.notice-signature {{
  margin-top: 12mm;
  padding-right: 12mm;
}}
.attachment-list {{
  display: grid;
  grid-template-columns: 25mm 1fr;
  column-gap: 0;
  margin-top: 9mm;
}}
.attachment-list div {{
  line-height: 30pt;
}}
.attachment-marker {{
  white-space: nowrap;
}}
.attachment-item {{
  padding-left: 0;
  text-indent: 0;
}}
.attachment-prefix {{
  font-size: 16pt;
  line-height: 30pt;
  margin: 0 0 4mm 0;
}}
.attachment-title {{
  font-family: 'GovHei', 'Heiti SC', sans-serif;
  font-size: 18pt;
  line-height: 32pt;
  text-align: center;
  font-weight: 400;
  margin: 0 0 10mm 0;
}}
table.form {{
  width: calc(100% - 0.8mm);
  border-collapse: collapse;
  table-layout: fixed;
  margin: 0 0 2mm 0;
  font-family: 'GovFang', 'Songti SC', serif;
  font-size: 13.5pt;
  line-height: 23pt;
  border: 0.75pt solid #000;
}}
table.form th, table.form td {{
  border: 0.75pt solid #000;
  padding: 5mm 3mm;
  text-align: center;
  vertical-align: middle;
  font-weight: 400;
}}
table.form th {{
  font-family: 'GovHei', 'Heiti SC', sans-serif;
}}
.table-wrap {{
  width: 100%;
  overflow: visible;
  padding-right: 0.8mm;
}}
.numbered {{
  text-indent: 0;
  padding-left: 0;
  text-align: justify;
}}
.signature {{
  width: 86mm;
  margin: -5mm 12mm 0 auto;
  font-size: 16pt;
  line-height: 30pt;
}}
.signature-row {{
  display: grid;
  grid-template-columns: 60mm 1fr;
  min-height: 9mm;
}}
.signature-label {{
  text-align: right;
  white-space: nowrap;
}}
.signature-row.date-row {{
  display: block;
}}
.signature-row.date-row .signature-label {{
  text-align: left;
  white-space: pre;
  padding-left: 31mm;
}}
.print-page {{
  break-before: page;
  page-break-before: always;
  position: relative;
  height: 238mm;
}}
.print-footer {{
  position: absolute;
  bottom: 4mm;
  left: 0;
  width: 100%;
  border-top: 0.8pt solid #000;
  border-bottom: 0.8pt solid #000;
  display: grid;
  grid-template-columns: 1fr 1fr;
  align-items: center;
  min-height: 10mm;
  padding: 0 7mm;
  font-size: 13pt;
  line-height: 20pt;
}}
.print-footer .date {{ text-align: right; }}
""",
        "</style></head><body>",
    ]
    parts.extend(render_html_notice(data))
    parts.extend(render_html_policy(data))
    parts.extend(render_html_attachments(data))
    if data.get("print_office") and data.get("print_date"):
        parts.append(
            "<section class='print-page'><div class='print-footer'>"
            f"<div>{esc(data['print_office'])}</div><div class='date'>{esc(data['print_date'])}</div>"
            "</div></section>"
        )
    parts.append("</body></html>")
    return "".join(parts)


def render_html_notice(data: dict[str, Any]) -> list[str]:
    parts = [
        "<section class='notice page-break'>",
        "<div class='notice-spacer'></div>",
        f"<h1 class='red-head'>{esc(data['authority'])}{esc(data['authority_suffix'])}</h1>",
        f"<p class='doc-number'>{esc(data['doc_number'])}</p>",
        "<div class='red-line'></div>",
        f"<h2 class='notice-title'>{esc(data['notice_title'])}</h2>",
    ]
    if data.get("addressees"):
        parts.append(f"<p class='no-indent'>{esc(data['addressees'])}</p>")
    if data.get("notice_body"):
        parts.append(f"<p class='body'>{esc(data['notice_body'])}</p>")
    parts.append("<div class='notice-signature'>")
    parts.append(f"<p class='right'>{esc(data['authority'])}</p>")
    parts.append(f"<p class='right'>{esc(data['issue_date'])}</p>")
    parts.append("</div></section>")
    return parts


def render_html_policy(data: dict[str, Any]) -> list[str]:
    chapter_nums = "一二三四五六七八九十"
    parts = ["<section class='policy page-break'>", f"<h1 class='policy-title'>{esc(data['policy_title'])}</h1>"]
    for idx, chapter in enumerate(data.get("chapters", []), start=1):
        prefix = f"第{chapter_nums[idx - 1] if idx <= 10 else idx}章"
        parts.append(f"<h2 class='chapter'>{esc(prefix)} {esc(chapter['title'])}</h2>")
        for article in chapter.get("articles", []):
            parts.append(f"<p class='body'>{esc(article)}</p>")
    attachment_names = attachment_title_list(data)
    if attachment_names:
        parts.append("<div class='attachment-list'>")
        for idx, name in enumerate(attachment_names, start=1):
            marker = "附件：" if idx == 1 else ""
            parts.append(f"<div class='attachment-marker'>{esc(marker)}</div>")
            parts.append(f"<div class='attachment-item'>{idx}. {esc(name)}</div>")
        parts.append("</div>")
    parts.append("</section>")
    return parts


def render_html_attachments(data: dict[str, Any]) -> list[str]:
    parts = []
    attachments = data.get("attachments", [])
    for number, item in enumerate(attachments, start=1):
        if not isinstance(item, dict):
            raise ValueError("Each attachment must be an object with title and optional blocks.")
        title = item.get("title")
        if not title:
            raise ValueError("Each attachment must include a title.")
        page_class = "attachment"
        if number < len(attachments):
            page_class += " page-break"
        parts.append(f"<section class='{page_class}'>")
        parts.append(f"<p class='attachment-prefix'>附件 {number}</p>")
        parts.append(f"<h1 class='attachment-title'>{esc(title)}</h1>")
        for block in item.get("blocks", []):
            parts.append(render_html_block(block))
        parts.append("</section>")
    return parts


def render_html_block(block: dict[str, Any]) -> str:
    block_type = block.get("type", "paragraphs")
    if block_type == "paragraphs":
        style_name = block.get("style", "body")
        cls = "no-indent" if style_name == "body_no_indent" else "body"
        items = []
        for text in block.get("items", []):
            item_class = "numbered" if re.match(r"^\s*\d+\.", str(text)) else cls
            items.append(f"<p class='{item_class}'>{esc(text)}</p>")
        return "".join(items)
    if block_type == "table":
        columns = block.get("columns", [])
        rows = block.get("rows", [])
        widths = block.get("widths_mm")
        colgroup = ""
        if widths:
            total = sum(float(width) for width in widths)
            colgroup = "<colgroup>" + "".join(
                f"<col style='width:{float(width) / total * 100:.3f}%'>" for width in widths
            ) + "</colgroup>"
        head = ""
        if columns:
            head = "<thead><tr>" + "".join(f"<th>{esc(col)}</th>" for col in columns) + "</tr></thead>"
        body_rows = "".join(
            "<tr>" + "".join(f"<td>{esc(value)}</td>" for value in row) + "</tr>"
            for row in rows
        )
        return f"<div class='table-wrap'><table class='form'>{colgroup}{head}<tbody>{body_rows}</tbody></table></div>"
    if block_type == "signature":
        labels = [str(label) for label in block.get("labels", [])]
        rows = "".join(
            render_signature_row(label)
            for label in labels
        )
        return f"<div class='signature'>{rows}</div>"
    if block_type == "spacer":
        return f"<div style='height:{float(block.get('height_mm', 8))}mm'></div>"
    if block_type == "page_break":
        return "<div class='page-break'></div>"
    raise ValueError(f"Unsupported attachment block type: {block_type}")


def render_signature_row(label: str) -> str:
    row_class = "signature-row date-row" if label.strip().startswith("日期：") else "signature-row"
    return f"<div class='{row_class}'><div class='signature-label'>{esc(label)}</div><div></div></div>"


def build_pdf_html(data: dict[str, Any], output: Path) -> bool:
    executable = chrome_path()
    if not executable:
        return False
    html_text = render_html_document(data)
    with tempfile.TemporaryDirectory() as tmpdir:
        tmp = Path(tmpdir)
        html_path = tmp / "policy.html"
        raw_pdf = tmp / "policy.raw.pdf"
        html_path.write_text(html_text, encoding="utf-8")
        command = [
            executable,
            "--headless=new",
            "--disable-gpu",
            "--no-first-run",
            "--no-default-browser-check",
            "--disable-extensions",
            "--no-pdf-header-footer",
            "--print-to-pdf-no-header",
            f"--print-to-pdf={raw_pdf}",
            html_path.as_uri(),
        ]
        subprocess.run(command, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        overlay_page_numbers(raw_pdf, output)
    return True


def overlay_page_numbers(source: Path, output: Path) -> None:
    from pypdf import PdfReader, PdfWriter

    reader = PdfReader(str(source))
    writer = PdfWriter()
    for idx, page in enumerate(reader.pages, start=1):
        base = white_page().pages[0]
        base.merge_page(page)
        base.merge_page(numbered_overlay(idx).pages[0])
        writer.add_page(base)
    with output.open("wb") as handle:
        writer.write(handle)


def white_page():
    from io import BytesIO
    from pypdf import PdfReader
    from reportlab.pdfgen import canvas

    packet = BytesIO()
    c = canvas.Canvas(packet, pagesize=A4)
    c.setFillColor(colors.white)
    c.rect(0, 0, PAGE_WIDTH, PAGE_HEIGHT, fill=1, stroke=0)
    c.save()
    packet.seek(0)
    return PdfReader(packet)


def numbered_overlay(page_number: int):
    from io import BytesIO
    from pypdf import PdfReader
    from reportlab.pdfgen import canvas

    packet = BytesIO()
    c = canvas.Canvas(packet, pagesize=A4)
    try:
        pdfmetrics.registerFont(UnicodeCIDFont("STSong-Light"))
        c.setFont("STSong-Light", 11)
    except Exception:
        c.setFont("Helvetica", 11)
    c.drawCentredString(PAGE_WIDTH / 2, 13 * mm, f"— {page_number} —")
    c.save()
    packet.seek(0)
    return PdfReader(packet)


def build_pdf(data: dict[str, Any], output: Path) -> None:
    if build_pdf_html(data, output):
        return
    register_fonts()
    styles = make_styles()
    doc = NumberedDoc(str(output), styles)
    story: list[Any] = []
    add_notice(story, data, styles)
    add_policy(story, data, styles)
    add_attachments(story, data, styles)
    if data.get("print_office") and data.get("print_date"):
        story.append(Spacer(1, 202 * mm))
        story.append(print_footer_block(data, styles))
    doc.build(story)


def main() -> None:
    parser = argparse.ArgumentParser(description="Generate a Chinese government recognition policy PDF.")
    parser.add_argument("--input", type=Path, required=True, help="JSON input file with drafted document content.")
    parser.add_argument("--output", type=Path, required=True, help="Output PDF path.")
    args = parser.parse_args()

    input_data = json.loads(args.input.read_text(encoding="utf-8"))
    data = merge_data(input_data)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    build_pdf(data, args.output)
    print(f"Wrote {args.output}")


if __name__ == "__main__":
    main()
