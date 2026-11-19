import { spawn } from "node:child_process";
import { hostname, networkInterfaces } from "node:os";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

function isPrivateIPv4(address) {
  const parts = address.split(".").map(Number);
  if (parts.length !== 4 || parts.some((part) => !Number.isInteger(part) || part < 0 || part > 255)) {
    return false;
  }
  const [first, second] = parts;
  if (first === 10) return true;
  if (first === 192 && second === 168) return true;
  if (first === 172 && second >= 16 && second <= 31) return true;
  return false;
}

export function detectDevOrigins() {
  const origins = [];
  const add = (origin) => {
    if (origin && !origins.includes(origin)) origins.push(origin);
  };

  const host = hostname();
  add(host);
  add(`${host}.local`);

  for (const [interfaceName, entries] of Object.entries(networkInterfaces())) {
    if (interfaceName.startsWith("lo") || !entries) continue;
    for (const entry of entries) {
      if (entry.family !== "IPv4" || entry.internal) continue;
      if (isPrivateIPv4(entry.address)) add(entry.address);
    }
  }

  return origins;
}

function main() {
  const origins = detectDevOrigins();
  console.log(`Next dev origins: ${origins.join(", ")}`);

  const child = spawn(process.platform === "win32" ? "npx.cmd" : "npx", ["next", "dev"], {
    stdio: "inherit",
    env: {
      ...process.env,
      PORT: process.env.PORT || "3000",
      GANJI_ALLOWED_DEV_ORIGINS: origins.join(","),
    },
  });

  child.on("exit", (code, signal) => {
    if (signal) process.kill(process.pid, signal);
    else process.exit(code ?? 0);
  });
}

const invokedDirectly = process.argv[1] && fileURLToPath(import.meta.url) === resolve(process.argv[1]);
if (invokedDirectly) main();
