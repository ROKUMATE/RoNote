import { expect, test } from "vitest";
import { parseEvent, parseRequest } from "./protocol";

test("a request or event in the protocol parses as itself", () => {
  expect(parseRequest({ v: 1, type: "hello" })).toEqual({ v: 1, type: "hello" });
  expect(parseEvent({ v: 1, type: "hello", isolated: true })).toEqual({
    v: 1,
    type: "hello",
    isolated: true,
  });
});

const notEvents: [string, unknown][] = [
  ["another version", { v: 2, type: "hello", isolated: true }],
  ["no version", { type: "hello", isolated: true }],
  ["the version as text", { v: "1", type: "hello", isolated: true }],
  ["an unknown type", { v: 1, type: "goodbye", isolated: true }],
  ["a type from the object prototype", { v: 1, type: "toString", isolated: true }],
  ["a missing field", { v: 1, type: "hello" }],
  ["a mistyped field", { v: 1, type: "hello", isolated: "yes" }],
  ["an extra field", { v: 1, type: "hello", isolated: true, code: "print(1)" }],
  ["null", null],
  ["text", '{"v":1,"type":"hello","isolated":true}'],
  ["an array", [1, "hello", true]],
  [
    "an object of a class",
    new (class {
      v = 1;
      type = "hello";
      isolated = true;
    })(),
  ],
  ["an inherited field", Object.assign(Object.create({ isolated: true }), { v: 1, type: "hello" })],
];

test.each(notEvents)("an event with %s is dropped", (_, data) => {
  expect(parseEvent(data)).toBeUndefined();
});

test("a request with an event's fields, or of another version, is dropped", () => {
  expect(parseRequest({ v: 1, type: "hello", isolated: true })).toBeUndefined();
  expect(parseRequest({ v: 0, type: "hello" })).toBeUndefined();
});

test("a run, and the events it brings, parse only with their fields in range", () => {
  const run = { v: 1, type: "run", id: "b1", code: "print(1)", mode: "exec" };
  expect(parseRequest(run)).toEqual(run);
  expect(parseRequest({ ...run, mode: "shell" })).toBeUndefined();
  expect(parseRequest({ ...run, code: "x".repeat(5_000_001) })).toBeUndefined();
  expect(parseRequest({ ...run, id: "x".repeat(201) })).toBeUndefined();

  const done = { v: 1, type: "done", id: "b1", ms: 12, end: "timeout" };
  expect(parseEvent(done)).toEqual(done);
  expect(parseEvent({ ...done, ms: -1 })).toBeUndefined();
  expect(parseEvent({ ...done, ms: Number.NaN })).toBeUndefined();
  expect(parseEvent({ ...done, end: "crashed" })).toBeUndefined();
  expect(
    parseEvent({ v: 1, type: "stdout", id: "b1", text: "x".repeat(1_000_001) }),
  ).toBeUndefined();
  expect(parseEvent({ v: 1, type: "ended", reason: "crash" })).toEqual({
    v: 1,
    type: "ended",
    reason: "crash",
  });
});
