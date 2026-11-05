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
