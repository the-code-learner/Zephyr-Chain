import { execFileSync } from "node:child_process";

// Security-sensitive CI gate. Reject every current high/critical advisory.
// A temporary GHSA exception used during development is intentionally gone:
// a withdrawn advisory must never suppress a different future vulnerability.
let raw;
try {
  raw = execFileSync("npm", ["audit", "--json"], {
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  });
} catch (error) {
  raw = typeof error.stdout === "string" ? error.stdout : "";
  if (!raw.trim()) {
    console.error("npm audit could not produce a report:", String(error.stderr || error));
    process.exit(1);
  }
}

let report;
try {
  report = JSON.parse(raw);
} catch (error) {
  console.error("npm audit produced invalid JSON:", error);
  process.exit(1);
}
if (!report || typeof report !== "object" || report.error ||
    !report.metadata || !report.vulnerabilities ||
    typeof report.vulnerabilities !== "object" || Array.isArray(report.vulnerabilities)) {
  console.error("npm audit returned an incomplete or erroneous report.");
  process.exit(1);
}

const blocking = Object.entries(report.vulnerabilities)
  .filter(([, detail]) => detail && (detail.severity === "high" || detail.severity === "critical"));

if (blocking.length > 0) {
  console.error("npm audit found high/critical vulnerabilities:");
  for (const [name, detail] of blocking) {
    console.error(`- ${name}: ${detail.severity}; fixAvailable=${JSON.stringify(detail.fixAvailable)}`);
  }
  process.exit(1);
}

console.log("npm audit: no high/critical vulnerabilities (no exceptions).");
