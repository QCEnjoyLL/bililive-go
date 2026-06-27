import React from 'react';
import { HashRouter as Router, Link } from 'react-router-dom';
import { Layout, Menu, Button, Tooltip, Popconfirm, message, Dropdown } from 'antd';
import type { MenuProps } from 'antd';
import {
    BgColorsOutlined,
    CheckOutlined,
    DesktopOutlined,
    MonitorOutlined,
    UnorderedListOutlined,
    DashboardOutlined,
    SettingOutlined,
    FolderOutlined,
    ToolOutlined,
    MenuFoldOutlined,
    MenuUnfoldOutlined,
    LineChartOutlined,
    CloudUploadOutlined,
    CalendarOutlined,
    CommentOutlined,
    LogoutOutlined,
    MoonOutlined,
    SunOutlined
} from '@ant-design/icons';
import './layout.css';
import { ResolvedThemeMode, ThemeMode, ThemePalette } from '../../utils/settings';
import { getPaletteDefinition, getThemeColors, THEME_PALETTES } from '../../utils/theme';

const { Header, Content, Sider } = Layout;

interface Props {
    children?: React.ReactNode;
    themeMode: ThemeMode;
    resolvedThemeMode: ResolvedThemeMode;
    themePalette: ThemePalette;
    onThemeChange: (mode: ThemeMode) => void;
    onThemePaletteChange: (palette: ThemePalette) => void;
}

interface State {
    collapsed: boolean;
    loggingOut: boolean;
}

// localStorage key 用于保存侧边栏收起状态
const SIDER_COLLAPSED_KEY = 'siderCollapsed';

class RootLayout extends React.Component<Props, State> {
    constructor(props: Props) {
        super(props);
        // 从 localStorage 读取收起状态
        let collapsed = false;
        try {
            const saved = localStorage.getItem(SIDER_COLLAPSED_KEY);
            if (saved !== null) {
                collapsed = saved === 'true';
            }
        } catch (e) {
            console.error('读取侧边栏状态失败:', e);
        }
        this.state = { collapsed, loggingOut: false };
    }

    toggleCollapsed = () => {
        const collapsed = !this.state.collapsed;
        this.setState({ collapsed });
        // 保存到 localStorage
        try {
            localStorage.setItem(SIDER_COLLAPSED_KEY, String(collapsed));
        } catch (e) {
            console.error('保存侧边栏状态失败:', e);
        }
    };

    handleLogout = async () => {
        this.setState({ loggingOut: true });
        try {
            await fetch('/api/auth/logout', { method: 'POST' });
        } catch (error) {
            message.warning('退出请求未完成，将返回登录页');
        } finally {
            window.location.assign('/login?next=%2F');
        }
    };

    render() {
        const { collapsed, loggingOut } = this.state;
        const {
            themeMode,
            resolvedThemeMode,
            themePalette,
            onThemeChange,
            onThemePaletteChange
        } = this.props;
        const isDark = resolvedThemeMode === 'dark';
        const selectedPalette = getPaletteDefinition(themePalette);
        const modeOptions: Array<{ key: ThemeMode; label: string; icon: React.ReactNode }> = [
            { key: 'system', label: '跟随系统', icon: <DesktopOutlined /> },
            { key: 'light', label: '浅色', icon: <SunOutlined /> },
            { key: 'dark', label: '深色', icon: <MoonOutlined /> },
        ];
        const appearanceItems: MenuProps['items'] = [
            {
                key: 'mode',
                type: 'group',
                label: '模式',
                children: modeOptions.map((item) => ({
                    key: `mode-${item.key}`,
                    icon: item.icon,
                    label: (
                        <span className="appearance-menu-row">
                            <span>{item.label}</span>
                            {themeMode === item.key && <CheckOutlined className="appearance-check" />}
                        </span>
                    ),
                })),
            },
            { key: 'divider', type: 'divider' },
            {
                key: 'palette',
                type: 'group',
                label: '配色',
                children: THEME_PALETTES.map((item) => {
                    const colors = getThemeColors(resolvedThemeMode, item.key);
                    return {
                        key: `palette-${item.key}`,
                        label: (
                            <span className="appearance-menu-row">
                                <span className="appearance-palette-label">
                                    <span
                                        className="appearance-swatch"
                                        style={{ background: colors.accent }}
                                    />
                                    <span>{item.label}</span>
                                </span>
                                {themePalette === item.key && <CheckOutlined className="appearance-check" />}
                            </span>
                        ),
                    };
                }),
            },
        ];
        const onAppearanceClick: MenuProps['onClick'] = ({ key }) => {
            const keyText = String(key);
            if (keyText.startsWith('mode-')) {
                onThemeChange(keyText.replace('mode-', '') as ThemeMode);
                return;
            }
            if (keyText.startsWith('palette-')) {
                onThemePaletteChange(keyText.replace('palette-', '') as ThemePalette);
            }
        };
        return (
            <Router>
                <Layout className="all-layout">
                    <Header className="header small-header app-header">
                        <div className="app-brand">
                            <img className="app-brand-logo" src="/favicon.ico" alt="" />
                            <h3 className="logo-text">Bililive-go</h3>
                        </div>
                        <div className="app-actions">
                            <Dropdown
                                menu={{ items: appearanceItems, onClick: onAppearanceClick }}
                                trigger={['click']}
                                placement="bottomRight"
                                classNames={{ root: 'appearance-dropdown' }}
                            >
                                <Button
                                    className="appearance-button"
                                    type="text"
                                    icon={<BgColorsOutlined />}
                                    aria-label="外观"
                                    title="外观"
                                >
                                    <span
                                        className="appearance-button-swatch"
                                        style={{ background: selectedPalette.swatch }}
                                    />
                                    <span className="appearance-button-text">{selectedPalette.label}</span>
                                </Button>
                            </Dropdown>
                            <Popconfirm
                                title="退出登录"
                                description="确定要退出当前 WebUI 会话吗？"
                                okText="退出"
                                cancelText="取消"
                                onConfirm={this.handleLogout}
                            >
                                <Tooltip title="退出登录">
                                    <Button
                                        className="logout-button"
                                        type="text"
                                        icon={<LogoutOutlined />}
                                        loading={loggingOut}
                                    >
                                        退出
                                    </Button>
                                </Tooltip>
                            </Popconfirm>
                        </div>
                    </Header>
                    <Layout>
                        <Sider
                            className="side-bar"
                            width={200}
                            collapsedWidth={60}
                            trigger={null}
                            collapsible
                            collapsed={collapsed}
                        >
                            {/* 折叠按钮在顶部，与菜单图标对齐 */}
                            <div className="sider-collapse">
                                <Button
                                    type="text"
                                    icon={collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
                                    onClick={this.toggleCollapsed}
                                    style={{
                                        fontSize: 16,
                                        width: '100%',
                                        textAlign: 'left',
                                        paddingLeft: collapsed ? 20 : 24,
                                        height: 40
                                    }}
                                >
                                    {!collapsed && '收起菜单'}
                                </Button>
                            </div>
                            <Menu
                                mode="inline"
                                defaultSelectedKeys={['1']}
                                inlineCollapsed={collapsed}
                                theme={isDark ? 'dark' : 'light'}
                                className="side-menu"
                                items={[
                                    {
                                        key: '1',
                                        icon: <MonitorOutlined />,
                                        label: <Link to="/">监控列表</Link>,
                                    },
                                    {
                                        key: '2',
                                        icon: <DashboardOutlined />,
                                        label: <Link to="/liveInfo">系统状态</Link>,
                                    },
                                    {
                                        key: '3',
                                        icon: <SettingOutlined />,
                                        label: <Link to="/configInfo">设置</Link>,
                                    },
                                    {
                                        key: 'danmaku',
                                        icon: <CommentOutlined />,
                                        label: <Link to="/danmaku">弹幕</Link>,
                                    },
                                    {
                                        key: '4',
                                        icon: <FolderOutlined />,
                                        label: <Link to="/fileList">文件</Link>,
                                    },
                                    {
                                        key: '5',
                                        icon: <ToolOutlined />,
                                        label: <a href="/tools/" target="_blank" rel="noopener noreferrer">工具</a>,
                                    },
                                    {
                                        key: 'tasks',
                                        icon: <UnorderedListOutlined />,
                                        label: <Link to="/tasks">任务队列</Link>,
                                    },
                                    {
                                        key: 'scheduler',
                                        icon: <CalendarOutlined />,
                                        label: <a href="/scheduler/" target="_blank" rel="noopener noreferrer">调度器</a>,
                                    },
                                    {
                                        key: 'iostats',
                                        icon: <LineChartOutlined />,
                                        label: <Link to="/iostats">IO 统计</Link>,
                                    },
                                    {
                                        key: 'update',
                                        icon: <CloudUploadOutlined />,
                                        label: <Link to="/update">更新</Link>,
                                    }
                                ]}
                            />
                        </Sider>
                        <Layout className="content-padding">
                            <Content
                                className="inside-content-padding app-content"
                                style={{
                                    margin: 0,
                                    minHeight: 280,
                                    overflow: "auto",
                                }}>
                                {this.props.children}
                            </Content>
                        </Layout>
                    </Layout>
                </Layout>
            </Router>
        )
    }
}

export default RootLayout;
