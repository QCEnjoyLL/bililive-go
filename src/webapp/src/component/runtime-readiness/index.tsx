import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Alert, Space, Spin, Tag, Typography } from 'antd';
import {
  CheckCircleOutlined,
  ExclamationCircleOutlined,
  LoadingOutlined
} from '@ant-design/icons';
import API from '../../utils/api';
import './runtime-readiness.css';

const api = new API();
const { Text } = Typography;

type ComponentState = 'ready' | 'preparing' | 'error';
type RuntimeState = 'ready' | 'partial' | 'preparing' | 'error';

interface RuntimeComponentStatus {
  key: string;
  label: string;
  required: boolean;
  status: ComponentState;
  message?: string;
  path?: string;
}

interface RuntimeReadiness {
  ready: boolean;
  all_ready: boolean;
  status: RuntimeState;
  message: string;
  components: RuntimeComponentStatus[];
}

const statusMeta = {
  ready: {
    alertType: 'success' as const,
    icon: <CheckCircleOutlined />,
    title: '录制环境已就绪'
  },
  partial: {
    alertType: 'info' as const,
    icon: <Spin indicator={<LoadingOutlined spin />} size="small" />,
    title: '录制环境可用，辅助组件准备中'
  },
  preparing: {
    alertType: 'warning' as const,
    icon: <Spin indicator={<LoadingOutlined spin />} size="small" />,
    title: '录制环境准备中'
  },
  error: {
    alertType: 'error' as const,
    icon: <ExclamationCircleOutlined />,
    title: '录制环境需要处理'
  }
};

const RuntimeReadinessBanner: React.FC = () => {
  const [readiness, setReadiness] = useState<RuntimeReadiness | null>(null);
  const [visible, setVisible] = useState(false);
  const [seenNonReady, setSeenNonReady] = useState(false);
  const [dismissedStatus, setDismissedStatus] = useState<RuntimeState | null>(null);
  const hideTimer = useRef<number | null>(null);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const data = await api.getRuntimeReadiness() as RuntimeReadiness;
        if (cancelled) return;
        setReadiness(data);

        if (data.status === 'ready') {
          setDismissedStatus(null);
          if (seenNonReady) {
            setVisible(true);
            if (hideTimer.current) {
              window.clearTimeout(hideTimer.current);
            }
            hideTimer.current = window.setTimeout(() => setVisible(false), 7000);
          } else {
            setVisible(false);
          }
          return;
        }

        setSeenNonReady(true);
        setVisible(dismissedStatus !== data.status);
      } catch (err) {
        console.error('获取录制环境状态失败:', err);
      }
    };

    load();
    const timer = window.setInterval(load, 5000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
      if (hideTimer.current) {
        window.clearTimeout(hideTimer.current);
      }
    };
  }, [dismissedStatus, seenNonReady]);

  const details = useMemo(() => {
    if (!readiness) return null;
    return (
      <Space size={[8, 8]} wrap>
        {readiness.components.map(component => {
          const color = component.status === 'ready'
            ? 'success'
            : component.status === 'error'
              ? 'error'
              : component.required
                ? 'warning'
                : 'processing';
          const suffix = component.status === 'ready'
            ? '已就绪'
            : component.status === 'error'
              ? '异常'
              : '准备中';
          return (
            <Tag color={color} key={component.key}>
              {component.label}：{suffix}
            </Tag>
          );
        })}
      </Space>
    );
  }, [readiness]);

  if (!readiness || !visible) {
    return null;
  }

  const meta = statusMeta[readiness.status] || statusMeta.preparing;
  return (
    <div className="runtime-readiness">
      <Alert
        type={meta.alertType}
        showIcon
        icon={meta.icon}
        closable={readiness.status !== 'preparing'}
        onClose={() => {
          setDismissedStatus(readiness.status);
          setVisible(false);
        }}
        message={meta.title}
        description={
          <div className="runtime-readiness-description">
            <Text>{readiness.message}</Text>
            {details}
          </div>
        }
      />
    </div>
  );
};

export default RuntimeReadinessBanner;
