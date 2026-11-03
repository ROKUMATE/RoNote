import { describe, expect, test } from "vitest";
import { maxUrlLength } from "../imports/ImportDialog";
import { specEnum, specSchemas } from "../test/spec";
import { importErrorMessages, requestErrorMessages } from "./messages";

describe.each([
  ["ImportErrorCode", importErrorMessages],
  ["ErrorCode", requestErrorMessages],
])("%s", (name, messages: Record<string, string>) => {
  test("has a message for every code in the spec, and no others", () => {
    expect(Object.keys(messages).sort()).toEqual(specEnum(name).sort());
  });

  test("messages are plain sentences", () => {
    for (const message of Object.values(messages)) {
      expect(message).toMatch(/^[A-Z].*\.$/);
    }
  });
});

test("the dialog refuses URLs at the spec's length limit", () => {
  expect(maxUrlLength).toBe(specSchemas.CreateImportRequest?.properties?.url?.maxLength);
});
