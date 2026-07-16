<script setup lang="ts">
export interface UiTableColumn {
  key: string;
  label: string;
}

type UiTableRow = Record<string, string | number | null | undefined>;

defineProps<{
  caption: string;
  columns: readonly UiTableColumn[];
  rows: readonly UiTableRow[];
  rowKey: string;
}>();
</script>

<template>
  <div class="overflow-x-auto rounded-content border border-line bg-white/70">
    <table class="w-full border-collapse text-left text-sm">
      <caption class="sr-only">{{ caption }}</caption>
      <thead class="border-b border-line text-xs font-medium text-ink-subtle">
        <tr>
          <th v-for="column in columns" :key="column.key" scope="col" class="px-4 py-3">
            {{ column.label }}
          </th>
        </tr>
      </thead>
      <tbody class="divide-y divide-line text-ink">
        <tr
          v-for="row in rows"
          :key="String(row[rowKey])"
          class="transition-colors hover:bg-black/[0.025]"
        >
          <td v-for="column in columns" :key="column.key" class="px-4 py-3.5">
            <slot :name="`cell-${column.key}`" :row="row" :value="row[column.key]">
              {{ row[column.key] ?? "--" }}
            </slot>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
