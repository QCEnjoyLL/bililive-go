import React from 'react';
import './App.css';
import { Routes, Route } from 'react-router-dom';
import { ConfigProvider, theme as antdTheme } from 'antd';
import RootLayout from './component/layout/index';
import LiveList from './component/live-list/index';
import LiveInfo from './component/live-info/index';
import ConfigInfo from './component/config-info/index';
import FileList from './component/file-list/index';
import TaskPage from './component/task-page/index';
import IOStats from './component/io-stats/index';
import UpdateBanner from './component/update-banner/index';
import UpdatePage from './component/update-page/index';
import DanmakuSettings from './component/danmaku-config/index';
import RuntimeReadinessBanner from './component/runtime-readiness/index';
import {
  getThemeMode,
  getThemePalette,
  ResolvedThemeMode,
  setThemeMode,
  setThemePalette,
  ThemeMode,
  ThemePalette,
} from './utils/settings';
import { applyThemeVariables, getSystemThemeMode, getThemeColors } from './utils/theme';

const App: React.FC = () => {
  const [themeMode, setThemeModeState] = React.useState<ThemeMode>(() => getThemeMode());
  const [themePalette, setThemePaletteState] = React.useState<ThemePalette>(() => getThemePalette());
  const [systemThemeMode, setSystemThemeMode] = React.useState<ResolvedThemeMode>(() => getSystemThemeMode());
  const resolvedThemeMode: ResolvedThemeMode = themeMode === 'system' ? systemThemeMode : themeMode;
  const themeColors = React.useMemo(
    () => getThemeColors(resolvedThemeMode, themePalette),
    [resolvedThemeMode, themePalette]
  );

  React.useEffect(() => {
    const media = window.matchMedia?.('(prefers-color-scheme: dark)');
    if (!media) {
      return;
    }
    const handleChange = () => setSystemThemeMode(media.matches ? 'dark' : 'light');
    handleChange();
    media.addEventListener?.('change', handleChange);
    return () => media.removeEventListener?.('change', handleChange);
  }, []);

  React.useEffect(() => {
    document.documentElement.dataset.bgoTheme = resolvedThemeMode;
    document.documentElement.dataset.bgoThemeMode = themeMode;
    document.documentElement.dataset.bgoPalette = themePalette;
    applyThemeVariables(themeColors);
  }, [resolvedThemeMode, themeMode, themePalette, themeColors]);

  const handleThemeChange = (mode: ThemeMode) => {
    setThemeMode(mode);
    setThemeModeState(mode);
  };

  const handleThemePaletteChange = (palette: ThemePalette) => {
    setThemePalette(palette);
    setThemePaletteState(palette);
  };

  return (
    <ConfigProvider
      theme={{
        algorithm: resolvedThemeMode === 'dark' ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm,
        token: {
          colorPrimary: themeColors.accent,
          colorInfo: themeColors.accent,
          colorLink: themeColors.accent,
          colorPrimaryHover: themeColors.accentHover,
          colorBgLayout: themeColors.pageBg,
          colorBgContainer: themeColors.panelBg,
          colorBgElevated: themeColors.elevatedBg,
          colorText: themeColors.text,
          colorTextSecondary: themeColors.muted,
          colorBorder: themeColors.border,
          colorBorderSecondary: themeColors.borderSoft,
          borderRadius: 6,
        },
      }}
    >
      <UpdateBanner />
      <RuntimeReadinessBanner />
      <RootLayout
        themeMode={themeMode}
        resolvedThemeMode={resolvedThemeMode}
        themePalette={themePalette}
        onThemeChange={handleThemeChange}
        onThemePaletteChange={handleThemePaletteChange}
      >
        <Routes>
          <Route path="/update/*" element={<UpdatePage />} />
          <Route path="/iostats/*" element={<IOStats />} />
          <Route path="/tasks/*" element={<TaskPage />} />
          <Route path="/fileList/*" element={<FileList />} />
          <Route path="/danmaku" element={<DanmakuSettings />} />
          <Route path="/configInfo/*" element={<ConfigInfo />} />
          <Route path="/liveInfo" element={<LiveInfo />} />
          <Route path="/" element={<LiveList />} />
        </Routes>
      </RootLayout>
    </ConfigProvider>
  );
}

export default App;


