#!/usr/bin/env node
"use strict";

// kiroproxy — launcher for the Kiro Proxy Go server.
//
// Responsibilities, in order:
//   1. make sure a server binary matching this package version is installed
//   2. pick a port that is actually free
//   3. run the server in the foreground, forwarding signals and its exit code
//   4. open the admin UI once the port answers
//   5. notify the user (non-blocking) when a newer npm package is available
//
// Everything else (accounts, keys, routing) lives in the server and its admin
// UI. This file stays a launcher on purpose.

const { spawn } = require("child_process");
const fs = require("fs");
const https = require("https");
const net = require("net");
const os = require("os");
const path = require("path");

const pkg = require("./package.json");
const { appDir, binaryPath, configPath } = require("./src/paths");
const {
  downloadBinary,
  installedVersion,
  isBinaryCurrent,
} = require("./src/downloadBinary");

const DEFAULT_PORT = 8080;
const MAX_PORT_ATTEMPTS = 10;

// ---------------------------------------------------------------- update check

// Fetch latest version from the npm registry in the background.
// Prints a notice box if the installed version is behind — never throws.
function checkForUpdate() {
  return new Promise((resolve) => {
    const url = `https://registry.npmjs.org/${pkg.name}/latest`;
    const req = https.get(url, { timeout: 5000 }, (res) => {
      let raw = "";
      res.on("data", (chunk) => (raw += chunk));
      res.on("end", () => {
        try {
          const latest = JSON.parse(raw).version;
          if (latest && latest !== pkg.version && isNewer(latest, pkg.version)) {
            resolve(latest);
          } else {
            resolve(null);
          }
        } catch {
          resolve(null);
        }
      });
    });
    req.on("error", () => resolve(null));
    req.on("timeout", () => { req.destroy(); resolve(null); });
  });
}

// Simple semver comparison — only handles numeric major.minor.patch.
function isNewer(a, b) {
  const parse = (v) => v.split(".").map(Number);
  const [aMaj, aMin, aPat] = parse(a);
  const [bMaj, bMin, bPat] = parse(b);
  if (aMaj !== bMaj) return aMaj > bMaj;
  if (aMin !== bMin) return aMin > bMin;
  return aPat > bPat;
}

function printUpdateNotice(latest) {
  const current = pkg.version;
  const name = pkg.name;
  const lines = [
    `   Update available: ${current}  →  ${latest}   `,
    `   Run: npm install -g ${name}@latest          `,
  ];
  const width = Math.max(...lines.map((l) => l.length));
  const border = "─".repeat(width);
  console.log(`\n\x1b[33m┌${border}┐`);
  for (const line of lines) {
    console.log(`│${line.padEnd(width)}│`);
  }
  console.log(`└${border}┘\x1b[0m`);
}

// ---------------------------------------------------------------- arg parsing

function parseArgs(argv) {
  const opts = {
    port: null,
    host: null,
    config: null,
    open: true,
    passthrough: [],
  };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    const takeValue = () => argv[++i];
    switch (arg) {
      case "-p":
      case "--port":
        opts.port = Number(takeValue());
        break;
      case "-h":
      case "--host":
        opts.host = takeValue();
        break;
      case "-c":
      case "--config":
        opts.config = path.resolve(takeValue());
        break;
      case "--no-open":
        opts.open = false;
        break;
      case "--open":
        opts.open = true;
        break;
      case "-v":
      case "--version":
        opts.version = true;
        break;
      case "--help":
        opts.help = true;
        break;
      default:
        if (arg.startsWith("--port=")) opts.port = Number(arg.slice(7));
        else if (arg.startsWith("--host=")) opts.host = arg.slice(7);
        else if (arg.startsWith("--config=")) opts.config = path.resolve(arg.slice(9));
        else opts.passthrough.push(arg);
    }
  }
  if (opts.port !== null && (!Number.isInteger(opts.port) || opts.port < 1 || opts.port > 65535)) {
    throw new Error(`invalid --port value: ${opts.port}`);
  }
  return opts;
}

function printHelp() {
  console.log(`
kiroproxy v${pkg.version} — multi-provider AI gateway

Usage
  kiroproxy [options]

Options
  -p, --port <n>       port to listen on (default ${DEFAULT_PORT}, next free port if busy)
  -h, --host <addr>    address to bind (default: from config, 0.0.0.0)
  -c, --config <path>  config file (default ~/.kiroproxy/config.json)
      --no-open        do not open the admin UI in a browser
  -v, --version        print version
      --help           show this help

State lives in ${appDir()}
Docs: https://github.com/vtruong2k3/Kiro-Go
`);
}

// ------------------------------------------------------------------- terminal

function createSpinner(text) {
  const frames = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];
  let i = 0;
  let timer = null;
  return {
    start() {
      if (!process.stdout.isTTY) {
        console.log(text);
        return this;
      }
      timer = setInterval(() => {
        process.stdout.write(`\r${frames[i++ % frames.length]} ${text}`);
      }, 80);
      return this;
    },
    stop() {
      if (timer) clearInterval(timer);
      timer = null;
      if (process.stdout.isTTY) process.stdout.write("\r\x1b[K");
    },
    succeed(msg) {
      this.stop();
      console.log(`✅ ${msg}`);
    },
    fail(msg) {
      this.stop();
      console.log(`❌ ${msg}`);
    },
  };
}

// ----------------------------------------------------------------- networking

// Bind-test rather than connect-test: a connect check says nothing about ports
// held by a socket in TIME_WAIT or bound to a different interface.
function isPortFree(port, host) {
  return new Promise((resolve) => {
    const server = net.createServer();
    server.once("error", () => resolve(false));
    server.once("listening", () => server.close(() => resolve(true)));
    server.listen(port, host === "0.0.0.0" ? undefined : host);
  });
}

async function pickPort(preferred, host) {
  for (let i = 0; i < MAX_PORT_ATTEMPTS; i++) {
    const candidate = preferred + i;
    if (candidate > 65535) break;
    if (await isPortFree(candidate, host)) return candidate;
  }
  throw new Error(
    `no free port in ${preferred}–${preferred + MAX_PORT_ATTEMPTS - 1}. Pass --port to choose one.`
  );
}

// Poll instead of sleeping a fixed amount: startup time varies with the size of
// the account pool and the SQLite migration.
function waitServerReady(port, { timeoutMs = 20_000, intervalMs = 150 } = {}) {
  const deadline = Date.now() + timeoutMs;
  return new Promise((resolve) => {
    const attempt = () => {
      const socket = net.connect({ host: "127.0.0.1", port }, () => {
        socket.destroy();
        resolve(true);
      });
      socket.on("error", () => {
        socket.destroy();
        if (Date.now() >= deadline) resolve(false);
        else setTimeout(attempt, intervalMs);
      });
    };
    attempt();
  });
}

function openBrowser(url) {
  const cmd =
    process.platform === "darwin"
      ? ["open", [url]]
      : process.platform === "win32"
        ? ["cmd", ["/c", "start", "", url]]
        : ["xdg-open", [url]];
  try {
    const child = spawn(cmd[0], cmd[1], { stdio: "ignore", detached: true });
    child.on("error", () => {});
    child.unref();
  } catch {
    // Headless box or no opener installed — the URL is printed anyway.
  }
}

// First non-internal IPv4: the address peers actually reach when bound to
// 0.0.0.0, worth printing so the user knows the server is network-visible.
function lanAddress() {
  for (const ifaces of Object.values(os.networkInterfaces())) {
    for (const iface of ifaces || []) {
      if (iface.family === "IPv4" && !iface.internal) return iface.address;
    }
  }
  return null;
}

// -------------------------------------------------------------------- startup

async function ensureBinary() {
  if (isBinaryCurrent(pkg.version)) return binaryPath();

  const have = installedVersion();
  const spinner = createSpinner(
    have
      ? `updating server binary ${have} → ${pkg.version}`
      : `downloading server binary v${pkg.version}`
  ).start();
  try {
    const p = await downloadBinary(pkg.version);
    spinner.succeed(`server binary v${pkg.version} ready`);
    return p;
  } catch (err) {
    spinner.fail(`could not install server binary: ${err.message}`);
    if (have) {
      // An older binary still runs; a failed update should not brick the CLI.
      console.warn(`⚠️  falling back to installed v${have}`);
      return binaryPath();
    }
    throw err;
  }
}

async function main() {
  const opts = parseArgs(process.argv.slice(2));

  if (opts.version) {
    console.log(pkg.version);
    return 0;
  }
  if (opts.help) {
    printHelp();
    return 0;
  }

  const binary = await ensureBinary();
  const cfg = opts.config || configPath();
  fs.mkdirSync(path.dirname(cfg), { recursive: true });

  const host = opts.host || "0.0.0.0";
  // An explicit --port is a request, not a hint: fail loudly if it is taken
  // rather than quietly serving somewhere the user did not ask for.
  let port;
  if (opts.port) {
    if (!(await isPortFree(opts.port, host))) {
      throw new Error(`port ${opts.port} is already in use`);
    }
    port = opts.port;
  } else {
    port = await pickPort(DEFAULT_PORT, host);
    if (port !== DEFAULT_PORT) {
      console.log(`ℹ️  port ${DEFAULT_PORT} busy, using ${port}`);
    }
  }

  const args = ["--config", cfg, "--port", String(port), ...opts.passthrough];
  if (opts.host) args.push("--host", opts.host);

  const child = spawn(binary, args, {
    stdio: "inherit",
    env: { ...process.env },
  });

  // Forward signals so Ctrl-C stops the server rather than orphaning it.
  const forward = (sig) => () => {
    if (!child.killed) child.kill(sig);
  };
  process.on("SIGINT", forward("SIGINT"));
  process.on("SIGTERM", forward("SIGTERM"));

  const exited = new Promise((resolve) => {
    child.on("error", (err) => {
      console.error(`❌ failed to start server: ${err.message}`);
      resolve(1);
    });
    child.on("exit", (code, signal) => resolve(signal ? 1 : (code ?? 0)));
  });

  const ready = await Promise.race([
    waitServerReady(port).then((ok) => ({ ok })),
    exited.then((code) => ({ exitedWith: code })),
  ]);

  if (ready.exitedWith !== undefined) return ready.exitedWith;

  const adminURL = `http://localhost:${port}/admin`;

  // Start update check in background (non-blocking — we don't await here).
  const updatePromise = checkForUpdate();

  if (ready.ok) {
    const lan = host === "0.0.0.0" ? lanAddress() : null;
    console.log("");
    console.log(`🚀 Kiro Proxy v${pkg.version}`);
    console.log(`   Admin    ${adminURL}`);
    console.log(`   Claude   http://localhost:${port}/v1/messages`);
    console.log(`   OpenAI   http://localhost:${port}/v1/chat/completions`);
    console.log(`   Config   ${cfg}`);
    if (lan) console.log(`   Network  http://${lan}:${port}  ⚠️  reachable from your LAN`);
    console.log("");
    if (opts.open) openBrowser(adminURL);

    // Print update notice once the check resolves (usually within 1-2 s).
    updatePromise.then((latest) => {
      if (latest) printUpdateNotice(latest);
    });
  } else {
    console.warn(`⚠️  server did not answer on port ${port} yet — check the log above`);
  }

  return exited;
}

main()
  .then((code) => process.exit(code))
  .catch((err) => {
    console.error(`❌ ${err.message}`);
    process.exit(1);
  });
