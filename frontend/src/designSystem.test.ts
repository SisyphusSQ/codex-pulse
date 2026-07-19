/// <reference types="node" />

import { mount } from "@vue/test-utils";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

import StateEmpty from "@/components/ui/StateEmpty.vue";
import StateError from "@/components/ui/StateError.vue";
import StateSkeleton from "@/components/ui/StateSkeleton.vue";
import UiButton from "@/components/ui/UiButton.vue";
import UiCard from "@/components/ui/UiCard.vue";
import UiTable from "@/components/ui/UiTable.vue";

describe("Liquid Glass design foundations", () => {
  it("defines the frozen semantic tokens and accessibility fallbacks", () => {
    const css = readFileSync(join(process.cwd(), "src", "styles.css"), "utf8");
    const titlebar = readFileSync(
      join(process.cwd(), "src", "components", "shell", "AppTitlebar.vue"),
      "utf8",
    );
    const shell = readFileSync(
      join(process.cwd(), "src", "components", "shell", "AppShell.vue"),
      "utf8",
    );
    const sidebar = readFileSync(
      join(process.cwd(), "src", "components", "shell", "AppSidebar.vue"),
      "utf8",
    );

    for (const token of [
      "--color-surface-base",
      "--color-surface-content",
      "--color-surface-glass",
      "--color-accent",
      "--color-healthy",
      "--color-caution",
      "--color-critical",
      "--radius-window",
      "--radius-content",
      "--radius-control",
      "--motion-duration-standard",
    ]) {
      expect(css).toContain(token);
    }

    expect(css).toContain("@media (prefers-reduced-motion: reduce)");
    expect(css).toContain("@media (prefers-contrast: more)");
    expect(css).toContain("@media (prefers-reduced-transparency: reduce)");
    expect(css).toContain("@media (forced-colors: active)");
    expect(css).toContain('[data-transparency="reduce"]');
    expect(css).toContain(".sidebar-secondary-copy");
    expect(css).toContain("--wails-draggable: drag");
    expect(css).toContain("--wails-draggable: no-drag");
    expect(titlebar).toContain("wails-drag-region");
    expect(shell).toContain("px-6 pb-6 pt-2");
    expect(shell).toContain("flex min-h-0 min-w-0 flex-col pt-4");
    expect(sidebar).toContain("px-4 pb-5 pt-10");
    expect(css).toContain("padding: 0.5rem 1rem 1rem");
  });

  it("provides accessible button, card, table, empty, error, and skeleton primitives", async () => {
    const button = mount(UiButton, {
      props: { loading: true, variant: "primary" },
      slots: { default: "Refresh" },
    });
    expect(button.get("button").attributes("aria-busy")).toBe("true");
    expect(button.get("button").attributes("disabled")).toBeDefined();

    const card = mount(UiCard, {
      props: { title: "Quota", description: "Current window" },
      slots: { default: "62%" },
    });
    expect(card.get("section").attributes("aria-labelledby")).toBeTruthy();
    expect(card.text()).toContain("62%");

    const table = mount(UiTable, {
      props: {
        caption: "Usage",
        columns: [{ key: "name", label: "Name" }, { key: "value", label: "Value" }],
        rows: [{ name: "Input", value: "12M" }],
        rowKey: "name",
      },
    });
    expect(table.get("caption").text()).toBe("Usage");
    expect(table.findAll("th")).toHaveLength(2);
    expect(table.findAll("tbody tr")).toHaveLength(1);

    const empty = mount(StateEmpty, { props: { title: "Empty", description: "No records" } });
    expect(empty.get("[role='status']").text()).toContain("No records");

    const error = mount(StateError, {
      props: { title: "Unavailable", description: "Try again", actionLabel: "Retry" },
    });
    await error.get("button").trigger("click");
    expect(error.emitted("retry")).toHaveLength(1);

    const skeleton = mount(StateSkeleton, { props: { label: "Loading" } });
    expect(skeleton.get("[role='status']").attributes("aria-busy")).toBe("true");
    expect(skeleton.get(".sr-only").text()).toBe("Loading");
  });
});
