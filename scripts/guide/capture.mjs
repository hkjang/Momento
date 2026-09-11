// Takes the guide screenshots from a running deployment that seed.mjs filled.
// Desktop 1440x900, headless Chrome, every screen after its data has loaded.
//
//   MOMENTO_GUIDE_BASE_URL=http://127.0.0.1:8080 \
//   MOMENTO_GUIDE_ADMIN_EMAIL=... MOMENTO_GUIDE_ADMIN_PASSWORD=... node capture.mjs [name ...]
//
// MOMENTO_GUIDE_CHROME points at the browser binary (default: google-chrome).
// Files land in docs/assets/guide/<name>.png.

import { mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import puppeteer from "puppeteer-core";
import { SITE_NAME, readTarget } from "./common.mjs";

const target = readTarget();
const outDir = join(dirname(fileURLToPath(import.meta.url)), "..", "..", "docs", "assets", "guide");
mkdirSync(outDir, { recursive: true });
const only = process.argv.slice(2);

const browser = await puppeteer.launch({
  executablePath: process.env.MOMENTO_GUIDE_CHROME || "/usr/bin/google-chrome",
  headless: true,
  args: ["--no-sandbox", "--disable-gpu", "--hide-scrollbars", "--lang=ko-KR", "--font-render-hinting=none"],
});
const page = await browser.newPage();
await page.setViewport({ width: 1440, height: 900, deviceScaleFactor: 1 });
await page.setExtraHTTPHeaders({ "Accept-Language": "ko-KR,ko;q=0.9" });

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// Loading done means: no skeleton, no spinner, no "…하는 중" label, and the
// network quiet. A screen that is still fetching is not the screen the reader
// will see.
async function settled(extraMs = 800) {
  await page.waitForNetworkIdle({ idleTime: 600, timeout: 60_000 }).catch(() => {});
  await page
    .waitForFunction(
      () =>
        !document.querySelector(".MuiSkeleton-root, .MuiCircularProgress-root, .MuiLinearProgress-root") &&
        !/하는 중|불러오는 중|계산하는 중/.test(document.body.innerText),
      { timeout: 60_000 },
    )
    .catch(() => {});
  await sleep(extraMs);
}

async function clickText(selector, text) {
  const handles = await page.$$(selector);
  for (const handle of handles) {
    const label = (await handle.evaluate((el) => el.textContent || "")).trim();
    if (label === text || label.includes(text)) {
      await handle.click();
      return true;
    }
  }
  throw new Error(`"${text}" 를 찾지 못했습니다 (${selector})`);
}

async function shoot(name, { path, fullPage = false, before, after } = {}) {
  if (only.length && !only.includes(name)) return;
  if (before) await before();
  if (path) {
    await page.goto(`${target.baseURL}${path}`, { waitUntil: "domcontentloaded" });
    await settled();
  }
  if (after) {
    await after();
    await settled();
  }
  await page.screenshot({ path: join(outDir, `${name}.png`), fullPage });
  console.log("captured", name);
}

// ------------------------------------------------------------------ login
await shoot("login", {
  path: "/login",
  after: async () => {
    await page.type('input[type="email"], input[name="email"]', "admin@example.com").catch(() => {});
  },
});

await page.goto(`${target.baseURL}/login`, { waitUntil: "domcontentloaded" });
await settled(200);
await page.evaluate(() => {
  document.querySelectorAll("input").forEach((el) => {
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set;
    setter.call(el, "");
    el.dispatchEvent(new Event("input", { bubbles: true }));
  });
});
const inputs = await page.$$("input");
await inputs[0].type(target.email);
await inputs[1].type(target.password);
await inputs[1].press("Enter");
await page.waitForFunction(() => !location.pathname.startsWith("/login"), { timeout: 30_000 });
await settled();

// Pin the demo site so every screen reads the same data.
const sites = await (await fetch(`${target.baseURL}/api/v1/sites`, { headers: { cookie: (await page.cookies()).map((c) => `${c.name}=${c.value}`).join("; ") } })).json();
const demo = sites.find((s) => s.name === SITE_NAME);
if (!demo) {
  console.error(`${SITE_NAME} 사이트가 없습니다. 먼저 seed.mjs 를 실행하세요.`);
  process.exit(1);
}
await page.evaluate((key) => {
  localStorage.setItem("momento:selected-site", key);
  localStorage.setItem("momento:selected-environment", "prd");
}, demo.site_id);

// ------------------------------------------------------------------ user screens
await shoot("overview", { path: "/" });
await shoot("overview-full", { path: "/", fullPage: true });
await shoot("visitor-insights", { path: "/visitor-insights" });
await shoot("visitor-insights-full", { path: "/visitor-insights", fullPage: true });
await shoot("acquisition", { path: "/acquisition" });
await shoot("pages", { path: "/pages" });
await shoot("events", { path: "/events" });
await shoot("sessions", { path: "/sessions" });
await shoot("user-explorer-search", {
  path: "/user-explorer",
  after: async () => {
    await page.type('input[placeholder*="EMP001"]', "EMP0001");
    await page.keyboard.press("Enter");
  },
});
await shoot("user-explorer-timeline", {
  after: async () => {
    await clickText("button", "추적").catch(async () => {
      await clickText("a, button", "EMP0001");
    });
  },
});
await shoot("usage", { path: "/usage" });
await shoot("adoption", { path: "/adoption" });
await shoot("features", { path: "/features" });
await shoot("cohort", { path: "/cohort" });
await shoot("goals", { path: "/goals" });
await shoot("explorer", {
  path: "/explorer",
  after: async () => {
    await clickText("button", "실행").catch(() => {});
  },
});
await shoot("segments", { path: "/segments" });
await shoot("funnel", {
  path: "/funnel",
  fullPage: true,
  after: async () => {
    await clickText("button", "분석");
  },
});
await shoot("path", { path: "/path", fullPage: true });
await shoot("experience", { path: "/experience" });
await shoot("search-analytics", { path: "/search-analytics" });
await shoot("frustration", { path: "/frustration" });
await shoot("insights", { path: "/insights" });
await shoot("ai-analytics", { path: "/ai-analytics" });
await shoot("data-quality", { path: "/data-quality" });
await shoot("workspace", { path: "/workspace" });
await shoot("change-calendar", { path: "/change-calendar" });
await shoot("ecommerce", { path: "/ecommerce" });
await shoot("realtime", { path: "/realtime" });
await shoot("command-palette", {
  path: "/",
  after: async () => {
    await page.keyboard.down("Control");
    await page.keyboard.press("KeyK");
    await page.keyboard.up("Control");
    await sleep(500);
    await page.keyboard.type("개인정보");
  },
});
await shoot("profile", { path: "/profile" });

// ------------------------------------------------------------------ admin screens
await shoot("admin-home", { path: "/admin" });
await shoot("admin-sites", { path: "/admin?section=sites" });
await shoot("admin-sites-sdk", {
  after: async () => {
    await clickText("button", "SDK 설치").catch(() => clickText("button", "설치"));
  },
});
await shoot("admin-settings", { path: "/admin?section=settings" });
await shoot("admin-privacy", { path: "/admin?section=privacy" });
await shoot("admin-retention", { path: "/admin?section=retention" });
await shoot("admin-networks", { path: "/admin?section=networks" });
await shoot("admin-users", { path: "/admin?section=users" });
await shoot("admin-schemas", { path: "/admin?section=schemas" });
await shoot("admin-dimensions", { path: "/admin?section=dimensions" });
await shoot("admin-debugger", { path: "/admin?section=debugger" });
await shoot("admin-audit", { path: "/admin?section=audit" });
await shoot("admin-governance", { path: "/admin/governance" });
await shoot("admin-automation", { path: "/admin/automation" });
await shoot("admin-analytics-engineering", { path: "/admin/analytics-engineering" });
await shoot("admin-privacy-requests", { path: "/admin/privacy-requests" });
await shoot("admin-product-lab", { path: "/admin/product-lab" });

await browser.close();
