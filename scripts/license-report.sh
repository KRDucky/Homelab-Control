#!/usr/bin/env bash
# Prints the license of every npm package in the frontend lockfile, split into
# shipped (production) and development-only, plus the Go module policy.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)/frontend/app"
# shellcheck disable=SC2016  # single quotes are intentional: this is JavaScript
node -e '
const fs=require("fs"),path=require("path");
const lock=JSON.parse(fs.readFileSync("package-lock.json"));
const rows={prod:[],dev:[]};
for (const [k,v] of Object.entries(lock.packages)) {
  if (!k) continue;
  let lic = v.license;
  if (!lic) { try { lic = JSON.parse(fs.readFileSync(path.join(k,"package.json"))).license; } catch { lic = "not installed on this platform (optional native binary)"; } }
  rows[v.dev ? "dev" : "prod"].push(`${k.replace(/^.*node_modules\//,"")}@${v.version}\t${lic}`);
}
console.log("== Shipped in the UI bundle (production dependencies)"); console.log(rows.prod.sort().join("\n"));
const counts={}; for (const r of rows.dev) { const l=r.split("\t")[1]; counts[l]=(counts[l]||0)+1; }
console.log("\n== Development-only packages by license (not shipped)");
for (const [l,n] of Object.entries(counts).sort((a,b)=>b[1]-a[1])) console.log(`${n}\t${l}`);
const strong = rows.dev.filter(r => /GPL|AGPL|SSPL|BUSL/i.test(r));
console.log("\n== Copyleft/source-available dev packages needing review:", strong.length ? "\n"+strong.join("\n") : "none");
'
echo; echo "== Go modules"; cd ../../backend && (grep -E '^\s*require|^\s+[a-z].* v[0-9]' go.mod || echo "none (standard library only)")
