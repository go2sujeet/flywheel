#!/usr/bin/env node
"use strict";

// termshot.mjs <input.txt> <output.svg> [--title <t>]
//
// Renders a plain-text terminal transcript as a deterministic SVG window:
// dark background, rounded corners, three title-bar dots, monospace text,
// width sized to the longest line, lines XML-escaped. Lines starting with
// "$ " are prompt lines and get a subtle highlight. No dependencies and no
// randomness: identical input yields identical bytes.

import fs from "fs";

const FONT = "ui-monospace,SFMono-Regular,Menlo,Consolas,monospace";
const CHAR_W = 8.4; // per-column advance at font-size 13
const LINE_H = 20; // row height
const FONT_SIZE = 13;
const PAD_X = 20;
const PAD_Y = 18;
const TITLE_H = 36; // window title bar
const DOT_R = 6;
const DOT_GAP = 22;
const BG = "#0d1117";
const ROW_HL = "#161b22";
const FG = "#e6edf3";
const PROMPT = "#3fb950";
const TITLE_FG = "#8b949e";
const DOTS = ["#ff5f57", "#febc2e", "#28c840"];

function usage() {
  console.error("usage: termshot.mjs <input.txt> <output.svg> [--title <t>]");
  process.exit(2);
}

const argv = process.argv.slice(2);
let title = "";
let input = null;
let output = null;
for (let i = 0; i < argv.length; i++) {
  if (argv[i] === "--title") {
    title = argv[++i] || "";
  } else if (input === null) {
    input = argv[i];
  } else if (output === null) {
    output = argv[i];
  } else {
    usage();
  }
}
if (input === null || output === null) usage();

const text = fs.readFileSync(input, "utf8");
const lines = text.replace(/\r\n/g, "\n").replace(/\n+$/, "").split("\n");

const maxLen = lines.reduce((m, l) => Math.max(m, l.length), 0);
const width = Math.ceil(PAD_X * 2 + maxLen * CHAR_W);
const height = Math.ceil(PAD_Y * 2 + TITLE_H + lines.length * LINE_H);
const dotsX = PAD_X + DOT_R;
const dotCy = Math.round(TITLE_H / 2);
const textX = PAD_X;

function esc(s) {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

const out = [];
out.push(
  `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}" role="img" aria-label="${esc(title)}">`
);
out.push(`  <rect x="0" y="0" width="${width}" height="${height}" rx="12" fill="${BG}"/>`);
out.push(`  <rect x="0" y="0" width="${width}" height="${TITLE_H}" fill="${BG}"/>`);
for (let i = 0; i < 3; i++) {
  out.push(`  <circle cx="${dotsX + i * DOT_GAP}" cy="${dotCy}" r="${DOT_R}" fill="${DOTS[i]}"/>`);
}
if (title) {
  out.push(
    `  <text x="${width / 2}" y="${dotCy + 4}" text-anchor="middle" font-family="${FONT}" font-size="12" fill="${TITLE_FG}">${esc(title)}</text>`
  );
}
for (let i = 0; i < lines.length; i++) {
  const line = lines[i];
  const y = PAD_Y + TITLE_H + (i + 1) * LINE_H - 5;
  out.push(`  <text x="${textX}" y="${y}" font-family="${FONT}" font-size="${FONT_SIZE}" fill="${FG}">`);
  if (line.startsWith("$ ")) {
    const hlW = line.length * CHAR_W + 6;
    out.push(`    <rect x="${textX - 3}" y="${y - LINE_H + 3}" width="${hlW}" height="${LINE_H - 6}" rx="3" fill="${ROW_HL}"/>`);
    out.push(`    <tspan xml:space="preserve" fill="${PROMPT}">$ </tspan><tspan xml:space="preserve">${esc(line.slice(2))}</tspan>`);
  } else {
    out.push(`    <tspan xml:space="preserve">${esc(line)}</tspan>`);
  }
  out.push(`  </text>`);
}
out.push(`</svg>`);

fs.writeFileSync(output, out.join("\n") + "\n", "utf8");