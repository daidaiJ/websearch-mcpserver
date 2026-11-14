import fs from "node:fs";
import { getDocument } from "pdfjs-dist/legacy/build/pdf.mjs";

const path = process.argv[2];
const data = new Uint8Array(fs.readFileSync(path));
const doc = await getDocument({ data, disableWorker: true, isEvalSupported: false }).promise;
let text = "";
for (let i = 1; i <= doc.numPages; i++) {
  const page = await doc.getPage(i);
  const content = await page.getTextContent();
  text += content.items.map((it) => ("str" in it ? it.str : "")).join(" ") + "\n";
}
if (!text.trim()) {
  console.error("no text");
  process.exit(2);
}
process.stdout.write(text);
