#!/usr/bin/env python3
"""Extract text from a PDF read via stdin, output JSON to stdout.

Usage:
    cat file.pdf | python3 extract_pdf_text.py [--page-range RANGE] [--max-pages N] [--password PWD]

Output JSON keys:
    total_pages      – number of pages in the PDF
    extracted_pages  – number of pages actually extracted
    content          – concatenated text (with page headers)
    error            – non-empty string on failure (always valid JSON)
"""

import argparse
import json
import sys
from io import BytesIO

def parse_page_range(range_str, total_pages, max_pages):
    """Return a list of 1-based page numbers to extract."""
    range_str = range_str.strip().lower()
    if range_str in ("all", ""):
        return list(range(1, min(total_pages, max_pages) + 1))

    pages = []
    for part in range_str.split(","):
        part = part.strip()
        if "-" in part:
            bounds = part.split("-", 1)
            if len(bounds) != 2:
                raise ValueError(f"invalid range format: {part}")
            start, end = int(bounds[0].strip()), int(bounds[1].strip())
            for p in range(start, end + 1):
                if len(pages) >= max_pages:
                    break
                pages.append(p)
        else:
            if len(pages) >= max_pages:
                break
            pages.append(int(part))
    return pages


def main():
    parser = argparse.ArgumentParser(description="Extract text from PDF via stdin")
    parser.add_argument("--page-range", default="all", help="Page range (e.g. '1-5', '1,3,5', 'all')")
    parser.add_argument("--max-pages", type=int, default=50, help="Maximum pages to extract")
    parser.add_argument("--password", default="", help="PDF password")
    args = parser.parse_args()

    try:
        from pypdf import PdfReader
    except ImportError:
        json.dump({"total_pages": 0, "extracted_pages": 0, "content": "", "error": "pypdf not installed"}, sys.stdout)
        return

    try:
        data = sys.stdin.buffer.read()
        if not data:
            json.dump({"total_pages": 0, "extracted_pages": 0, "content": "", "error": "no PDF data on stdin"}, sys.stdout)
            return

        reader = PdfReader(BytesIO(data), password=args.password if args.password else None)
        total_pages = len(reader.pages)

        if total_pages == 0:
            json.dump({"total_pages": 0, "extracted_pages": 0, "content": "", "error": "PDF has no pages"}, sys.stdout)
            return

        pages_to_extract = parse_page_range(args.page_range, total_pages, args.max_pages)

        parts = []
        extracted = 0
        for page_num in pages_to_extract:
            if page_num < 1 or page_num > total_pages:
                continue
            page = reader.pages[page_num - 1]
            text = page.extract_text() or ""
            if text:
                parts.append(f"\n--- Page {page_num} ---\n{text}")
                extracted += 1

        content = "\n".join(parts).strip()
        if not content:
            content = "(No text content could be extracted from this PDF. It may contain only images or scanned content.)"

        json.dump({"total_pages": total_pages, "extracted_pages": extracted, "content": content, "error": ""}, sys.stdout)

    except Exception as e:
        json.dump({"total_pages": 0, "extracted_pages": 0, "content": "", "error": str(e)}, sys.stdout)


if __name__ == "__main__":
    main()
