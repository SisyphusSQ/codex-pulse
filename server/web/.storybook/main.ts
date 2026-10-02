import type { StorybookConfig } from '@storybook/react-vite';

const config: StorybookConfig = {
  framework: '@storybook/react-vite',
  stories: ['../src/design/**/*.stories.tsx'],
  addons: ['@storybook/addon-docs'],
  core: { disableTelemetry: true, builder: { name: '@storybook/builder-vite', options: { viteConfigPath: '.storybook/vite.config.ts' } } },
  typescript: { reactDocgen: false },
};
export default config;
