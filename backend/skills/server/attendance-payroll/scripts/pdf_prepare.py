"""Prepare bounded, OCR-friendly page images from a PDF."""
import os
import sys

import fitz
from PIL import Image


def non_white_bounds(pix):
    """Return the content bounds, preserving a small margin for table lines."""
    samples, width, height, stride = pix.samples, pix.width, pix.height, pix.stride
    rows = []
    for y in range(height):
        row = samples[y * stride:(y + 1) * stride]
        if any(value < 245 for value in row):
            rows.append(y)
    if not rows:
        return 0, height
    return max(0, rows[0] - 12), min(height, rows[-1] + 13)


def render_pdf(source, target, limit):
    """Render each original PDF page to one PNG. Do not split rows or date columns."""
    document = fitz.open(source)
    if len(document) > limit:
        raise RuntimeError("PDF exceeds page limit")
    paths = []
    for page_number, page in enumerate(document, 1):
        full = page.get_pixmap(dpi=144, alpha=False)
        start, end = non_white_bounds(full)
        image = Image.frombytes("RGB", (full.width, full.height), full.samples)
        path = os.path.join(target, f"page-{page_number}-part-1.png")
        image.crop((0, start, image.width, end)).save(path)
        paths.append(path)
    return paths


if __name__ == "__main__":
    source, target, limit = sys.argv[1], sys.argv[2], int(sys.argv[3])
    render_pdf(source, target, limit)
