/*
 * Copyright (c) 2024-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import React, { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { UptimeMonitor, UptimeMonitorInput, UptimeType } from "@/types/types";
import { intervalOptions } from "@/components/speedtest/packetloss/constants/packetLossConstants";
import { formatInterval } from "@/components/speedtest/packetloss/utils/packetLossUtils";
import {
  MAX_TIMEOUT_SECONDS,
  MIN_TIMEOUT_SECONDS,
  expectedStatusOptions,
  methodOptions,
  typeOptions,
  validateUptimeInput,
} from "./constants";

interface UptimeMonitorFormProps {
  showForm: boolean;
  onClose: () => void;
  onSubmit: (data: UptimeMonitorInput) => void;
  editingMonitor?: UptimeMonitor | null;
  formData: UptimeMonitorInput;
  onFormDataChange: (data: UptimeMonitorInput) => void;
  isLoading?: boolean;
}

const selectTriggerClass =
  "w-full bg-gray-200/50 dark:bg-gray-800/50 border-gray-300 dark:border-gray-900";

const inputGuards = {
  "data-1p-ignore": true,
  "data-lpignore": "true",
  "data-form-type": "other",
  autoComplete: "off",
} as const;

export const UptimeMonitorForm: React.FC<UptimeMonitorFormProps> = ({
  showForm,
  onClose,
  onSubmit,
  editingMonitor,
  formData,
  onFormDataChange,
  isLoading = false,
}) => {
  const [error, setError] = useState<string | null>(null);
  const isHTTP = formData.type === "http";

  const update = (patch: Partial<UptimeMonitorInput>) => {
    setError(null);
    onFormDataChange({ ...formData, ...patch });
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const data = { ...formData, target: formData.target.trim() };
    const problem = validateUptimeInput(data);
    if (problem) {
      setError(problem);
      return;
    }
    onSubmit(data);
  };

  const handleClose = () => {
    setError(null);
    onClose();
  };

  return (
    <Dialog open={showForm} onOpenChange={handleClose}>
      <DialogContent className="w-full max-w-md bg-white dark:bg-gray-850 border dark:border-gray-900 max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="text-lg font-medium text-gray-900 dark:text-white">
            {editingMonitor ? "Edit Uptime Monitor" : "New Uptime Monitor"}
          </DialogTitle>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <Label className="mb-2">Type</Label>
            <div className="flex gap-2">
              {typeOptions.map((option) => (
                <button
                  key={option.value}
                  type="button"
                  onClick={() => update({ type: option.value as UptimeType })}
                  className={`flex-1 px-3 py-1.5 rounded-md text-sm border transition-colors ${
                    formData.type === option.value
                      ? "bg-blue-500/10 text-blue-600 dark:text-blue-400 border-blue-500/30"
                      : "bg-gray-200/50 dark:bg-gray-800/50 text-gray-700 dark:text-gray-300 border-gray-300 dark:border-gray-800 hover:bg-gray-300/50 dark:hover:bg-gray-700/50"
                  }`}
                >
                  {option.label}
                </button>
              ))}
            </div>
          </div>

          <div>
            <Label>{isHTTP ? "URL" : "Host and port"}</Label>
            <Input
              type="text"
              value={formData.target}
              onChange={(e) => update({ target: e.target.value })}
              placeholder={
                isHTTP ? "https://example.com/health" : "192.168.1.10:22"
              }
              required
              {...inputGuards}
            />
            <p className="text-xs text-gray-500 dark:text-gray-500 mt-1">
              {isHTTP
                ? "http:// or https://. Redirects are followed, up to five."
                : "The check passes when the port accepts a connection."}
            </p>
          </div>

          <div>
            <Label>Name</Label>
            <Input
              type="text"
              value={formData.name}
              onChange={(e) => update({ name: e.target.value })}
              placeholder={isHTTP ? "e.g., Home Assistant" : "e.g., NAS SSH"}
              {...inputGuards}
            />
          </div>

          {isHTTP && (
            <>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <Label>Method</Label>
                  <Select
                    value={formData.method}
                    onValueChange={(value) => update({ method: value })}
                  >
                    <SelectTrigger className={selectTriggerClass}>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {methodOptions.map((method) => (
                        <SelectItem key={method} value={method}>
                          {method}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div>
                  <Label>Expected status</Label>
                  <Select
                    value={formData.expectedStatus}
                    onValueChange={(value) => update({ expectedStatus: value })}
                  >
                    <SelectTrigger className={selectTriggerClass}>
                      <SelectValue>{formData.expectedStatus}</SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {expectedStatusOptions.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>

              <div>
                <Label>Body must contain</Label>
                <Input
                  type="text"
                  value={formData.keyword}
                  onChange={(e) => update({ keyword: e.target.value })}
                  placeholder="optional, e.g. healthy"
                  disabled={formData.method === "HEAD"}
                  {...inputGuards}
                />
                <p className="text-xs text-gray-500 dark:text-gray-500 mt-1">
                  GET only. The first 1 MiB of the body is searched.
                </p>
              </div>

              <div className="flex items-center space-x-2">
                <Checkbox
                  id="uptime-verify-tls"
                  checked={formData.verifyTls}
                  onCheckedChange={(checked) =>
                    update({ verifyTls: checked as boolean })
                  }
                />
                <Label htmlFor="uptime-verify-tls" className="cursor-pointer">
                  Verify the TLS certificate
                </Label>
              </div>
            </>
          )}

          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label>Check Interval</Label>
              <Select
                value={formData.interval}
                onValueChange={(value) => update({ interval: value })}
              >
                <SelectTrigger className={selectTriggerClass}>
                  <SelectValue>
                    {intervalOptions.find(
                      (option) => option.value === formData.interval,
                    )?.label || `Every ${formatInterval(formData.interval)}`}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {intervalOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label>Timeout (s)</Label>
              <Input
                type="number"
                min={MIN_TIMEOUT_SECONDS}
                max={MAX_TIMEOUT_SECONDS}
                step={1}
                value={formData.timeoutSeconds}
                onChange={(e) =>
                  update({ timeoutSeconds: Number(e.target.value) })
                }
                {...inputGuards}
              />
            </div>
          </div>

          <div className="flex items-center space-x-2">
            <Checkbox
              id="uptime-enabled"
              checked={formData.enabled}
              onCheckedChange={(checked) =>
                update({ enabled: checked as boolean })
              }
            />
            <Label htmlFor="uptime-enabled" className="cursor-pointer">
              Start monitoring immediately
            </Label>
          </div>

          {error && (
            <p className="text-sm text-red-600 dark:text-red-400" role="alert">
              {error}
            </p>
          )}

          <div className="flex gap-3 pt-4">
            <Button
              type="submit"
              disabled={isLoading}
              isLoading={isLoading}
              variant="default"
              className="flex-1"
            >
              {editingMonitor ? "Update Monitor" : "Create Monitor"}
            </Button>
            <Button type="button" onClick={handleClose} variant="secondary">
              Cancel
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
};
