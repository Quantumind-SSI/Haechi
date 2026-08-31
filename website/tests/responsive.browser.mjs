import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { constants } from "node:fs";
import { access, mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createHaechiServer } from "../server.mjs";

const requestTimeoutMs = 5_000;
const startupTimeoutMs = 10_000;
const mobileViewports = [
  { width: 320, height: 568 },
  { width: 390, height: 844 },
  { width: 667, height: 320 },
  { width: 768, height: 1_024 },
  { width: 780, height: 1_024 },
];

class DevToolsClient {
  constructor(socket) {
    this.socket = socket;
    this.nextId = 0;
    this.pending = new Map();
    socket.addEventListener("message", (event) => this.handleMessage(event));
    socket.addEventListener("close", () => this.rejectPending());
  }

  static async connect(url) {
    assert.equal(
      typeof WebSocket,
      "function",
      "Node must run with --experimental-websocket",
    );
    const socket = new WebSocket(url);
    try {
      await waitForSocket(socket);
      return new DevToolsClient(socket);
    } catch (error) {
      socket.close();
      throw error;
    }
  }

  handleMessage(event) {
    const message = JSON.parse(String(event.data));
    const pending = this.pending.get(message.id);
    if (!pending) return;
    this.pending.delete(message.id);
    clearTimeout(pending.timer);
    if (message.error) pending.reject(new Error(message.error.message));
    else pending.resolve(message.result);
  }

  rejectPending() {
    for (const request of this.pending.values()) {
      clearTimeout(request.timer);
      request.reject(new Error("Chrome DevTools connection closed"));
    }
    this.pending.clear();
  }

  send(method, params = {}, sessionId) {
    return new Promise((resolve, reject) => {
      const id = ++this.nextId;
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`Chrome DevTools command timed out: ${method}`));
      }, requestTimeoutMs);
      this.pending.set(id, { resolve, reject, timer });
      this.socket.send(JSON.stringify({ id, method, params, sessionId }));
    });
  }

  close() {
    this.socket.close();
  }
}

function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

function waitForSocket(socket) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(
      () => reject(new Error("Chrome DevTools WebSocket timed out")),
      requestTimeoutMs,
    );
    socket.addEventListener(
      "open",
      () => {
        clearTimeout(timer);
        resolve();
      },
      { once: true },
    );
    socket.addEventListener(
      "error",
      () => {
        clearTimeout(timer);
        reject(new Error("Chrome DevTools WebSocket failed"));
      },
      { once: true },
    );
  });
}

async function findChrome() {
  const candidates = [
    process.env.CHROME_PATH,
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    "/usr/bin/google-chrome",
    "/opt/google/chrome/chrome",
    "/usr/bin/chromium",
    "/usr/bin/chromium-browser",
  ].filter(Boolean);
  for (const candidate of candidates) {
    try {
      await access(candidate, constants.X_OK);
      return candidate;
    } catch (error) {
      if (error.code !== "ENOENT" && error.code !== "EACCES") throw error;
    }
  }
  throw new Error("Chrome not found; set CHROME_PATH to its executable");
}

function waitForDebugger(browser) {
  return new Promise((resolve, reject) => {
    let output = "";
    const finish = (error, url) => {
      clearTimeout(timer);
      browser.stderr.off("data", onData);
      browser.off("exit", onExit);
      browser.stderr.resume();
      if (error) reject(error);
      else resolve(url);
    };
    const onData = (chunk) => {
      output += String(chunk);
      const match = output.match(/DevTools listening on (ws:\/\/\S+)/);
      if (match) finish(undefined, match[1]);
    };
    const onExit = (code) =>
      finish(new Error(`Chrome exited before startup with code ${code}`));
    const timer = setTimeout(
      () => finish(new Error("Chrome did not expose DevTools in time")),
      startupTimeoutMs,
    );
    browser.stderr.on("data", onData);
    browser.once("exit", onExit);
  });
}

async function startBrowser(profileDirectory) {
  const executable = await findChrome();
  const browser = spawn(
    executable,
    [
      "--headless=new",
      "--no-sandbox",
      "--disable-dev-shm-usage",
      "--disable-background-timer-throttling",
      "--disable-renderer-backgrounding",
      "--remote-debugging-port=0",
      `--user-data-dir=${profileDirectory}`,
      "about:blank",
    ],
    { stdio: ["ignore", "ignore", "pipe"] },
  );
  try {
    const debuggerUrl = await waitForDebugger(browser);
    return { browser, debuggerUrl };
  } catch (error) {
    await stopBrowser(browser);
    throw error;
  }
}

async function stopBrowser(browser) {
  if (!browser || browser.exitCode !== null) return;
  browser.kill("SIGTERM");
  for (
    let attempt = 0;
    attempt < 20 && browser.exitCode === null;
    attempt += 1
  ) {
    await delay(50);
  }
  if (browser.exitCode === null) browser.kill("SIGKILL");
}

async function startServer(dataDirectory) {
  const server = createHaechiServer({ dataDir: dataDirectory });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  return server;
}

async function closeServer(server) {
  if (!server) return;
  await new Promise((resolve, reject) => {
    server.close((error) => (error ? reject(error) : resolve()));
  });
}

async function createPage(client) {
  const { targetId } = await client.send("Target.createTarget", {
    url: "about:blank",
  });
  const { sessionId } = await client.send("Target.attachToTarget", {
    targetId,
    flatten: true,
  });
  await client.send("Page.enable", {}, sessionId);
  await client.send("Runtime.enable", {}, sessionId);
  await client.send("Network.enable", {}, sessionId);
  await client.send(
    "Network.setCacheDisabled",
    { cacheDisabled: true },
    sessionId,
  );
  await client.send("Target.activateTarget", { targetId });
  return sessionId;
}

async function evaluate(client, sessionId, expression) {
  const response = await client.send(
    "Runtime.evaluate",
    { expression, awaitPromise: true, returnByValue: true },
    sessionId,
  );
  assert.equal(
    response.exceptionDetails,
    undefined,
    "browser evaluation failed",
  );
  return response.result.value;
}

async function loadViewport(client, sessionId, baseUrl, width, height) {
  const mobile = width <= 780;
  await client.send(
    "Emulation.setDeviceMetricsOverride",
    { width, height, deviceScaleFactor: 2, mobile },
    sessionId,
  );
  await client.send(
    "Emulation.setTouchEmulationEnabled",
    { enabled: mobile, maxTouchPoints: mobile ? 5 : 1 },
    sessionId,
  );
  await client.send(
    "Page.navigate",
    { url: `${baseUrl}/?viewport=${width}x${height}&run=${Date.now()}` },
    sessionId,
  );
  for (let attempt = 0; attempt < 50; attempt += 1) {
    const ready = await evaluate(
      client,
      sessionId,
      `document.readyState === "complete" && document.documentElement.classList.contains("js")`,
    );
    if (ready) return;
    await delay(100);
  }
  throw new Error(`page did not load at ${width}x${height}`);
}

const mobileAuditExpression = `(async () => {
  const rect = (element) => {
    const value = element.getBoundingClientRect();
    return { x: value.x, right: value.right, width: value.width, height: value.height };
  };
  const toggle = document.querySelector("[data-menu-toggle]");
  const navigation = document.querySelector("[data-navigation]");
  const main = document.querySelector("main");
  const closed = rect(navigation);
  toggle.click();
  await new Promise((resolve) => requestAnimationFrame(resolve));
  const slide = navigation.getAnimations().find((animation) =>
    animation.effect.getKeyframes().some((frame) => frame.transform),
  );
  const slideDuration = slide?.effect.getComputedTiming().duration || 0;
  navigation.getAnimations().forEach((animation) => animation.finish());
  const open = rect(navigation);
  const links = [...navigation.querySelectorAll("a")].map(rect);

  const controls = [...new Set([
    toggle,
    ...navigation.querySelectorAll("a"),
    ...document.querySelectorAll(".button, .text-link, [role='tab']"),
    document.querySelector(".consent"),
    ...document.querySelectorAll(".footer-links a"),
  ])];
  const undersized = controls
    .map((element) => ({ label: element.textContent.trim().replace(/\\s+/g, " "), ...rect(element) }))
    .filter((control) => control.width < 44 || control.height < 44);

  const lastLink = navigation.querySelector("a:last-child");
  lastLink.focus();
  document.dispatchEvent(new KeyboardEvent("keydown", { key: "Tab", bubbles: true, cancelable: true }));
  const focusTrapped = document.activeElement === toggle;
  document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
  navigation.getAnimations().forEach((animation) => animation.finish());

  return {
    closed,
    open,
    links,
    slideDuration,
    undersized,
    focusTrapped,
    escapeClosed: toggle.getAttribute("aria-expanded") === "false",
    focusRestored: document.activeElement === toggle,
    backgroundRestored: !main.inert,
    overflowY: getComputedStyle(navigation).overflowY,
    scrollHeight: navigation.scrollHeight,
    clientHeight: navigation.clientHeight,
    orientation: document.querySelector("[role='tablist']").getAttribute("aria-orientation"),
  };
})()`;

async function auditMobileViewports(client, sessionId, baseUrl) {
  for (const viewport of mobileViewports) {
    await loadViewport(
      client,
      sessionId,
      baseUrl,
      viewport.width,
      viewport.height,
    );
    const result = await evaluate(client, sessionId, mobileAuditExpression);
    assert.equal(Math.round(result.closed.x), viewport.width);
    assert.equal(
      Math.round(result.open.x),
      0,
      `menu did not open at ${viewport.width}px: ${JSON.stringify(result)}`,
    );
    assert.equal(Math.round(result.open.right), viewport.width);
    assert.equal(Math.round(result.open.width), viewport.width);
    assert.ok(result.slideDuration >= 200, "menu must animate horizontally");
    assert.ok(
      result.links.every(
        (link) =>
          link.x >= 0 &&
          link.right <= viewport.width &&
          link.width > 0 &&
          link.height >= 44,
      ),
    );
    assert.deepEqual(result.undersized, []);
    assert.equal(result.focusTrapped, true);
    assert.equal(result.escapeClosed, true);
    assert.equal(result.focusRestored, true);
    assert.equal(result.backgroundRestored, true);
    assert.equal(result.overflowY, "auto");
    assert.equal(result.orientation, "vertical");
    if (viewport.height === 320) {
      assert.ok(
        result.scrollHeight > result.clientHeight,
        "short menu must scroll",
      );
    }
  }
}

async function auditArchitectureKeyboard(client, sessionId) {
  const result = await evaluate(
    client,
    sessionId,
    `(() => {
      const first = document.querySelector("[role='tab']");
      first.focus();
      first.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true, cancelable: true }));
      const selected = document.querySelector("[aria-selected='true']");
      return {
        stage: selected.dataset.stage,
        focused: document.activeElement === selected,
        tabStops: document.querySelectorAll("[role='tab'][tabindex='0']").length,
        labelledBy: document.querySelector("[role='tabpanel']").getAttribute("aria-labelledby"),
        selectedId: selected.id,
      };
    })()`,
  );
  assert.equal(result.stage, "capability");
  assert.equal(result.focused, true);
  assert.equal(result.tabStops, 1);
  assert.equal(result.labelledBy, result.selectedId);
}

function contrastRatio(foreground, background) {
  const parse = (colour) => colour.match(/\d+/g).slice(0, 3).map(Number);
  const luminance = (colour) => {
    const channels = parse(colour).map((channel) => {
      const value = channel / 255;
      return value <= 0.04045
        ? value / 12.92
        : ((value + 0.055) / 1.055) ** 2.4;
    });
    return channels[0] * 0.2126 + channels[1] * 0.7152 + channels[2] * 0.0722;
  };
  const values = [luminance(foreground), luminance(background)].sort(
    (left, right) => right - left,
  );
  return (values[0] + 0.05) / (values[1] + 0.05);
}

async function auditDesktop(client, sessionId, baseUrl) {
  for (const width of [781, 1_024]) {
    await loadViewport(client, sessionId, baseUrl, width, 900);
    const result = await evaluate(
      client,
      sessionId,
      `(() => {
        const toggle = document.querySelector("[data-menu-toggle]");
        const navigation = document.querySelector("[data-navigation]");
        const buttons = [...document.querySelectorAll(".button")];
        const style = getComputedStyle(buttons[0]);
        return {
          toggleDisplay: getComputedStyle(toggle).display,
          navigationPosition: getComputedStyle(navigation).position,
          buttonHeights: buttons.map((button) => button.getBoundingClientRect().height),
          foreground: style.color,
          background: style.backgroundColor,
          orientation: document.querySelector("[role='tablist']").getAttribute("aria-orientation"),
        };
      })()`,
    );
    assert.equal(result.toggleDisplay, "none");
    assert.equal(result.navigationPosition, "static");
    assert.ok(result.buttonHeights.every((height) => height >= 44));
    assert.ok(contrastRatio(result.foreground, result.background) >= 4.5);
    assert.equal(result.orientation, "horizontal");
  }
}

async function auditReducedMotion(client, sessionId, baseUrl) {
  await client.send(
    "Emulation.setEmulatedMedia",
    { features: [{ name: "prefers-reduced-motion", value: "reduce" }] },
    sessionId,
  );
  await loadViewport(client, sessionId, baseUrl, 390, 844);
  const timing = await evaluate(
    client,
    sessionId,
    `(() => {
      const style = getComputedStyle(document.querySelector("[data-navigation]"));
      return { duration: style.transitionDuration, delay: style.transitionDelay };
    })()`,
  );
  const maximumDuration = Math.max(
    ...timing.duration.split(",").map((value) => Number.parseFloat(value)),
  );
  const maximumDelay = Math.max(
    ...timing.delay.split(",").map((value) => Number.parseFloat(value)),
  );
  assert.ok(
    maximumDuration <= 0.00001,
    "reduced motion must remove menu travel",
  );
  assert.equal(maximumDelay, 0, "reduced motion must not delay visibility");
}

async function run() {
  const temporaryDirectory = await mkdtemp(
    join(tmpdir(), "haechi-responsive-"),
  );
  let browser;
  let server;
  let client;
  try {
    server = await startServer(join(temporaryDirectory, "data"));
    const baseUrl = `http://127.0.0.1:${server.address().port}`;
    const launched = await startBrowser(join(temporaryDirectory, "chrome"));
    browser = launched.browser;
    client = await DevToolsClient.connect(launched.debuggerUrl);
    const sessionId = await createPage(client);
    await auditMobileViewports(client, sessionId, baseUrl);
    await auditArchitectureKeyboard(client, sessionId);
    await auditDesktop(client, sessionId, baseUrl);
    await auditReducedMotion(client, sessionId, baseUrl);
    console.log(
      "Responsive interaction audit passed for phone, tablet, and desktop.",
    );
  } finally {
    client?.close();
    await stopBrowser(browser);
    await closeServer(server);
    await rm(temporaryDirectory, { recursive: true, force: true });
  }
}

run().catch((error) => {
  console.error(error.stack || error.message);
  process.exitCode = 1;
});
