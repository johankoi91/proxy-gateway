const { chromium } = require("playwright");

const baseURL = process.env.GUARD_URL || "http://127.0.0.1:9180";
const username = process.env.GUARD_USER || "admin";
const password = process.env.GUARD_PASSWORD || "local-test-password";
const artifactPrefix = process.env.GUARD_ARTIFACT_PREFIX || "guard";
const minActiveConnections = Number(process.env.GUARD_MIN_ACTIVE || 5);
const runTests = process.env.GUARD_RUN_TESTS !== "false";

async function verify(browser, name, viewport) {
  const context = await browser.newContext({ viewport, httpCredentials: { username, password } });
  const page = await context.newPage();
  const errors = [];
  page.on("console", (message) => message.type() === "error" && errors.push(message.text()));
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(baseURL, { waitUntil: "networkidle" });
  await page.locator(".status-headline").waitFor();
  if (await page.locator(".component-table").count()) throw new Error(`${name}: diagnostics table leaked into overview`);
  const attentionCount = await page.locator(".attention-row").count();
  const allClearCount = await page.locator(".all-clear").count();
  if (attentionCount < 1 && allClearCount < 1) throw new Error(`${name}: expected alerts or an all-clear state`);
  await assertNoOverflow(page, `${name} overview`);
  await page.screenshot({ path: `artifacts/${name}-overview.png`, fullPage: true });

  await page.getByRole("button", { name: /连接观察/ }).click();
  await page.getByRole("heading", { name: "连接观察" }).waitFor();
  const activePolicyRows = await page.locator(".active-policy-table tbody tr").count();
  if (activePolicyRows < minActiveConnections) throw new Error(`${name}: expected V2 active connections, got ${activePolicyRows}`);
  await assertNoOverflow(page, `${name} connections`);
  await page.screenshot({ path: `artifacts/${name}-connections.png`, fullPage: true });

  await page.getByRole("button", { name: /系统检查/ }).click();
  await page.getByRole("heading", { name: "系统检查" }).waitFor();
  const componentRows = await page.locator(".component-table tbody tr").count();
  if (componentRows < 9) throw new Error(`${name}: expected at least 9 component rows, got ${componentRows}`);
  await assertNoOverflow(page, `${name} diagnostics`);
  if (runTests) {
    await page.getByRole("button", { name: "运行检查" }).click();
    await page.waitForResponse((response) => response.url().endsWith("/api/tests/run") && response.status() === 200);
  }
  await page.screenshot({ path: `artifacts/${name}-diagnostics.png`, fullPage: true });

  await page.getByRole("button", { name: "事件记录" }).click();
  await page.getByRole("heading", { name: "事件记录" }).waitFor();
  if (await page.locator(".timeline-row").count() < 1) throw new Error(`${name}: expected event rows`);
  await assertNoOverflow(page, `${name} events`);
  if (errors.length) throw new Error(`${name}: browser errors: ${errors.join(" | ")}`);
  await context.close();
}

async function assertNoOverflow(page, name) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1);
  if (overflow) {
    const offenders = await page.evaluate(() => [...document.querySelectorAll("*")]
      .map((element) => ({
        name: `${element.tagName.toLowerCase()}.${element.className}`,
        left: Math.round(element.getBoundingClientRect().left),
        right: Math.round(element.getBoundingClientRect().right),
        scrollWidth: element.scrollWidth,
        clientWidth: element.clientWidth
      }))
      .filter((item) => item.right > window.innerWidth + 1 || item.left < -1)
      .slice(0, 12));
    throw new Error(`${name}: horizontal page overflow detected: ${JSON.stringify(offenders)}`);
  }
}

(async () => {
  const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROME_PATH || chromium.executablePath() });
  try {
    await verify(browser, `${artifactPrefix}-desktop`, { width: 1440, height: 1000 });
    await verify(browser, `${artifactPrefix}-mobile`, { width: 390, height: 844 });
    console.log("visual-check=ok");
  } finally {
    await browser.close();
  }
})().catch((error) => {
  console.error(error);
  process.exit(1);
});
