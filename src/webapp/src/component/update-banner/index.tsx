import React, { useState, useEffect, useCallback, useRef } from 'react';
import { Alert, Button, Modal, Progress, Typography, Space, Tag, Divider, message } from 'antd';
import {
  SyncOutlined,
  DownloadOutlined,
  CheckCircleOutlined,
  ExclamationCircleOutlined,
  CloseCircleOutlined,
  CloudDownloadOutlined,
  ReloadOutlined
} from '@ant-design/icons';
import { subscribeSSE, unsubscribeSSE, SSEMessage } from '../../utils/sse';
import API from '../../utils/api';
import './update-banner.css';

const api = new API();
const { Text, Paragraph } = Typography;
const UPDATE_MESSAGE_KEY = 'program-update-banner';

interface UpdateInfo {
  version: string;
  release_date?: string;
  changelog?: string;
  prerelease?: boolean;
  asset_name?: string;
  asset_size?: number;
}

interface DownloadProgress {
  downloaded_bytes: number;
  total_bytes: number;
  speed: number;
  percentage: number;
}

interface UpdateStatus {
  state: string;
  available_info?: UpdateInfo;
  progress?: DownloadProgress;
  error?: string;
  can_apply_now?: boolean;
  active_recordings_count?: number;
  graceful_update_pending?: boolean;
  graceful_update_version?: string;
}

const isAuthError = (err: any) => {
  const msg = String(err?.message || err || '');
  return err?.status === 401 || err?.status === 403 || msg.includes('请先登录 WebUI');
};

const loginRedirectURL = () => {
  const next = `${window.location.pathname}${window.location.search}${window.location.hash}` || '/';
  return `/login?next=${encodeURIComponent(next)}`;
};

const UpdateBanner: React.FC = () => {
  const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(null);
  const [downloadProgress, setDownloadProgress] = useState<DownloadProgress | null>(null);
  const [isReady, setIsReady] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [activeRecordings, setActiveRecordings] = useState(0);
  const [showModal, setShowModal] = useState(false);
  const [applying, setApplying] = useState(false);
  const [restarting, setRestarting] = useState(false);
  const [restartSeconds, setRestartSeconds] = useState(0);
  const [dismissed, setDismissed] = useState(false);
  const restartPollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const clearRestartPoll = useCallback(() => {
    if (restartPollRef.current) {
      clearInterval(restartPollRef.current);
      restartPollRef.current = null;
    }
  }, []);

  const redirectToLogin = useCallback(() => {
    message.warning({ content: '更新已完成，需要重新登录 WebUI', key: UPDATE_MESSAGE_KEY, duration: 2 });
    setTimeout(() => {
      window.location.assign(loginRedirectURL());
    }, 800);
  }, []);

  const waitForServerRestart = useCallback(() => {
    clearRestartPoll();
    setApplying(true);
    setRestarting(true);
    setRestartSeconds(0);
    setError(null);
    message.loading({ content: '更新已提交，正在等待服务重启...', key: UPDATE_MESSAGE_KEY, duration: 0 });

    let elapsed = 0;
    restartPollRef.current = setInterval(async () => {
      elapsed += 2;
      setRestartSeconds(elapsed);

      try {
        const res = await fetch('/api/info', {
          cache: 'no-store',
          credentials: 'same-origin',
          redirect: 'manual',
          signal: AbortSignal.timeout(2000),
        });

        if (res.ok) {
          clearRestartPoll();
          message.success({ content: '更新已完成，正在刷新页面...', key: UPDATE_MESSAGE_KEY, duration: 2 });
          setTimeout(() => window.location.reload(), 800);
          return;
        }

        if (res.status === 401 || res.status === 403) {
          clearRestartPoll();
          redirectToLogin();
          return;
        }
      } catch {
        // 服务重启期间请求失败是正常情况，继续轮询。
      }

      if (elapsed >= 90) {
        clearRestartPoll();
        setApplying(false);
        setRestarting(false);
        setError('更新请求已提交，但等待服务重启超时。请手动刷新页面，或重新打开 WebUI。');
        message.warning({ content: '等待服务重启超时，请手动刷新页面', key: UPDATE_MESSAGE_KEY, duration: 5 });
      }
    }, 2000);
  }, [clearRestartPoll, redirectToLogin]);

  const handleSSEMessage = useCallback((messageEvent: SSEMessage) => {
    const { data } = messageEvent;

    switch (messageEvent.type) {
      case 'update_available':
        setUpdateInfo(data as UpdateInfo);
        setError(null);
        setDismissed(false);
        break;

      case 'update_downloading':
        setDownloadProgress(data as DownloadProgress);
        setError(null);
        setDismissed(false);
        break;

      case 'update_ready':
        setIsReady(true);
        setDownloadProgress(null);
        setActiveRecordings(data.active_recordings || 0);
        setError(null);
        setDismissed(false);
        break;

      case 'update_error':
        setError(data.error);
        setDownloadProgress(null);
        setApplying(false);
        setRestarting(false);
        break;
    }
  }, []);

  useEffect(() => {
    const subIds = [
      subscribeSSE('*', 'update_available', handleSSEMessage),
      subscribeSSE('*', 'update_downloading', handleSSEMessage),
      subscribeSSE('*', 'update_ready', handleSSEMessage),
      subscribeSSE('*', 'update_error', handleSSEMessage),
    ];

    api.getUpdateStatus().then((response: any) => {
      const status = response as UpdateStatus;
      if (!status || !status.state) return;

      if (status.available_info) {
        setUpdateInfo(status.available_info as UpdateInfo);
      }
      if (status.state === 'ready') {
        setIsReady(true);
      }
      if (status.state === 'downloading' && status.progress) {
        setDownloadProgress(status.progress as DownloadProgress);
      }
      if (status.error) {
        setError(status.error);
      }
      if (status.active_recordings_count) {
        setActiveRecordings(status.active_recordings_count);
      }
    }).catch((err: any) => {
      if (isAuthError(err)) {
        redirectToLogin();
      }
    });

    return () => {
      subIds.forEach(id => unsubscribeSSE(id));
      clearRestartPoll();
    };
  }, [clearRestartPoll, handleSSEMessage, redirectToLogin]);

  const handleDownload = async () => {
    try {
      setError(null);
      await api.downloadProgramUpdate();
      message.success({ content: '已开始下载更新', key: UPDATE_MESSAGE_KEY, duration: 2 });
    } catch (err: any) {
      if (isAuthError(err)) {
        redirectToLogin();
        return;
      }
      setError(err?.message || '下载失败');
      message.error({ content: `下载失败: ${err?.message || '未知错误'}`, key: UPDATE_MESSAGE_KEY, duration: 4 });
    }
  };

  const handleShowDetails = () => {
    setShowModal(true);
  };

  const handleApplyUpdate = async (graceful: boolean) => {
    setApplying(true);
    setError(null);
    message.loading({ content: '正在提交更新请求...', key: UPDATE_MESSAGE_KEY, duration: 0 });

    try {
      const result: any = await api.applyUpdate({ gracefulWait: graceful, forceNow: !graceful });
      setShowModal(false);

      if (result?.status === 'waiting') {
        setApplying(false);
        setRestarting(false);
        message.success({
          content: `已启用优雅更新，将在 ${result.active_recordings || activeRecordings || 0} 个录制结束后自动更新`,
          key: UPDATE_MESSAGE_KEY,
          duration: 4,
        });
        return;
      }

      waitForServerRestart();
    } catch (err: any) {
      if (isAuthError(err)) {
        redirectToLogin();
        return;
      }
      setApplying(false);
      setRestarting(false);
      setError(err?.message || '应用更新失败');
      message.error({ content: `应用更新失败: ${err?.message || '未知错误'}`, key: UPDATE_MESSAGE_KEY, duration: 5 });
    }
  };

  if ((!updateInfo && !restarting) || dismissed) {
    return null;
  }

  const formatSize = (bytes: number) => {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  };

  const formatSpeed = (bytesPerSecond: number) => {
    if (bytesPerSecond < 1024) return `${bytesPerSecond.toFixed(0)} B/s`;
    if (bytesPerSecond < 1024 * 1024) return `${(bytesPerSecond / 1024).toFixed(1)} KB/s`;
    return `${(bytesPerSecond / 1024 / 1024).toFixed(1)} MB/s`;
  };

  const renderMessage = () => {
    if (applying || restarting) {
      return (
        <Space>
          <SyncOutlined spin />
          <Text>{restarting ? `正在等待服务重启... ${restartSeconds}s` : '正在应用更新...'}</Text>
          <Text type="secondary">完成后将自动刷新；如需重新登录会自动跳转。</Text>
        </Space>
      );
    }

    if (error) {
      return (
        <Space>
          <CloseCircleOutlined />
          <Text>更新出错: {error}</Text>
          <Button size="small" type="link" onClick={handleDownload}>重试</Button>
        </Space>
      );
    }

    if (downloadProgress && !isReady) {
      return (
        <Space style={{ width: '100%' }} direction="vertical" size={4}>
          <Space>
            <SyncOutlined spin />
            <Text>正在下载更新 v{updateInfo?.version}...</Text>
            <Text type="secondary">{formatSpeed(downloadProgress.speed)}</Text>
          </Space>
          <Progress
            percent={Math.round(downloadProgress.percentage)}
            size="small"
            status="active"
            format={() => `${formatSize(downloadProgress.downloaded_bytes)} / ${formatSize(downloadProgress.total_bytes)}`}
          />
        </Space>
      );
    }

    if (isReady) {
      return (
        <Space>
          <CheckCircleOutlined style={{ color: '#52c41a' }} />
          <Text>新版本 {updateInfo?.version} 已下载完成，准备更新</Text>
          <Button size="small" type="primary" onClick={handleShowDetails}>
            立即更新
          </Button>
          {activeRecordings > 0 && (
            <Tag color="orange">
              <ExclamationCircleOutlined /> {activeRecordings} 个录制中
            </Tag>
          )}
        </Space>
      );
    }

    return (
      <Space>
        <CloudDownloadOutlined />
        <Text>发现新版本 v{updateInfo?.version}</Text>
        {updateInfo?.prerelease && <Tag color="gold">预发布</Tag>}
        <Button size="small" type="link" onClick={handleShowDetails}>
          查看详情
        </Button>
        <Button size="small" type="primary" icon={<DownloadOutlined />} onClick={handleDownload}>
          下载更新
        </Button>
      </Space>
    );
  };

  return (
    <>
      <Alert
        className="update-banner"
        message={renderMessage()}
        type={error ? 'error' : isReady || restarting ? 'success' : 'info'}
        closable={!applying && !restarting}
        onClose={() => setDismissed(true)}
        banner
      />

      {updateInfo && (
        <Modal
          title={
            <Space>
              <ReloadOutlined />
              更新到 v{updateInfo.version}
            </Space>
          }
          open={showModal}
          onCancel={() => setShowModal(false)}
          footer={null}
          width={520}
        >
          <div className="update-modal-content">
            <Space direction="vertical" size="middle" style={{ width: '100%' }}>
              {updateInfo.prerelease && (
                <Alert
                  message="这是一个预发布版本，可能包含不稳定的功能"
                  type="warning"
                  showIcon
                />
              )}

              {updateInfo.release_date && (
                <Text type="secondary">发布日期: {updateInfo.release_date}</Text>
              )}

              {updateInfo.changelog && (
                <>
                  <Divider plain>更新日志</Divider>
                  <Paragraph className="changelog-content">
                    {updateInfo.changelog}
                  </Paragraph>
                </>
              )}

              {updateInfo.asset_size && (
                <Text type="secondary">
                  文件大小: {formatSize(updateInfo.asset_size)}
                </Text>
              )}

              <Divider />

              {activeRecordings > 0 && (
                <Alert
                  message={
                    <span>
                      当前有 <strong>{activeRecordings}</strong> 个直播正在录制中
                    </span>
                  }
                  description="选择“优雅更新”会等待所有录制完成后自动更新；选择“强制更新”会立即中断录制并更新。"
                  type="warning"
                  showIcon
                />
              )}

              <Space style={{ width: '100%', justifyContent: 'flex-end' }}>
                <Button onClick={() => setShowModal(false)}>
                  稍后再说
                </Button>
                {activeRecordings > 0 ? (
                  <>
                    <Button
                      type="primary"
                      onClick={() => handleApplyUpdate(true)}
                      loading={applying}
                    >
                      优雅更新
                    </Button>
                    <Button
                      danger
                      onClick={() => handleApplyUpdate(false)}
                      loading={applying}
                    >
                      强制更新
                    </Button>
                  </>
                ) : (
                  <Button
                    type="primary"
                    onClick={() => handleApplyUpdate(true)}
                    loading={applying}
                    icon={<ReloadOutlined />}
                  >
                    {isReady ? '立即更新' : '下载并更新'}
                  </Button>
                )}
              </Space>
            </Space>
          </div>
        </Modal>
      )}
    </>
  );
};

export default UpdateBanner;
