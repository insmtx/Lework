"""Prepare bounded, OCR-friendly page images from a PDF."""
import os
import sys
from io import BytesIO

import fitz
from PIL import Image

MAX_IMAGE_SIDE = 1800
MAX_IMAGE_BYTES = 3 * 1024 * 1024


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
    """Render each original PDF page to one bounded image.

    The page remains intact: rows and date columns are never split. The size
    limit prevents OpenCode from entering its large-image normalization path
    when the model reads a generated page image.
    """
    document = fitz.open(source)
    if len(document) > limit:
        raise RuntimeError("PDF exceeds page limit")
    paths = []
    for page_number, page in enumerate(document, 1):
        full = page.get_pixmap(dpi=120, alpha=False)
        start, end = non_white_bounds(full)
        image = Image.frombytes("RGB", (full.width, full.height), full.samples)
        image = image.crop((0, start, image.width, end))
        image.thumbnail((MAX_IMAGE_SIDE, MAX_IMAGE_SIDE), Image.Resampling.LANCZOS)

        png_buffer = BytesIO()
        image.save(png_buffer, format="PNG", optimize=True)
        suffix = ".png"
        payload = png_buffer.getvalue()
        if len(payload) > MAX_IMAGE_BYTES:
            # Keep text/table edges readable while avoiding OpenCode's
            # base64/image.normalize threshold. PNG remains the default.
            for quality in (85, 75, 65):
                jpeg_buffer = BytesIO()
                image.save(jpeg_buffer, format="JPEG", quality=quality, optimize=True)
                payload = jpeg_buffer.getvalue()
                suffix = ".jpg"
                if len(payload) <= MAX_IMAGE_BYTES:
                    break

        path = os.path.join(target, f"page-{page_number}-part-1{suffix}")
        with open(path, "wb") as output:
            output.write(payload)
        paths.append(path)
    return paths


if __name__ == "__main__":
    source, target, limit = sys.argv[1], sys.argv[2], int(sys.argv[3])
    render_pdf(source, target, limit)
