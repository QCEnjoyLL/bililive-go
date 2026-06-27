/**
 * 本地存储设置管理器
 * 用于管理用户在当前浏览器中的个人偏好设置
 * 这些设置不会保存到服务器配置中
 */

const SETTINGS_KEY = 'bililive_go_local_settings';

export interface LocalSettings {
  // 是否启用 SSE 更新监控列表（默认启用）
  enableListSSE: boolean;
  // SSE 更新的轮询间隔（秒），当禁用 SSE 时使用 REST API 轮询
  pollIntervalSeconds: number;
  // WebUI 主题模式
  themeMode: ThemeMode;
  // WebUI 配色方案
  themePalette: ThemePalette;
}

export type ThemeMode = 'system' | 'light' | 'dark';
export type ResolvedThemeMode = 'light' | 'dark';
export const THEME_PALETTE_KEYS = [
  'one',
  'absolutely',
  'ayu',
  'catppuccin',
  'codex',
  'dracula',
  'everforest',
  'github',
  'gruvbox',
  'linear',
  'lobster',
  'material',
  'matrix',
  'monokai',
  'night-owl',
  'nord',
  'notion',
  'oscurance',
  'raycast',
  'rose-pine',
  'sentry',
  'solarized',
  'temple',
  'tokyo-night',
  'vercel',
  'vs-code-plus',
  'xcode',
] as const;
export type ThemePalette = (typeof THEME_PALETTE_KEYS)[number];

const DEFAULT_SETTINGS: LocalSettings = {
  enableListSSE: true,
  pollIntervalSeconds: 180, // 3分钟
  themeMode: 'system',
  themePalette: 'one',
};

/**
 * 获取本地设置
 */
export function getLocalSettings(): LocalSettings {
  try {
    const stored = localStorage.getItem(SETTINGS_KEY);
    if (stored) {
      const parsed = JSON.parse(stored);
      return { ...DEFAULT_SETTINGS, ...parsed };
    }
  } catch (error) {
    console.error('Failed to load local settings:', error);
  }
  return { ...DEFAULT_SETTINGS };
}

/**
 * 保存本地设置
 */
export function saveLocalSettings(settings: Partial<LocalSettings>): void {
  try {
    const current = getLocalSettings();
    const updated = { ...current, ...settings };
    localStorage.setItem(SETTINGS_KEY, JSON.stringify(updated));
    // 触发自定义事件，通知其他组件设置已更改
    window.dispatchEvent(new CustomEvent('localSettingsChanged', { detail: updated }));
  } catch (error) {
    console.error('Failed to save local settings:', error);
  }
}

/**
 * 获取是否启用列表 SSE
 */
export function isListSSEEnabled(): boolean {
  return getLocalSettings().enableListSSE;
}

/**
 * 设置是否启用列表 SSE
 */
export function setListSSEEnabled(enabled: boolean): void {
  saveLocalSettings({ enableListSSE: enabled });
}

/**
 * 获取轮询间隔（毫秒）
 */
export function getPollIntervalMs(): number {
  return getLocalSettings().pollIntervalSeconds * 1000;
}

/**
 * 获取 WebUI 主题模式
 */
export function getThemeMode(): ThemeMode {
  const mode = getLocalSettings().themeMode;
  return mode === 'dark' || mode === 'light' || mode === 'system' ? mode : 'system';
}

/**
 * 设置 WebUI 主题模式
 */
export function setThemeMode(mode: ThemeMode): void {
  saveLocalSettings({ themeMode: mode });
}

/**
 * 获取 WebUI 配色方案
 */
export function getThemePalette(): ThemePalette {
  const palette = getLocalSettings().themePalette;
  return THEME_PALETTE_KEYS.includes(palette as ThemePalette) ? palette as ThemePalette : 'one';
}

/**
 * 设置 WebUI 配色方案
 */
export function setThemePalette(palette: ThemePalette): void {
  saveLocalSettings({ themePalette: palette });
}
