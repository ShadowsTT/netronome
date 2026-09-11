/*
 * Copyright (c) 2024-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { UptimeMonitorInput, UptimeType } from "@/types/types";

export const typeOptions: { value: UptimeType; label: string }[] = [
  { value: "http", label: "HTTP" },
  { value: "tcp", label: "TCP" },
];

export const methodOptions = ["GET", "HEAD"];

// Mirrors the server: a code, a class, or an inclusive range.
export const expectedStatusOptions = [
  { value: "2xx", label: "2xx (success)" },
  { value: "200", label: "200 only" },
  { value: "200-399", label: "200–399 (allow redirects)" },
  { value: "3xx", label: "3xx (redirect)" },
  { value: "200-499", label: "200–499 (anything but a server error)" },
];

// Same bounds as the server's MinTimeoutSeconds / MaxTimeoutSeconds.
export const MIN_TIMEOUT_SECONDS = 1;
export const MAX_TIMEOUT_SECONDS = 120;

// Badges for the states the server reports. A monitor that has not run yet is
// in the "unknown" state and gets no badge.
const STATE_BADGES: Record<string, { label: string; className: string }> = {
  ok: {
    label: "Up",
    className:
      "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/20",
  },
  down: {
    label: "Down",
    className: "bg-red-500/10 text-red-600 dark:text-red-400 border-red-500/20",
  },
  recovered: {
    label: "Recovered",
    className:
      "bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/20",
  },
};

export const stateBadge = (state?: string) => STATE_BADGES[state ?? ""] ?? null;

export const defaultUptimeFormData: UptimeMonitorInput = {
  name: "",
  type: "http",
  target: "",
  interval: "1m",
  timeoutSeconds: 10,
  method: "GET",
  expectedStatus: "2xx",
  keyword: "",
  verifyTls: true,
  enabled: true,
};

// validateUptimeInput mirrors the server's checks so the form can say what is
// wrong before the request goes out. It returns the first problem, or null.
export const validateUptimeInput = (input: UptimeMonitorInput): string | null => {
  const target = input.target.trim();
  if (!target) {
    return "Target is required";
  }

  if (input.type === "tcp") {
    if (!parseHostPort(target)) {
      return "Target must be host:port, with a port between 1 and 65535";
    }
  } else {
    let url: URL;
    try {
      url = new URL(target);
    } catch {
      return "Target must be an http:// or https:// URL";
    }
    if ((url.protocol !== "http:" && url.protocol !== "https:") || !url.host) {
      return "Target must be an http:// or https:// URL";
    }
    if (input.keyword.trim() && input.method === "HEAD") {
      return "Keyword needs the GET method, HEAD has no body";
    }
  }

  if (
    !Number.isInteger(input.timeoutSeconds) ||
    input.timeoutSeconds < MIN_TIMEOUT_SECONDS ||
    input.timeoutSeconds > MAX_TIMEOUT_SECONDS
  ) {
    return `Timeout must be between ${MIN_TIMEOUT_SECONDS} and ${MAX_TIMEOUT_SECONDS} seconds`;
  }

  return null;
};

// parseHostPort accepts host:port and [ipv6]:port, like Go's net.SplitHostPort.
const parseHostPort = (target: string): boolean => {
  const at = target.lastIndexOf(":");
  if (at <= 0) {
    return false;
  }
  const host = target.slice(0, at);
  const port = Number(target.slice(at + 1));
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    return false;
  }
  if (host.startsWith("[")) {
    return host.endsWith("]") && host.length > 2;
  }
  return host.length > 0 && !host.includes(":");
};

// describeTarget is the one-line summary under a monitor's name.
export const describeTarget = (monitor: UptimeMonitorInput): string =>
  monitor.type === "tcp"
    ? `TCP ${monitor.target}`
    : `${monitor.method} ${monitor.target} → ${monitor.expectedStatus}${
        monitor.keyword ? ` · contains "${monitor.keyword}"` : ""
      }`;

// daysUntil returns whole days from now to an ISO timestamp, negative if past.
export const daysUntil = (iso: string, now: Date = new Date()): number =>
  Math.floor((new Date(iso).getTime() - now.getTime()) / 86_400_000);
