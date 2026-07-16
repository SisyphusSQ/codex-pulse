/// <reference types="node" />

import { readdirSync, readFileSync } from "node:fs";
import { extname, join, relative } from "node:path";
import { describe, expect, it } from "vitest";

const sourceRoot = join(process.cwd(), "src");
const messageCatalog = join(sourceRoot, "i18n", "messages", "zh-CN.ts");

function productionSourceFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) {
      return productionSourceFiles(path);
    }
    if (![".ts", ".vue"].includes(extname(entry.name))) {
      return [];
    }
    if (entry.name.endsWith(".test.ts") || entry.name.endsWith(".d.ts") || path === messageCatalog) {
      return [];
    }
    return [path];
  });
}

describe("zh-CN visible message boundary", () => {
  it("keeps CJK copy in the central message catalog", () => {
    const violations = productionSourceFiles(sourceRoot)
      .filter((path) => /[\u3400-\u9fff]/u.test(readFileSync(path, "utf8")))
      .map((path) => relative(sourceRoot, path));

    expect(violations).toEqual([]);
  });
});
