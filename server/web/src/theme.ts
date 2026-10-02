import type { ThemeConfig } from 'antd';

export const pulseTheme: ThemeConfig = {
  cssVar: { key: 'pulse' },
  token: {
    colorPrimary: '#2678f5', colorInfo: '#2678f5', colorText: '#182338',
    colorTextSecondary: '#617089', colorTextTertiary: '#718096',
    colorBgLayout: '#f7f9fc', colorBorder: '#dce3ed', colorBorderSecondary: '#edf1f6',
    borderRadius: 6, borderRadiusLG: 8, fontSize: 14, controlHeight: 32,
    fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif',
  },
  components: {
    Layout: { headerBg: '#ffffff', siderBg: '#f2f6fc', headerHeight: 56, footerBg: '#f7f9fc' },
    Menu: { itemBg: 'transparent', subMenuItemBg: 'transparent', itemHeight: 40, itemMarginInline: 12, itemBorderRadius: 6, itemSelectedBg: '#e4efff', itemSelectedColor: '#2678f5' },
    Card: { headerHeight: 48, headerFontSize: 15, bodyPadding: 18 },
    Table: { headerBg: '#f7f9fc', headerColor: '#617089', cellPaddingBlockSM: 10, cellPaddingInlineSM: 12, fontSize: 13 },
    Statistic: { contentFontSize: 30, titleFontSize: 13 },
    Segmented: { trackBg: '#f3f6fb', itemSelectedBg: '#e4efff', itemSelectedColor: '#2678f5' },
  },
};
