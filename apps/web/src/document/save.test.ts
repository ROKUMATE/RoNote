import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { ApiError } from "../api/client";
import type { DocContent } from "../api/schema";
import { Autosave, type SaveFn, type SaveStatus } from "./save";

const content: DocContent = { doc_version: 1, blocks: [] };

function setup(save: SaveFn) {
  const statuses: SaveStatus["state"][] = [];
  const autosave = new Autosave({
    version: 3,
    save: vi.fn(save),
    read: () => content,
    onStatus: (s) => statuses.push(s.state),
  });
  return { autosave, statuses };
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

const conflict = () => new ApiError(409, { code: "version_conflict", message: "", request_id: "" });

test("saves a second after the last change, naming the version it was edited from", async () => {
  const save = vi.fn(async (_c: DocContent, version: number) => version + 1);
  const { autosave, statuses } = setup(save);

  autosave.change();
  await vi.advanceTimersByTimeAsync(600);
  autosave.change();
  await vi.advanceTimersByTimeAsync(600);
  expect(save).not.toHaveBeenCalled();
  await vi.advanceTimersByTimeAsync(400);

  expect(save).toHaveBeenCalledTimes(1);
  expect(save.mock.calls[0]?.[1]).toBe(3);
  expect(statuses.at(-1)).toBe("saved");

  autosave.change();
  await vi.advanceTimersByTimeAsync(1000);
  expect(save.mock.calls[1]?.[1]).toBe(4);
});

test("a blur or a hidden page saves at once", async () => {
  const save = vi.fn<SaveFn>(async (_c, version) => version + 1);
  const { autosave } = setup(save);

  autosave.change();
  await autosave.flush({ keepalive: true });

  expect(save).toHaveBeenCalledTimes(1);
  expect(save.mock.calls[0]?.[2]).toEqual({ keepalive: true });
  await vi.advanceTimersByTimeAsync(2000);
  expect(save).toHaveBeenCalledTimes(1);
});

test("nothing to save, nothing saved", async () => {
  const save = vi.fn(async () => 4);
  const { autosave } = setup(save);

  await autosave.flush();

  expect(save).not.toHaveBeenCalled();
});

test("offline, it keeps the changes and retries until a save goes through", async () => {
  let online = false;
  const save = vi.fn(async (_c: DocContent, version: number) => {
    if (!online) throw new TypeError("Failed to fetch");
    return version + 1;
  });
  const { autosave, statuses } = setup(save);

  autosave.change();
  await vi.advanceTimersByTimeAsync(1000);
  expect(statuses.at(-1)).toBe("offline");
  await vi.advanceTimersByTimeAsync(1000);
  expect(save).toHaveBeenCalledTimes(2);
  online = true;
  await vi.advanceTimersByTimeAsync(2000);

  expect(save).toHaveBeenCalledTimes(3);
  expect(save.mock.calls.map((c) => c[1])).toEqual([3, 3, 3]);
  expect(statuses.at(-1)).toBe("saved");
});

test("a server error is retried like being offline", async () => {
  const save = vi.fn().mockRejectedValueOnce(new ApiError(500, undefined)).mockResolvedValue(4);
  const { autosave, statuses } = setup(save);

  autosave.change();
  await vi.advanceTimersByTimeAsync(2000);

  expect(statuses).toContain("offline");
  expect(statuses.at(-1)).toBe("saved");
});

test("a conflict stops saving for good", async () => {
  const save = vi.fn(async () => {
    throw conflict();
  });
  const { autosave, statuses } = setup(save);

  autosave.change();
  await vi.advanceTimersByTimeAsync(1000);
  autosave.change();
  await autosave.flush();
  await vi.advanceTimersByTimeAsync(60_000);

  expect(statuses.at(-1)).toBe("conflict");
  expect(save).toHaveBeenCalledTimes(1);
});

test("a refused save stops saving and keeps its reason", async () => {
  const save = vi.fn(async () => {
    throw new ApiError(422, { code: "source_block_modified", message: "", request_id: "" });
  });
  const statuses: SaveStatus[] = [];
  const autosave = new Autosave({
    version: 3,
    save,
    read: () => content,
    onStatus: (s) => statuses.push(s),
  });

  autosave.change();
  await vi.advanceTimersByTimeAsync(1000);

  expect(statuses.at(-1)).toEqual({ state: "refused", code: "source_block_modified" });
});

test("a change while saving is saved next, from the new version", async () => {
  let finish: (version: number) => void = () => {};
  const save = vi
    .fn<SaveFn>()
    .mockImplementationOnce(() => new Promise((resolve) => (finish = resolve)))
    .mockResolvedValue(5);
  const { autosave, statuses } = setup(save);

  autosave.change();
  await vi.advanceTimersByTimeAsync(1000);
  autosave.change();
  finish(4);
  await vi.advanceTimersByTimeAsync(1000);

  expect(save.mock.calls.map((c) => c[1])).toEqual([3, 4]);
  expect(statuses.at(-1)).toBe("saved");
});

test("after a revert the next save names the server's new version", async () => {
  const save = vi.fn(async (_c: DocContent, version: number) => version + 1);
  const { autosave } = setup(save);

  autosave.setVersion(7);
  autosave.change();
  await autosave.flush();

  expect(save.mock.calls[0]?.[1]).toBe(7);
});
