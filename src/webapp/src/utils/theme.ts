import { ResolvedThemeMode, ThemePalette } from './settings';

export interface ThemeColorSet {
  accent: string;
  accentHover: string;
  accentSoft: string;
  pageBg: string;
  contentBg: string;
  panelBg: string;
  elevatedBg: string;
  sidebarBg: string;
  headerBg: string;
  headerText: string;
  text: string;
  muted: string;
  border: string;
  borderSoft: string;
  headerHover: string;
  selectedBg: string;
  selectedText: string;
}

export interface ThemePaletteDefinition {
  key: ThemePalette;
  label: string;
  swatch: string;
  light: ThemeColorSet;
  dark: ThemeColorSet;
}

const makeLight = (
  accent: string,
  accentHover: string,
  accentSoft: string,
  overrides: Partial<ThemeColorSet> = {},
): ThemeColorSet => ({
  accent,
  accentHover,
  accentSoft,
  pageBg: '#f5f6f8',
  contentBg: '#ffffff',
  panelBg: '#ffffff',
  elevatedBg: '#f7f8fa',
  sidebarBg: '#eef2f6',
  headerBg: '#ffffff',
  headerText: '#17202a',
  text: '#17202a',
  muted: '#667085',
  border: '#d9dee7',
  borderSoft: '#edf0f4',
  headerHover: 'rgba(23, 32, 42, 0.07)',
  selectedBg: accentSoft,
  selectedText: accentHover,
  ...overrides,
});

const makeDark = (
  accent: string,
  accentHover: string,
  accentSoft: string,
  overrides: Partial<ThemeColorSet> = {},
): ThemeColorSet => ({
  accent,
  accentHover,
  accentSoft,
  pageBg: '#111820',
  contentBg: '#111820',
  panelBg: '#151b23',
  elevatedBg: '#1b222c',
  sidebarBg: '#182029',
  headerBg: '#111820',
  headerText: '#eef3f8',
  text: '#eef3f8',
  muted: '#9da7b3',
  border: '#2a333d',
  borderSoft: '#202832',
  headerHover: 'rgba(238, 243, 248, 0.10)',
  selectedBg: accentSoft,
  selectedText: accentHover,
  ...overrides,
});

const makePalette = (
  key: ThemePalette,
  label: string,
  swatch: string,
  lightAccent: string,
  lightHover: string,
  lightSoft: string,
  darkAccent: string,
  darkHover: string,
  darkSoft: string,
  lightOverrides: Partial<ThemeColorSet> = {},
  darkOverrides: Partial<ThemeColorSet> = {},
): ThemePaletteDefinition => ({
  key,
  label,
  swatch,
  light: makeLight(lightAccent, lightHover, lightSoft, lightOverrides),
  dark: makeDark(darkAccent, darkHover, darkSoft, darkOverrides),
});

const THEME_PALETTE_DEFINITIONS: ThemePaletteDefinition[] = [
  makePalette('one', 'One', '#61afef', '#4078f2', '#2864d8', 'rgba(64, 120, 242, 0.13)', '#61afef', '#7ec7ff', 'rgba(97, 175, 239, 0.20)', {
    pageBg: '#fafafa',
    sidebarBg: '#f0f2f5',
    text: '#24292f',
    headerText: '#24292f',
  }, {
    pageBg: '#282c34',
    contentBg: '#282c34',
    panelBg: '#2c313c',
    elevatedBg: '#343b48',
    sidebarBg: '#21252b',
    headerBg: '#21252b',
    text: '#d7dae0',
    muted: '#abb2bf',
    border: '#3e4451',
  }),
  makePalette('absolutely', 'Absolutely', '#cc7d5e', '#b85f43', '#9c4c34', 'rgba(184, 95, 67, 0.14)', '#cc7d5e', '#dc8c69', 'rgba(204, 125, 94, 0.22)', {
    pageBg: '#f5f1ec',
    sidebarBg: '#eee7df',
    panelBg: '#fffaf5',
    elevatedBg: '#f6eee7',
    border: '#dfd3c9',
    text: '#2d2926',
    headerText: '#2d2926',
  }, {
    pageBg: '#2d2d2b',
    contentBg: '#2d2d2b',
    panelBg: '#353532',
    elevatedBg: '#3d3d39',
    sidebarBg: '#242421',
    headerBg: '#242421',
    text: '#f9f9f7',
    headerText: '#f9f9f7',
    border: '#4b4944',
  }),
  makePalette('ayu', 'Ayu', '#ffb454', '#c46f00', '#9f5a00', 'rgba(196, 111, 0, 0.14)', '#ffb454', '#ffd580', 'rgba(255, 180, 84, 0.18)', {
    pageBg: '#faf7ef',
    sidebarBg: '#f0eadf',
    panelBg: '#fffdf7',
    text: '#1f2933',
  }, {
    pageBg: '#111722',
    contentBg: '#111722',
    panelBg: '#11151c',
    elevatedBg: '#1b202a',
    sidebarBg: '#141922',
    headerBg: '#11151c',
    text: '#e6e1cf',
    muted: '#9da8b8',
    border: '#27313f',
  }),
  makePalette('catppuccin', 'Catppuccin', '#cba6f7', '#8839ef', '#6c2bd9', 'rgba(136, 57, 239, 0.13)', '#cba6f7', '#ddb6ff', 'rgba(203, 166, 247, 0.18)', {
    pageBg: '#eff1f5',
    sidebarBg: '#e6e9ef',
    text: '#4c4f69',
    muted: '#6c6f85',
  }, {
    pageBg: '#1e1e2e',
    contentBg: '#1e1e2e',
    panelBg: '#181825',
    elevatedBg: '#252538',
    sidebarBg: '#181825',
    headerBg: '#181825',
    text: '#cdd6f4',
    muted: '#a6adc8',
    border: '#313244',
  }),
  makePalette('codex', 'Codex', '#0169cc', '#0b6bcb', '#0759ad', 'rgba(11, 107, 203, 0.12)', '#0169cc', '#218bff', 'rgba(1, 105, 204, 0.20)', {
    pageBg: '#f6f7f9',
    sidebarBg: '#eef2f5',
    headerText: '#1c252e',
    text: '#17202a',
  }, {
    pageBg: '#111820',
    contentBg: '#111820',
    panelBg: '#151b23',
    elevatedBg: '#1b222c',
    sidebarBg: '#18212a',
    headerBg: '#111820',
  }),
  makePalette('dracula', 'Dracula', '#bd93f9', '#7c3aed', '#6d28d9', 'rgba(124, 58, 237, 0.13)', '#bd93f9', '#d6acff', 'rgba(189, 147, 249, 0.20)', {
    pageBg: '#f8f7ff',
    sidebarBg: '#eeebff',
    text: '#282a36',
  }, {
    pageBg: '#282a36',
    contentBg: '#282a36',
    panelBg: '#21222c',
    elevatedBg: '#343746',
    sidebarBg: '#21222c',
    headerBg: '#1f2029',
    text: '#f8f8f2',
    muted: '#c7c4d8',
    border: '#44475a',
  }),
  makePalette('everforest', 'Everforest', '#a7c080', '#6c8f43', '#557136', 'rgba(108, 143, 67, 0.14)', '#a7c080', '#b9d18f', 'rgba(167, 192, 128, 0.18)', {
    pageBg: '#f4f0d9',
    sidebarBg: '#e8e0bf',
    panelBg: '#fffbea',
    text: '#3c4841',
    border: '#d8cfad',
  }, {
    pageBg: '#2d353b',
    contentBg: '#2d353b',
    panelBg: '#343f44',
    elevatedBg: '#3d484d',
    sidebarBg: '#263238',
    headerBg: '#263238',
    text: '#d3c6aa',
    muted: '#a7ad9a',
    border: '#4f585e',
  }),
  makePalette('github', 'GitHub', '#1f6feb', '#0969da', '#0756b6', 'rgba(9, 105, 218, 0.12)', '#1f6feb', '#388bfd', 'rgba(31, 111, 235, 0.20)', {
    pageBg: '#f6f8fa',
    sidebarBg: '#f0f3f6',
    border: '#d0d7de',
  }, {
    pageBg: '#0d1117',
    contentBg: '#0d1117',
    panelBg: '#161b22',
    elevatedBg: '#21262d',
    sidebarBg: '#161b22',
    headerBg: '#0d1117',
    border: '#30363d',
  }),
  makePalette('gruvbox', 'Gruvbox', '#fabd2f', '#af6f00', '#8f5900', 'rgba(175, 111, 0, 0.15)', '#fabd2f', '#ffd75f', 'rgba(250, 189, 47, 0.18)', {
    pageBg: '#fbf1c7',
    sidebarBg: '#ebdbb2',
    panelBg: '#fff7d7',
    text: '#3c3836',
    border: '#d5c4a1',
  }, {
    pageBg: '#282828',
    contentBg: '#282828',
    panelBg: '#32302f',
    elevatedBg: '#3c3836',
    sidebarBg: '#1d2021',
    headerBg: '#1d2021',
    text: '#ebdbb2',
    muted: '#bdae93',
    border: '#504945',
  }),
  makePalette('linear', 'Linear', '#5e6ad2', '#5e6ad2', '#4f5bbc', 'rgba(94, 106, 210, 0.13)', '#5e6ad2', '#7c86e8', 'rgba(94, 106, 210, 0.20)', {
    pageBg: '#f7f7f8',
    sidebarBg: '#f0f0f2',
    text: '#1f2023',
  }, {
    pageBg: '#121416',
    contentBg: '#121416',
    panelBg: '#17191d',
    elevatedBg: '#1c1f25',
    sidebarBg: '#15171b',
    headerBg: '#101114',
    text: '#f7f8f8',
    muted: '#a6a8ad',
    border: '#2a2d33',
  }),
  makePalette('lobster', 'Lobster', '#e5534b', '#c23a2f', '#9f2f27', 'rgba(194, 58, 47, 0.14)', '#ff7a70', '#ff9a92', 'rgba(255, 122, 112, 0.20)', {
    pageBg: '#fff4f1',
    sidebarBg: '#f8e4df',
    panelBg: '#fffaf8',
    text: '#35211f',
    border: '#ead0ca',
  }, {
    pageBg: '#2b1e22',
    contentBg: '#2b1e22',
    panelBg: '#33242a',
    elevatedBg: '#3b2a31',
    sidebarBg: '#251a1f',
    headerBg: '#251a1f',
    text: '#ffece7',
    muted: '#d8b7b0',
    border: '#563a42',
  }),
  makePalette('material', 'Material', '#80cbc4', '#1976d2', '#115293', 'rgba(25, 118, 210, 0.13)', '#80cbc4', '#a7ffeb', 'rgba(128, 203, 196, 0.20)', {
    pageBg: '#f6f8fb',
    sidebarBg: '#edf2f7',
    text: '#263238',
  }, {
    pageBg: '#263238',
    contentBg: '#263238',
    panelBg: '#2f3d46',
    elevatedBg: '#37474f',
    sidebarBg: '#202b31',
    headerBg: '#202b31',
    text: '#eeffff',
    muted: '#b0bec5',
    border: '#455a64',
  }),
  makePalette('matrix', 'Matrix', '#00d26a', '#0f8f4a', '#0b6f39', 'rgba(15, 143, 74, 0.14)', '#00d26a', '#50fa7b', 'rgba(0, 210, 106, 0.18)', {
    pageBg: '#f1fbf4',
    sidebarBg: '#e1f3e8',
    text: '#14351f',
    border: '#c7e6d2',
  }, {
    pageBg: '#122116',
    contentBg: '#122116',
    panelBg: '#16291c',
    elevatedBg: '#1c3323',
    sidebarBg: '#101d14',
    headerBg: '#101d14',
    text: '#d7ffe4',
    muted: '#8fbc9a',
    border: '#2a4c35',
  }),
  makePalette('monokai', 'Monokai', '#a6e22e', '#6f8f00', '#536d00', 'rgba(111, 143, 0, 0.16)', '#a6e22e', '#c2ff55', 'rgba(166, 226, 46, 0.18)', {
    pageBg: '#f7f4ea',
    sidebarBg: '#ece5d6',
    panelBg: '#fffaf0',
    text: '#272822',
    border: '#ddd4c0',
  }, {
    pageBg: '#272822',
    contentBg: '#272822',
    panelBg: '#303127',
    elevatedBg: '#383a2e',
    sidebarBg: '#20211c',
    headerBg: '#20211c',
    text: '#f8f8f2',
    muted: '#cfcfc2',
    border: '#49483e',
  }),
  makePalette('night-owl', 'Night Owl', '#82aaff', '#3268d8', '#2450ad', 'rgba(50, 104, 216, 0.14)', '#82aaff', '#b4ccff', 'rgba(130, 170, 255, 0.20)', {
    pageBg: '#eef5ff',
    sidebarBg: '#e1ecfb',
    text: '#16243d',
    border: '#cbd9ef',
  }, {
    pageBg: '#101a2a',
    contentBg: '#101a2a',
    panelBg: '#12213a',
    elevatedBg: '#172a46',
    sidebarBg: '#0e1726',
    headerBg: '#0e1726',
    text: '#d6deeb',
    muted: '#8fa6c9',
    border: '#263c5a',
  }),
  makePalette('nord', 'Nord', '#88c0d0', '#4c7a92', '#3b6479', 'rgba(76, 122, 146, 0.14)', '#88c0d0', '#a3d7e6', 'rgba(136, 192, 208, 0.20)', {
    pageBg: '#eceff4',
    sidebarBg: '#e5e9f0',
    text: '#2e3440',
    border: '#d8dee9',
  }, {
    pageBg: '#2e3440',
    contentBg: '#2e3440',
    panelBg: '#343b49',
    elevatedBg: '#3b4252',
    sidebarBg: '#252b35',
    headerBg: '#252b35',
    text: '#eceff4',
    muted: '#b7c0d0',
    border: '#4c566a',
  }),
  makePalette('notion', 'Notion', '#2f3437', '#2f3437', '#1f2326', 'rgba(47, 52, 55, 0.12)', '#e9e5dc', '#ffffff', 'rgba(233, 229, 220, 0.16)', {
    pageBg: '#f7f6f3',
    sidebarBg: '#eeeae4',
    panelBg: '#ffffff',
    elevatedBg: '#f2f0eb',
    text: '#2f3437',
    border: '#ded9d1',
  }, {
    pageBg: '#191919',
    contentBg: '#191919',
    panelBg: '#202020',
    elevatedBg: '#2a2a2a',
    sidebarBg: '#202020',
    headerBg: '#191919',
    text: '#f1f1ef',
    muted: '#aaa7a2',
    border: '#373737',
  }),
  makePalette('oscurance', 'Oscurance', '#9f8cff', '#7357d8', '#5b43b0', 'rgba(115, 87, 216, 0.14)', '#b4a7ff', '#d2caff', 'rgba(180, 167, 255, 0.20)', {
    pageBg: '#f5f3ff',
    sidebarBg: '#ece8ff',
    text: '#27213f',
    border: '#d8d0f5',
  }, {
    pageBg: '#1c1b2e',
    contentBg: '#1c1b2e',
    panelBg: '#23223a',
    elevatedBg: '#2c2a45',
    sidebarBg: '#171629',
    headerBg: '#171629',
    text: '#efeaff',
    muted: '#b9b0d7',
    border: '#3d395f',
  }),
  makePalette('raycast', 'Raycast', '#ff6363', '#e5484d', '#c7353b', 'rgba(229, 72, 77, 0.14)', '#ff6363', '#ff8a8a', 'rgba(255, 99, 99, 0.20)', {
    pageBg: '#fff5f5',
    sidebarBg: '#ffe7e7',
    text: '#311c1f',
    border: '#f0caca',
  }, {
    pageBg: '#23191a',
    contentBg: '#23191a',
    panelBg: '#2d2021',
    elevatedBg: '#382829',
    sidebarBg: '#1e1516',
    headerBg: '#1e1516',
    text: '#fff1f1',
    muted: '#d7adad',
    border: '#523334',
  }),
  makePalette('rose-pine', 'Rose Pine', '#eb6f92', '#d7827e', '#b4637a', 'rgba(215, 130, 126, 0.15)', '#eb6f92', '#f6a1b6', 'rgba(235, 111, 146, 0.20)', {
    pageBg: '#faf4ed',
    sidebarBg: '#f2e9de',
    panelBg: '#fffaf6',
    text: '#575279',
    border: '#dfdad9',
  }, {
    pageBg: '#191724',
    contentBg: '#191724',
    panelBg: '#1f1d2e',
    elevatedBg: '#26233a',
    sidebarBg: '#171521',
    headerBg: '#171521',
    text: '#e0def4',
    muted: '#908caa',
    border: '#403d52',
  }),
  makePalette('sentry', 'Sentry', '#6c5fc7', '#5f4bb6', '#4c3b91', 'rgba(95, 75, 182, 0.14)', '#c59cff', '#d8baff', 'rgba(197, 156, 255, 0.20)', {
    pageBg: '#f8f4ff',
    sidebarBg: '#eee7fb',
    text: '#30263f',
    border: '#dccff0',
  }, {
    pageBg: '#241b2f',
    contentBg: '#241b2f',
    panelBg: '#2b2138',
    elevatedBg: '#352947',
    sidebarBg: '#201729',
    headerBg: '#201729',
    text: '#f5edff',
    muted: '#c1acd7',
    border: '#49385f',
  }),
  makePalette('solarized', 'Solarized', '#268bd2', '#268bd2', '#1f6f9f', 'rgba(38, 139, 210, 0.14)', '#2aa198', '#55d6c2', 'rgba(42, 161, 152, 0.20)', {
    pageBg: '#fdf6e3',
    sidebarBg: '#eee8d5',
    panelBg: '#fffaf0',
    text: '#586e75',
    border: '#d8cfb0',
  }, {
    pageBg: '#073642',
    contentBg: '#073642',
    panelBg: '#0b404d',
    elevatedBg: '#124b59',
    sidebarBg: '#002b36',
    headerBg: '#002b36',
    text: '#eee8d5',
    muted: '#93a1a1',
    border: '#2f5d68',
  }),
  makePalette('temple', 'Temple', '#d49b34', '#a66c1f', '#805218', 'rgba(166, 108, 31, 0.15)', '#f6c177', '#ffd59a', 'rgba(246, 193, 119, 0.20)', {
    pageBg: '#f7f1df',
    sidebarBg: '#ebe0c3',
    panelBg: '#fff9ea',
    text: '#342d21',
    border: '#d9c9a5',
  }, {
    pageBg: '#24201a',
    contentBg: '#24201a',
    panelBg: '#2d281f',
    elevatedBg: '#383124',
    sidebarBg: '#1f1b16',
    headerBg: '#1f1b16',
    text: '#f6ead0',
    muted: '#c7b48e',
    border: '#51442f',
  }),
  makePalette('tokyo-night', 'Tokyo Night', '#7aa2f7', '#3d68d8', '#2d50aa', 'rgba(61, 104, 216, 0.14)', '#7aa2f7', '#9ab8ff', 'rgba(122, 162, 247, 0.20)', {
    pageBg: '#f1f5ff',
    sidebarBg: '#e5ebfa',
    text: '#1f2335',
    border: '#cfd8ee',
  }, {
    pageBg: '#1a1b26',
    contentBg: '#1a1b26',
    panelBg: '#202331',
    elevatedBg: '#292e42',
    sidebarBg: '#16161e',
    headerBg: '#16161e',
    text: '#c0caf5',
    muted: '#9aa5ce',
    border: '#3b4261',
  }),
  makePalette('vercel', 'Vercel', '#111827', '#111827', '#0f172a', 'rgba(17, 24, 39, 0.12)', '#f5f5f5', '#ffffff', 'rgba(245, 245, 245, 0.14)', {
    pageBg: '#fafafa',
    sidebarBg: '#f0f0f0',
    text: '#111827',
    border: '#dedede',
  }, {
    pageBg: '#171717',
    contentBg: '#171717',
    panelBg: '#1f1f1f',
    elevatedBg: '#292929',
    sidebarBg: '#202020',
    headerBg: '#171717',
    text: '#f5f5f5',
    muted: '#a3a3a3',
    border: '#3f3f3f',
  }),
  makePalette('vs-code-plus', 'VS Code Plus', '#007acc', '#007acc', '#0067ad', 'rgba(0, 122, 204, 0.13)', '#007acc', '#2499e8', 'rgba(0, 122, 204, 0.22)', {
    pageBg: '#f3f3f3',
    sidebarBg: '#ebebeb',
    text: '#1f1f1f',
    border: '#d4d4d4',
  }, {
    pageBg: '#1e1e1e',
    contentBg: '#1e1e1e',
    panelBg: '#252526',
    elevatedBg: '#2d2d30',
    sidebarBg: '#252526',
    headerBg: '#1e1e1e',
    text: '#cccccc',
    muted: '#a7a7a7',
    border: '#3c3c3c',
  }),
  makePalette('xcode', 'Xcode', '#007aff', '#007aff', '#005ecb', 'rgba(0, 122, 255, 0.13)', '#0a84ff', '#5eb1ff', 'rgba(10, 132, 255, 0.22)', {
    pageBg: '#f7fbff',
    sidebarBg: '#edf5ff',
    text: '#1d1d1f',
    border: '#d5e3f5',
  }, {
    pageBg: '#242833',
    contentBg: '#242833',
    panelBg: '#2b303d',
    elevatedBg: '#343b4c',
    sidebarBg: '#20242e',
    headerBg: '#20242e',
    text: '#f2f2f7',
    muted: '#b9c2d0',
    border: '#465066',
  }),
];

export const DEFAULT_THEME_PALETTE: ThemePalette = 'one';
export const THEME_PALETTES: ThemePaletteDefinition[] = [...THEME_PALETTE_DEFINITIONS]
  .sort((a, b) => a.key.localeCompare(b.key));

export function getPaletteDefinition(palette: ThemePalette): ThemePaletteDefinition {
  return THEME_PALETTES.find((item) => item.key === palette)
    || THEME_PALETTES.find((item) => item.key === DEFAULT_THEME_PALETTE)
    || THEME_PALETTES[0];
}

export function getThemeColors(mode: ResolvedThemeMode, palette: ThemePalette): ThemeColorSet {
  const definition = getPaletteDefinition(palette);
  return definition[mode];
}

export function getSystemThemeMode(): ResolvedThemeMode {
  if (typeof window !== 'undefined' && window.matchMedia?.('(prefers-color-scheme: dark)').matches) {
    return 'dark';
  }
  return 'light';
}

export function applyThemeVariables(colors: ThemeColorSet): void {
  const root = document.documentElement;
  const entries: Array<[string, string]> = [
    ['--bgo-accent', colors.accent],
    ['--bgo-accent-hover', colors.accentHover],
    ['--bgo-accent-soft', colors.accentSoft],
    ['--bgo-page-bg', colors.pageBg],
    ['--bgo-content-bg', colors.contentBg],
    ['--bgo-panel-bg', colors.panelBg],
    ['--bgo-elevated-bg', colors.elevatedBg],
    ['--bgo-sidebar-bg', colors.sidebarBg],
    ['--bgo-header-bg', colors.headerBg],
    ['--bgo-header-text', colors.headerText],
    ['--bgo-text', colors.text],
    ['--bgo-muted', colors.muted],
    ['--bgo-border', colors.border],
    ['--bgo-border-soft', colors.borderSoft],
    ['--bgo-header-hover', colors.headerHover],
    ['--bgo-selected-bg', colors.selectedBg],
    ['--bgo-selected-text', colors.selectedText],
  ];
  entries.forEach(([key, value]) => root.style.setProperty(key, value));
}
