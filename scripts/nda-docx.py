"""Builds the NDA .docx files from their Markdown source: python scripts/nda-docx.py campaigns/zero-cost-q4-2026/nda (needs python-docx)."""
import re
import sys
from docx import Document
from docx.enum.table import WD_TABLE_ALIGNMENT
from docx.enum.text import WD_ALIGN_PARAGRAPH
from docx.oxml import OxmlElement
from docx.oxml.ns import qn
from docx.shared import Cm, Pt, RGBColor

NAVY = RGBColor(0x10, 0x18, 0x41)


def add_runs(p, text):
    # French typography: keep « », :, ; and ? on the same line as the word they belong to
    text = re.sub(r" ([:;?!»])", " \\1", text).replace("« ", "« ")
    # **bold** and « [placeholder] » highlighting
    for part in re.split(r"(\*\*[^*]+\*\*)", text):
        if not part:
            continue
        bold = part.startswith("**") and part.endswith("**")
        chunk = part[2:-2] if bold else part
        for sub in re.split(r"(\[[^\]]+\])", chunk):
            if not sub:
                continue
            r = p.add_run(sub)
            r.bold = bold
            if sub.startswith("[") and sub.endswith("]"):
                r.font.highlight_color = 7  # yellow: fields to fill in


def bottom_border(p):
    pPr = p._p.get_or_add_pPr()
    bdr = OxmlElement("w:pBdr")
    b = OxmlElement("w:bottom")
    for k, v in {"w:val": "single", "w:sz": "6", "w:space": "1", "w:color": "101841"}.items():
        b.set(qn(k), v)
    bdr.append(b)
    pPr.append(bdr)


def convert(src, dst, footer_text):
    lines = open(src, encoding="utf-8").read().splitlines()
    doc = Document()
    sec = doc.sections[0]
    sec.page_height, sec.page_width = Cm(29.7), Cm(21)
    for m in ("left_margin", "right_margin"):
        setattr(sec, m, Cm(2.3))
    sec.top_margin = sec.bottom_margin = Cm(2)

    normal = doc.styles["Normal"]
    normal.font.name = "Calibri"
    normal.font.size = Pt(10.5)
    normal.paragraph_format.space_after = Pt(6)
    normal.paragraph_format.line_spacing = 1.15
    for name, size in (("Title", 18), ("Heading 2", 12)):
        st = doc.styles[name]
        st.font.name = "Calibri"
        st.font.size = Pt(size)
        st.font.bold = True
        st.font.color.rgb = NAVY
    doc.styles["Heading 2"].paragraph_format.space_before = Pt(12)
    doc.styles["Heading 2"].paragraph_format.keep_with_next = True

    fp = sec.footer.paragraphs[0]
    fp.alignment = WD_ALIGN_PARAGRAPH.CENTER
    fr = fp.add_run(footer_text + "  ·  ")
    fr.font.size = Pt(8)
    # page number field
    for kind, text in (("begin", None), (None, "PAGE"), ("end", None)):
        r = fp.add_run()
        r.font.size = Pt(8)
        if kind:
            el = OxmlElement("w:fldChar")
            el.set(qn("w:fldCharType"), kind)
        else:
            el = OxmlElement("w:instrText")
            el.set(qn("xml:space"), "preserve")
            el.text = text
        r._r.append(el)

    i = 0
    while i < len(lines):
        line = lines[i].rstrip()
        if not line:
            i += 1
            continue
        if line.startswith("# "):
            p = doc.add_paragraph(style="Title")
            add_runs(p, line[2:])
            bottom_border(p)
        elif line.startswith("## "):
            doc.add_heading(line[3:], level=2)
        elif line == "---":
            bottom_border(doc.add_paragraph())
        elif line.startswith("| "):
            rows = []
            while i < len(lines) and lines[i].startswith("|"):
                cells = [c.strip() for c in lines[i].strip().strip("|").split("|")]
                if not all(re.fullmatch(r"-+", c) for c in cells):
                    rows.append(cells)
                i += 1
            t = doc.add_table(rows=0, cols=len(rows[0]))
            t.alignment = WD_TABLE_ALIGNMENT.CENTER
            for ri, row in enumerate(rows):
                cells = t.add_row().cells
                for c, text in zip(cells, row):
                    c.width = Cm(8.2)
                    p = c.paragraphs[0]
                    add_runs(p, f"**{text}**" if ri == 0 else text)
                    if ri == len(rows) - 1:  # signature line: leave room
                        for _ in range(3):
                            c.add_paragraph()
            continue
        elif line.startswith("- "):
            p = doc.add_paragraph(style="List Bullet")
            add_runs(p, line[2:])
        else:
            p = doc.add_paragraph()
            if re.match(r"^\(?[a-i]\)", line):
                p.paragraph_format.left_indent = Cm(0.8)
            add_runs(p, line)
            if line.startswith("**ENTRE**") or line.startswith("**BETWEEN**") or line.startswith("**ET**") or line.startswith("**AND**"):
                p.paragraph_format.space_before = Pt(6)
        i += 1
    doc.core_properties.title = lines[0].lstrip("# ").strip()
    doc.core_properties.author = "SecuraOps"
    doc.save(dst)
    print("wrote", dst)


if __name__ == "__main__":
    base = sys.argv[1]
    convert(f"{base}/nda-health-check-fr.md", f"{base}/nda-health-check-fr.docx",
            "Accord de confidentialité – Register Health Check · SecuraOps")
    convert(f"{base}/nda-health-check-en.md", f"{base}/nda-health-check-en.docx",
            "Confidentiality Agreement – Register Health Check · SecuraOps")
