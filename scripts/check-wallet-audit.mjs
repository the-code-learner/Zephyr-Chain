import { execFileSync } from "node:child_process";

const ALLOWED_ADVISORY = "GHSA-vfj7-8cjw-p6xm";
const ALLOWED_ROOT_PACKAGE = "braces";
const EXPIRES_AT = Date.parse("2026-11-16T00:00:00Z");

let raw = "";
try {
  raw = execFileSync("npm", ["audit", "--json"], {
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  });
} catch (error) {
  raw = String(error.stdout || "");
  if (!raw.trim()) {
    process.stderr.write(String(error.stderr || error));
    process.exit(1);
  }
}

let report;
try {
  report = JSON.parse(raw);
} catch (error) {
  console.error("npm audit returned invalid JSON:", error);
  process.exit(1);
}

const vulnerabilities = report.vulnerabilities || {};
const blocking = Object.entries(vulnerabilities)
  .filter(([, value]) => value.severity === "high" || value.severity === "critical")
  .map(([name]) => name);

if (blocking.length === 0) {
  console.log("npm audit: no high or critical vulnerabilities.");
  process.exit(0);
}

const root = vulnerabilities[ALLOWED_ROOT_PACKAGE];
const rootVia = Array.isArray(root?.via) ? root.via : [];
const rootHasAllowedAdvisory = rootVia.some((entry) => {
  if (!entry || typeof entry === "string") return false;
  return String(entry.url || "").includes(ALLOWED_ADVISORY);
});

if (!rootHasAllowedAdvisory) {
  console.error(`Expected temporary audit exception ${ALLOWED_ADVISORY} was not the root cause.`);
  process.exit(1);
}
const rootFix = root.fixAvailable;
if (rootFix !== false && !(rootFix && typeof rootFix === "object" && rootFix.isSemVerMajor === true)) {
  console.error(`${ALLOWED_ADVISORY} now has a non-breaking npm fix path; remove the exception and update dependencies.`);
  process.exit(1);
}
if (Date.now() >= EXPIRES_AT) {
  console.error(`Temporary audit exception ${ALLOWED_ADVISORY} expired on 2026-11-16.`);
  process.exit(1);
}

const allowed = new Set();
let changed = true;
while (changed) {
  changed = false;
  for (const name of blocking) {
    if (allowed.has(name)) continue;
    const value = vulnerabilities[name];
    const via = Array.isArray(value?.via) ? value.via : [];
    if (via.length === 0) continue;
    const allViaAllowed = via.every((entry) => {
      if (typeof entry === "string") return allowed.has(entry);
      if (!entry) return false;
      return String(entry.url || "").includes(ALLOWED_ADVISORY);
    });
    if (allViaAllowed) {
      allowed.add(name);
      changed = true;
    }
  }
}

const unexpected = blocking.filter((name) => !allowed.has(name));
if (unexpected.length > 0) {
  console.error("npm audit found unexpected high/critical vulnerabilities:");
  for (const name of unexpected) {
    const value = vulnerabilities[name];
    console.error(`- ${name}: ${value.severity}; fixAvailable=${JSON.stringify(value.fixAvailable)}`);
  }
  process.exit(1);
}

console.warn(
  `npm audit: allowing only ${ALLOWED_ADVISORY} (unpatched braces dev-tooling advisory) until 2026-11-16.`
);
console.warn(`Affected audit nodes: ${[...allowed].sort().join(", ")}`);
