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
  if (await page.locator(".component-groups").count()) throw new Error(`${name}: diagnostics groups leaked into overview`);
  if (await page.locator(".runtime-panel").count() !== 1) throw new Error(`${name}: runtime summary was not merged into the status band`);
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
  if (await page.locator(".verification-list").count() !== 1) throw new Error(`${name}: connection verification list is missing`);
  if (await page.locator(".verification-surface, .stage-flow").count()) throw new Error(`${name}: legacy verification cards were not removed`);
  if (await page.locator(".evidence-row").count() < 4) throw new Error(`${name}: active test evidence was not merged into connection rows`);
  const stageOrder = await page.locator(".verification-row").evaluateAll((rows) => rows.map((row) => row.getAttribute("data-stage")));
  if (stageOrder.join(",") !== "network,firewall,tunnel,provider,dns,vps,egress") throw new Error(`${name}: invalid architecture stage order: ${stageOrder}`);
  const configurationOrder = await page.locator(".runtime-component-row").evaluateAll((rows) => rows.map((row) => row.getAttribute("data-component-config")));
  if (configurationOrder.join(",") !== "firewall,tunnel,sing-box,dns,outbound,guard") throw new Error(`${name}: invalid component configuration order: ${configurationOrder}`);
  const commands = await page.locator(".runtime-command code").allTextContents();
  if (!commands.some((value) => value.includes("lightnode-guard firewall refresh"))) throw new Error(`${name}: PF startup command is missing`);
  if (!commands.some((value) => value.includes("sing-box run -c"))) throw new Error(`${name}: sing-box startup arguments are missing`);
  const componentGroups = await page.locator(".component-group").count();
  if (componentGroups !== 4) throw new Error(`${name}: expected 4 component groups, got ${componentGroups}`);
  const groupOrder = await page.locator(".component-group").evaluateAll((groups) => groups.map((group) => group.getAttribute("data-group")));
  if (groupOrder.join(",") !== "network,tunnel,dns,egress") throw new Error(`${name}: invalid component group order: ${groupOrder}`);
  const componentRows = await page.locator(".component-row").count();
  if (componentRows < 9) throw new Error(`${name}: expected at least 9 component rows, got ${componentRows}`);
  await assertNoOverflow(page, `${name} diagnostics`);
  if (runTests) {
    await page.getByRole("button", { name: "运行检查" }).click();
    await page.waitForResponse((response) => response.url().endsWith("/api/tests/run") && response.status() === 200);
  }
  await page.screenshot({ path: `artifacts/${name}-diagnostics.png`, fullPage: true });
  const pageText = await page.locator("body").innerText();
  if (/REPLACE_|api secret|admin password|private key/i.test(pageText)) throw new Error(`${name}: sensitive configuration appeared in the dashboard`);

  await page.getByRole("button", { name: /节点配置/ }).click();
  await page.getByRole("heading", { name: "节点配置" }).waitFor();
  const serverInput = page.getByLabel("VPS IPv4");
  await page.waitForFunction(() => Boolean(document.querySelector(".node-form-grid input")?.value));
  if (!/^\d{1,3}(\.\d{1,3}){3}$/.test(await serverInput.inputValue())) throw new Error(`${name}: current node IPv4 was not loaded`);
  const secretValues = await page.locator(".node-secret-grid input").evaluateAll((inputs) => inputs.map((input) => input.value));
  if (secretValues.length !== 3 || secretValues.some(Boolean)) throw new Error(`${name}: secret values were returned to the browser`);
  if (await page.locator(".node-secret-grid .configured").count() !== 3) throw new Error(`${name}: secret configured flags are incomplete`);
  await assertNoOverflow(page, `${name} node configuration`);
  await page.screenshot({ path: `artifacts/${name}-node.png`, fullPage: true });

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
