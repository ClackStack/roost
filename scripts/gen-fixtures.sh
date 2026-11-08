#!/usr/bin/env bash
# Regenerates internal/extract/testdata using LibreOffice and reportlab, so
# tests run against files written by real software rather than hand-made XML.
# The output is committed; only rerun this to change the fixtures.
set -euo pipefail
out="$(cd "$(dirname "$0")/.." && pwd)/internal/extract/testdata"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT

cat > "$tmp/sample.html" <<'H'
<html><head><meta charset="utf-8"><title>Project Falcon Dossier</title></head><body>
<h1>Overview</h1><p>The acquisition of Northwind closes in Q3. Café budgets are €1.2M.</p>
<h2>Risks</h2><p>Regulatory review may delay the heron milestone.</p>
<table><tr><td>Vendor</td><td>Amount</td></tr><tr><td>Acme</td><td>4200</td></tr></table>
</body></html>
H
soffice --headless --convert-to docx:"MS Word 2007 XML" --outdir "$tmp/o" "$tmp/sample.html" >/dev/null
soffice --headless --convert-to pdf --outdir "$tmp/o" "$tmp/sample.html" >/dev/null

cat > "$tmp/sample.fodp" <<'H'
<?xml version="1.0" encoding="UTF-8"?>
<office:document xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0"
 xmlns:draw="urn:oasis:names:tc:opendocument:xmlns:drawing:1.0"
 xmlns:presentation="urn:oasis:names:tc:opendocument:xmlns:presentation:1.0"
 xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0"
 xmlns:svg="urn:oasis:names:tc:opendocument:xmlns:svg-compatible:1.0"
 xmlns:style="urn:oasis:names:tc:opendocument:xmlns:style:1.0"
 office:version="1.3" office:mimetype="application/vnd.oasis.opendocument.presentation">
<office:master-styles><style:master-page style:name="Default"/></office:master-styles>
<office:body><office:presentation>
<draw:page draw:name="s1" draw:master-page-name="Default">
 <draw:frame presentation:class="title" svg:x="2cm" svg:y="1cm" svg:width="20cm" svg:height="3cm"><draw:text-box><text:p>Quarterly Strategy</text:p></draw:text-box></draw:frame>
 <draw:frame presentation:class="outline" svg:x="2cm" svg:y="5cm" svg:width="20cm" svg:height="8cm"><draw:text-box><text:p>Expand into the osprey market</text:p></draw:text-box></draw:frame>
 <presentation:notes><draw:frame presentation:class="notes" svg:x="2cm" svg:y="2cm" svg:width="16cm" svg:height="8cm"><draw:text-box><text:p>Do not mention the kestrel layoffs</text:p></draw:text-box></draw:frame></presentation:notes>
</draw:page>
<draw:page draw:name="s2" draw:master-page-name="Default">
 <draw:frame presentation:class="title" svg:x="2cm" svg:y="1cm" svg:width="20cm" svg:height="3cm"><draw:text-box><text:p>Budget Outlook</text:p></draw:text-box></draw:frame>
 <draw:frame presentation:class="outline" svg:x="2cm" svg:y="5cm" svg:width="20cm" svg:height="8cm"><draw:text-box><text:p>Harrier spending rises in 2027</text:p></draw:text-box></draw:frame>
</draw:page>
</office:presentation></office:body></office:document>
H
soffice --headless --convert-to pptx --outdir "$tmp/o" "$tmp/sample.fodp" >/dev/null

printf 'Region,Revenue\nEMEA,1200\nAPAC,3400\n' > "$tmp/book.csv"
soffice --headless --convert-to xlsx --outdir "$tmp/o" "$tmp/book.csv" >/dev/null
mv "$tmp/o/book.xlsx" "$tmp/o/sample.xlsx"

python3 - "$tmp/o/multipage.pdf" <<'P'
import sys
from reportlab.pdfgen import canvas
c = canvas.Canvas(sys.argv[1])
c.setTitle("Merlin Report"); c.setAuthor("Finance Team")
c.drawString(72, 720, "First page mentions the merlin programme.")
c.showPage()
c.drawString(72, 720, "Second page covers the goshawk contract.")
c.showPage()
c.save()
P
python3 - "$tmp/o/scanned.pdf" <<'P'
import sys
from reportlab.pdfgen import canvas
c = canvas.Canvas(sys.argv[1])
c.rect(72, 500, 200, 200, fill=1)   # a page with marks but no text layer
c.showPage(); c.save()
P
python3 - "$tmp/o/encrypted.pdf" <<'P'
import sys
from reportlab.pdfgen import canvas
from reportlab.lib import pdfencrypt
c = canvas.Canvas(sys.argv[1], encrypt=pdfencrypt.StandardEncryption("userpw", canPrint=0))
c.drawString(72, 720, "Locked falcon text")
c.showPage(); c.save()
P
cp "$tmp"/o/* "$out"/
ls -la "$out"
