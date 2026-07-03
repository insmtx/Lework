#!/usr/bin/env python3
"""
从 docx 中提取指定范围的页面或段落，生成新的 docx。
纯工具脚本，不包含任何业务逻辑。

用法:
  python3 extract_docx_range.py <source.docx> <output.docx> --from-page N
  python3 extract_docx_range.py <source.docx> <output.docx> --from-para N
  python3 extract_docx_range.py <source.docx> <output.docx> --from-para N --to-para M
"""

import sys
import os
import zipfile
import xml.etree.ElementTree as ET
import shutil
import tempfile
import argparse

WML_NS = 'http://schemas.openxmlformats.org/wordprocessingml/2006/main'


def extract_by_body_index(source_docx, output_docx, start, end=None):
    """从 body 的子元素索引 start 开始提取，可选提取到 end"""
    tmpdir = tempfile.mkdtemp()

    try:
        with zipfile.ZipFile(source_docx, 'r') as zf:
            zf.extractall(tmpdir)

        doc_path = os.path.join(tmpdir, 'word', 'document.xml')
        tree = ET.parse(doc_path)
        root = tree.getroot()
        body = root.find(f'.//{{{WML_NS}}}body')

        if body is None:
            print("Error: Cannot find document body", file=sys.stderr)
            return False

        children = list(body)
        total = len(children)

        if start < 0 or start >= total:
            print(f"Error: start index {start} out of range (0-{total-1})", file=sys.stderr)
            return False
        if end is not None and (end <= start or end > total):
            print(f"Error: end index {end} invalid (must be > {start} and <= {total})", file=sys.stderr)
            return False

        # Skip leading blank paragraphs with page breaks (chapter separator artifacts)
        while start < total:
            child = children[start]
            tag = child.tag.split('}')[-1] if '}' in child.tag else child.tag
            if tag != 'p':
                break
            texts = child.findall(f'.//{{{WML_NS}}}t')
            text = ''.join(t.text or '' for t in texts).strip()
            if text:
                break
            # Check if empty paragraph has a page break (either in pPr or in run br)
            has_pb = False
            pPr = child.find(f'{{{WML_NS}}}pPr')
            if pPr is not None and pPr.find(f'{{{WML_NS}}}pageBreakBefore') is not None:
                has_pb = True
            for br in child.findall(f'.//{{{WML_NS}}}br'):
                if br.get(f'{{{WML_NS}}}type') == 'page':
                    has_pb = True
            if has_pb:
                print(f"  Skipping leading blank page-break paragraph at index {start}")
                start += 1
            else:
                break

        # Remove elements before start
        for child in children[:start]:
            body.remove(child)

        # Remove elements after end (if specified)
        if end is not None:
            remaining = list(body)
            excess = remaining[end - start:]
            for child in excess:
                body.remove(child)

        remaining = len(list(body))
        print(f"Total body children: {total}, extracted: {remaining} (indices {start} to {end or total - 1})")

        tree.write(doc_path, xml_declaration=True, encoding='UTF-8')

        if os.path.exists(output_docx):
            os.remove(output_docx)

        with zipfile.ZipFile(output_docx, 'w', zipfile.ZIP_DEFLATED) as zf:
            for root_dir, dirs, files in os.walk(tmpdir):
                for f in files:
                    filepath = os.path.join(root_dir, f)
                    arcname = os.path.relpath(filepath, tmpdir)
                    if '__MACOSX' in arcname or arcname.startswith('.'):
                        continue
                    zf.write(filepath, arcname)

        print(f"Output: {output_docx}")
        return True

    finally:
        shutil.rmtree(tmpdir)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='Extract a range of body elements from a docx file.')
    parser.add_argument('source', help='Source docx file')
    parser.add_argument('output', help='Output docx file')
    parser.add_argument('--from', dest='start', type=int, required=True,
                        help='Starting body child index (0-based)')
    parser.add_argument('--to', dest='end', type=int, default=None,
                        help='Ending body child index (inclusive). If omitted, extract to end.')
    args = parser.parse_args()

    if not os.path.exists(args.source):
        print(f"Error: Source file not found: {args.source}", file=sys.stderr)
        sys.exit(1)

    success = extract_by_body_index(args.source, args.output, args.start, args.end)
    sys.exit(0 if success else 1)
