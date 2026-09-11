/*
 * Copyright (c) 2024-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { strict as assert } from "node:assert";
import { test } from "node:test";
import type { UptimeMonitorInput } from "../../types/types.ts";
import {
  daysUntil,
  defaultUptimeFormData,
  describeTarget,
  validateUptimeInput,
} from "./constants.ts";

const http = (over: Partial<UptimeMonitorInput>): UptimeMonitorInput => ({
  ...defaultUptimeFormData,
  target: "https://example.invalid/health",
  ...over,
});

const tcp = (target: string): UptimeMonitorInput =>
  http({ type: "tcp", target });

test("validateUptimeInput accepts good http and tcp monitors", () => {
  assert.equal(validateUptimeInput(http({})), null);
  assert.equal(validateUptimeInput(http({ target: "http://10.0.0.5:8080/" })), null);
  assert.equal(validateUptimeInput(http({ keyword: "ok" })), null);
  assert.equal(validateUptimeInput(tcp("192.168.1.1:22")), null);
  assert.equal(validateUptimeInput(tcp("[::1]:443")), null);
  assert.equal(validateUptimeInput(tcp("nas.local:5000")), null);
});

test("validateUptimeInput rejects what the server rejects", () => {
  const cases: [UptimeMonitorInput, RegExp][] = [
    [http({ target: "   " }), /required/],
    [http({ target: "example.invalid" }), /http:\/\/ or https:\/\//],
    [http({ target: "ftp://example.invalid/" }), /http:\/\/ or https:\/\//],
    [http({ method: "HEAD", keyword: "x" }), /HEAD has no body/],
    [http({ timeoutSeconds: 0 }), /between 1 and 120/],
    [http({ timeoutSeconds: 121 }), /between 1 and 120/],
    [http({ timeoutSeconds: 2.5 }), /between 1 and 120/],
    [tcp("192.168.1.1"), /host:port/],
    [tcp("192.168.1.1:70000"), /host:port/],
    [tcp(":22"), /host:port/],
    [tcp("::1:443"), /host:port/],
  ];
  for (const [input, want] of cases) {
    const got = validateUptimeInput(input);
    assert.ok(got && want.test(got), `${input.type} ${input.target}: ${got}`);
  }
});

test("describeTarget summarises each type", () => {
  assert.equal(describeTarget(tcp("db:5432")), "TCP db:5432");
  assert.equal(
    describeTarget(http({})),
    "GET https://example.invalid/health → 2xx",
  );
  assert.equal(
    describeTarget(http({ method: "HEAD", expectedStatus: "200", keyword: "ok" })),
    'HEAD https://example.invalid/health → 200 · contains "ok"',
  );
});

test("daysUntil counts whole days and goes negative once expired", () => {
  const now = new Date("2026-09-11T12:00:00Z");
  assert.equal(daysUntil("2026-10-11T12:00:00Z", now), 30);
  assert.equal(daysUntil("2026-09-12T11:00:00Z", now), 0);
  assert.equal(daysUntil("2026-09-10T12:00:00Z", now), -1);
});
