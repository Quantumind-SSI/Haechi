/**
 * A minimal Chrome DevTools Protocol client, sufficient to screenshot a page.
 *
 * The first version of this renderer launched Chrome once per canvas. That is
 * 46 process launches for a full run: slow, and unreliable enough to deadlock
 * on a fresh profile. One browser held open for the whole run removes both
 * problems and lets a single page load produce the screenshot and the layout
 * measurement together, so what is measured is exactly what is captured.
 *
 * Node 22 and later expose a global `WebSocket`, so this needs no dependency.
 *
 * Every wait is bounded. A protocol command that never gets a reply rejects on
 * a timer rather than hanging the build.
 */

import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { setTimeout as delay } from "node:timers/promises";

/** Milliseconds to wait for a single protocol command to answer. */
const COMMAND_TIMEOUT_MS = 30_000;
/** Milliseconds to wait for Chrome to publish its debugging port. */
const LAUNCH_TIMEOUT_MS = 30_000;
/** Interval between port-file polls. */
const POLL_INTERVAL_MS = 50;
/** Milliseconds to wait for a navigation to reach its load event. */
const LOAD_TIMEOUT_MS = 30_000;

/**
 * Chrome flags used for every run.
 *
 * `--disable-background-networking` is deliberately absent. Combined with a
 * fresh `--user-data-dir` it makes headless Chrome hang indefinitely on
 * startup, which cost an afternoon to bisect. Do not add it back.
 *
 * `--user-data-dir` is mandatory: without it Chrome uses the operator's real
 * profile and serialises on the singleton lock.
 *
 * @param {string} profileDir Throwaway profile directory.
 * @returns {string[]}
 */
export function chromeArgs(profileDir) {
  return [
    "--headless",
    "--disable-gpu",
    "--no-first-run",
    "--no-default-browser-check",
    "--disable-extensions",
    "--hide-scrollbars",
    "--force-color-profile=srgb",
    "--remote-debugging-port=0",
    `--user-data-dir=${profileDir}`,
    "about:blank",
  ];
}

/**
 * Read the WebSocket endpoint Chrome writes into its profile directory once
 * the debugging port is listening.
 *
 * @param {string} profileDir
 * @returns {Promise<string>} A `ws://` URL.
 * @throws {Error} If the file does not appear within {@link LAUNCH_TIMEOUT_MS}.
 */
async function waitForEndpoint(profileDir) {
  const portFile = path.join(profileDir, "DevToolsActivePort");
  const deadline = Date.now() + LAUNCH_TIMEOUT_MS;
  while (Date.now() < deadline) {
    if (fs.existsSync(portFile)) {
      const lines = fs.readFileSync(portFile, "utf8").split("\n");
      if (lines.length >= 2 && lines[0].trim() !== "") {
        return `ws://127.0.0.1:${lines[0].trim()}${lines[1].trim()}`;
      }
    }
    await delay(POLL_INTERVAL_MS);
  }
  throw new Error(
    `Chrome did not publish a debugging port within ${LAUNCH_TIMEOUT_MS}ms. Check that the browser can start headless.`,
  );
}

/**
 * A connected browser. Create with {@link launch}, always release with
 * {@link Browser#close}.
 */
export class Browser {
  /**
   * @param {import("node:child_process").ChildProcess} process
   * @param {WebSocket} socket
   */
  constructor(process, socket) {
    /** @type {import("node:child_process").ChildProcess} */
    this.process = process;
    /** @type {WebSocket} */
    this.socket = socket;
    /** @type {number} */
    this.nextId = 1;
    /** @type {Map<number, {resolve: Function, reject: Function, timer: NodeJS.Timeout}>} */
    this.pending = new Map();
    /** @type {Map<string, Function[]>} */
    this.listeners = new Map();
    /** @type {string | null} */
    this.sessionId = null;
    this.socket.addEventListener("message", (event) =>
      this.receive(event.data),
    );
  }

  /**
   * Route one protocol message to its command or event handlers.
   *
   * @param {string} raw JSON text from the socket.
   * @returns {void}
   */
  receive(raw) {
    const message = JSON.parse(raw);
    if (typeof message.id === "number") {
      const slot = this.pending.get(message.id);
      if (slot === undefined) return;
      this.pending.delete(message.id);
      clearTimeout(slot.timer);
      if (message.error !== undefined) {
        slot.reject(
          new Error(`${message.error.message} (code ${message.error.code})`),
        );
        return;
      }
      slot.resolve(message.result);
      return;
    }
    for (const handler of this.listeners.get(message.method) ?? []) {
      handler(message.params);
    }
  }

  /**
   * Send one protocol command.
   *
   * @param {string} method Domain and command, e.g. `Page.navigate`.
   * @param {object} [params] Command parameters.
   * @param {boolean} [onSession] Address the attached page session rather than the browser.
   * @returns {Promise<object>} The command result.
   * @throws {Error} On a protocol error or after {@link COMMAND_TIMEOUT_MS}.
   */
  send(method, params = {}, onSession = true) {
    const id = this.nextId;
    this.nextId += 1;
    const payload = { id, method, params };
    if (onSession && this.sessionId !== null)
      payload.sessionId = this.sessionId;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`${method} timed out after ${COMMAND_TIMEOUT_MS}ms`));
      }, COMMAND_TIMEOUT_MS);
      this.pending.set(id, { resolve, reject, timer });
      this.socket.send(JSON.stringify(payload));
    });
  }

  /**
   * Register an event handler.
   *
   * @param {string} method Event name, e.g. `Page.loadEventFired`.
   * @param {Function} handler
   * @returns {() => void} Unsubscribe function.
   */
  on(method, handler) {
    const existing = this.listeners.get(method) ?? [];
    existing.push(handler);
    this.listeners.set(method, existing);
    return () => {
      const list = this.listeners.get(method) ?? [];
      const at = list.indexOf(handler);
      if (at >= 0) list.splice(at, 1);
    };
  }

  /**
   * Set the viewport, then load a URL and wait for its load event.
   *
   * @param {string} url Absolute URL, typically `file://`.
   * @param {{w: number, h: number, scale: number}} viewport
   * @returns {Promise<void>}
   * @throws {Error} If the page does not load within {@link LOAD_TIMEOUT_MS}.
   */
  async load(url, viewport) {
    assert.ok(viewport.w > 0 && viewport.h > 0, "viewport must be positive");
    await this.send("Emulation.setDeviceMetricsOverride", {
      width: viewport.w,
      height: viewport.h,
      deviceScaleFactor: viewport.scale,
      mobile: false,
    });
    const loaded = new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        off();
        reject(
          new Error(`load of ${url} timed out after ${LOAD_TIMEOUT_MS}ms`),
        );
      }, LOAD_TIMEOUT_MS);
      const off = this.on("Page.loadEventFired", () => {
        clearTimeout(timer);
        off();
        resolve(undefined);
      });
    });
    await this.send("Page.navigate", { url });
    await loaded;
  }

  /**
   * Evaluate an expression in the page and return its JSON value.
   *
   * @param {string} expression JavaScript source. Must evaluate to a JSON-safe value.
   * @returns {Promise<unknown>}
   * @throws {Error} If the expression throws.
   */
  async evaluate(expression) {
    const result = await this.send("Runtime.evaluate", {
      expression,
      returnByValue: true,
      awaitPromise: true,
    });
    if (result.exceptionDetails !== undefined) {
      throw new Error(
        `page evaluation failed: ${result.exceptionDetails.text}`,
      );
    }
    return result.result.value;
  }

  /**
   * Capture the current viewport as PNG bytes.
   *
   * @returns {Promise<Buffer>}
   */
  async screenshot() {
    const result = await this.send("Page.captureScreenshot", {
      format: "png",
      captureBeyondViewport: false,
      fromSurface: true,
    });
    return Buffer.from(result.data, "base64");
  }

  /**
   * Close the socket and terminate the browser process.
   *
   * Cleanup runs to completion even when a step fails, because leaving a
   * browser process alive is worse than any single failure here. Failures are
   * collected and returned rather than thrown, so a caller unwinding from an
   * earlier error does not have that error replaced by a teardown error, and
   * rather than swallowed, so a leak is never silent.
   *
   * @returns {Promise<string[]>} Descriptions of any cleanup step that failed.
   *   An empty array means the browser shut down cleanly.
   */
  async close() {
    for (const slot of this.pending.values()) clearTimeout(slot.timer);
    this.pending.clear();

    /** @type {string[]} */
    const problems = [];
    try {
      await this.send("Browser.close", {}, false);
    } catch (error) {
      problems.push(`graceful close refused, sent SIGKILL: ${error.message}`);
      this.process.kill("SIGKILL");
    }

    // A socket already closed by the browser's own exit is the normal path, not
    // a failure, so it is filtered by state rather than by catching.
    if (this.socket.readyState !== WebSocket.CLOSED) {
      try {
        this.socket.close();
      } catch (error) {
        problems.push(`socket close failed: ${error.message}`);
      }
    }

    const exited = new Promise((resolve) => this.process.once("exit", resolve));
    const timer = setTimeout(() => {
      problems.push("browser did not exit within 5000ms, sent SIGKILL");
      this.process.kill("SIGKILL");
    }, 5_000);
    await exited;
    clearTimeout(timer);
    return problems;
  }
}

/**
 * Launch Chrome and attach to a single blank page.
 *
 * @param {string} executable Absolute path to a Chrome-family binary.
 * @param {string} profileDir Throwaway profile directory, created by the caller.
 * @returns {Promise<Browser>} A browser with one attached page session.
 * @throws {Error} If Chrome cannot start or the page cannot be attached.
 */
export async function launch(executable, profileDir) {
  const child = spawn(executable, chromeArgs(profileDir), {
    stdio: ["ignore", "ignore", "ignore"],
  });
  child.once("error", (error) => {
    throw new Error(`could not start ${executable}: ${error.message}`);
  });

  const endpoint = await waitForEndpoint(profileDir);
  const socket = new WebSocket(endpoint);
  await new Promise((resolve, reject) => {
    const timer = setTimeout(
      () =>
        reject(
          new Error(
            `WebSocket to ${endpoint} did not open in ${LAUNCH_TIMEOUT_MS}ms`,
          ),
        ),
      LAUNCH_TIMEOUT_MS,
    );
    socket.addEventListener("open", () => {
      clearTimeout(timer);
      resolve(undefined);
    });
    socket.addEventListener("error", () => {
      clearTimeout(timer);
      reject(new Error(`WebSocket to ${endpoint} failed`));
    });
  });

  const browser = new Browser(child, socket);
  const target = await browser.send(
    "Target.createTarget",
    { url: "about:blank" },
    false,
  );
  const attached = await browser.send(
    "Target.attachToTarget",
    { targetId: target.targetId, flatten: true },
    false,
  );
  browser.sessionId = attached.sessionId;
  await browser.send("Page.enable");
  await browser.send("Runtime.enable");
  return browser;
}
