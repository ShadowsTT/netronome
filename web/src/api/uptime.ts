/*
 * Copyright (c) 2024-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { getApiUrl } from "@/utils/baseUrl";
import {
  PaginatedResponse,
  UptimeMonitor,
  UptimeMonitorInput,
  UptimeResult,
  UptimeUpdate,
} from "@/types/types";

// uptimeFetch keeps the error text the API sends, so a rejected monitor shows
// the field that was wrong instead of a generic message.
const uptimeFetch = async (
  path: string,
  init?: RequestInit,
): Promise<Response> => {
  const response = await fetch(getApiUrl(path), init);
  if (!response.ok) {
    const message = await response
      .json()
      .then((data) => data.error as string | undefined)
      .catch(() => undefined);
    throw new Error(message || "Uptime monitor request failed");
  }
  return response;
};

const jsonBody = (monitor: UptimeMonitorInput): RequestInit => ({
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify(monitor),
});

export const getUptimeMonitors = async (): Promise<UptimeMonitor[]> => {
  const response = await uptimeFetch("/uptime/monitors");
  // an empty list comes back as null, so hand callers a list either way
  return (await response.json()) ?? [];
};

export const createUptimeMonitor = async (
  monitor: UptimeMonitorInput,
): Promise<UptimeMonitor> => {
  const response = await uptimeFetch("/uptime/monitors", {
    method: "POST",
    ...jsonBody(monitor),
  });
  return response.json();
};

export const updateUptimeMonitor = async (
  monitor: UptimeMonitorInput & { id: number },
): Promise<UptimeMonitor> => {
  const response = await uptimeFetch(`/uptime/monitors/${monitor.id}`, {
    method: "PUT",
    ...jsonBody(monitor),
  });
  return response.json();
};

export const deleteUptimeMonitor = async (id: number): Promise<void> => {
  await uptimeFetch(`/uptime/monitors/${id}`, { method: "DELETE" });
};

export const getUptimeMonitorStatus = async (
  id: number,
): Promise<UptimeUpdate> => {
  const response = await uptimeFetch(`/uptime/monitors/${id}/status`);
  return response.json();
};

export const getUptimeHistory = async (
  id: number,
  page: number = 1,
  limit: number = 25,
): Promise<PaginatedResponse<UptimeResult>> => {
  const response = await uptimeFetch(
    `/uptime/monitors/${id}/history?page=${page}&limit=${limit}`,
  );
  return response.json();
};
