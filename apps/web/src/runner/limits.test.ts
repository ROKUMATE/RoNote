import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { limitRun, outputLimit, runLimit } from "./limits";

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

test("the clock starts when told, and runs out at the run limit", () => {
  const timeout = vi.fn();
  const limits = limitRun(timeout);
  vi.advanceTimersByTime(runLimit * 2);
  expect(timeout).not.toHaveBeenCalled();

  limits.start();
  vi.advanceTimersByTime(runLimit - 1);
  expect(limits.elapsed()).toBe(runLimit - 1);
  expect(timeout).not.toHaveBeenCalled();
  vi.advanceTimersByTime(1);
  expect(timeout).toHaveBeenCalledOnce();
});

test("an ended clock never runs out", () => {
  const timeout = vi.fn();
  const limits = limitRun(timeout);
  limits.start();
  limits.end();
  vi.advanceTimersByTime(runLimit);
  expect(timeout).not.toHaveBeenCalled();
});

test("output is taken up to the limit, and the text that reaches it says so", () => {
  const limits = limitRun(() => {});
  expect(limits.take("x".repeat(600_000))).toEqual({ text: "x".repeat(600_000), full: false });
  const rest = limits.take("y".repeat(600_000));
  expect(rest.text.length).toBe(outputLimit - 600_000);
  expect(rest.full).toBe(true);
});

test("output that exactly fills the limit is the last", () => {
  const limits = limitRun(() => {});
  expect(limits.take("x".repeat(outputLimit)).full).toBe(true);
});

test("whole items fit or don't, and one that doesn't isn't counted", () => {
  const limits = limitRun(() => {});
  expect(limits.fits(outputLimit - 10)).toBe(true);
  expect(limits.fits(11)).toBe(false);
  expect(limits.fits(10)).toBe(true);
  expect(limits.fits(1)).toBe(false);
});
