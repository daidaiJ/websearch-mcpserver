#!/usr/bin/env python3
import sys

engine, path = sys.argv[1], sys.argv[2]
text = ""
try:
    if engine == "pymupdf":
        import pymupdf
        doc = pymupdf.open(path)
        text = "\n".join(page.get_text() for page in doc)
        doc.close()
    elif engine == "pypdf":
        from pypdf import PdfReader
        reader = PdfReader(path)
        text = "\n".join((page.extract_text() or "") for page in reader.pages)
    elif engine == "pdfminer":
        from pdfminer.high_level import extract_text
        text = extract_text(path) or ""
    elif engine == "pypdfium2":
        import pypdfium2 as pdfium
        pdf = pdfium.PdfDocument(path)
        parts = []
        for i in range(len(pdf)):
            page = pdf[i]
            tp = page.get_textpage()
            parts.append(tp.get_text_bounded() or "")
            tp.close()
            page.close()
        pdf.close()
        text = "\n".join(parts)
    else:
        print("unknown engine", engine, file=sys.stderr)
        sys.exit(2)
except Exception as e:
    print(f"{type(e).__name__}: {e}", file=sys.stderr)
    sys.exit(2)
if not text.strip():
    print("no text", file=sys.stderr)
    sys.exit(2)
sys.stdout.write(text)
