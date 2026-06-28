import React from 'react';
import { Card, Form, Switch, Select, Input, Tag, Alert, Space, InputNumber, Divider, Button } from 'antd';
import { CloudUploadOutlined } from '@ant-design/icons';
import API from '../../utils/api';

const { TextArea } = Input;
const api = new API();

interface ConfigFieldProps {
  label: string;
  description?: string;
  children: React.ReactElement;
}

// 简化版 ConfigField 组件
const ConfigField: React.FC<ConfigFieldProps> = ({ label, description, children }) => (
  <div className="config-item" style={{ marginBottom: 16 }}>
    <div className="config-item-label" style={{ marginBottom: 4, fontWeight: 500 }}>{label}</div>
    <div className="config-item-content">
      <div className="config-item-input">{children}</div>
      {description && (
        <div className="config-item-description" style={{ marginTop: 4, color: '#888', fontSize: 12 }}>
          {description}
        </div>
      )}
    </div>
  </div>
);

interface CloudUploadSettingsProps {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  config: any;
}

/**
 * 云盘上传设置组件
 * 用于 GlobalSettings 中显示云上传配置
 */
const CloudUploadSettings: React.FC<CloudUploadSettingsProps> = ({ config }) => {
  const form = Form.useFormInstance();
  const isEnabled = config.on_record_finished?.cloud_upload?.enable;
  const watchedExternalURL = Form.useWatch(['openlist', 'external_url']);
  const externalURL = String(watchedExternalURL ?? config.openlist?.external_url ?? '').trim();
  const usesExternalOpenList = externalURL.length > 0;
  const [validationLoading, setValidationLoading] = React.useState(false);
  const [validationResult, setValidationResult] = React.useState<any>(null);

  const handleValidateOpenList = async () => {
    const values = form.getFieldsValue(true);
    const payload = {
      external_url: values.openlist?.external_url || '',
      external_token: values.openlist?.external_token || '',
      storage_name: values.on_record_finished?.cloud_upload?.storage_name || '',
    };

    setValidationLoading(true);
    setValidationResult(null);
    try {
      const result = await api.validateOpenListConfig(payload);
      setValidationResult(result);
    } catch (error: any) {
      setValidationResult({
        ok: false,
        ready_for_upload: false,
        message: error?.message || '验证请求失败',
        errors: [error?.message || '验证请求失败'],
      });
    } finally {
      setValidationLoading(false);
    }
  };

  const renderValidationResult = () => {
    if (!validationResult) return null;
    const storages = Array.isArray(validationResult.storages) ? validationResult.storages : [];
    const errors = Array.isArray(validationResult.errors) ? validationResult.errors : [];
    const type = validationResult.ready_for_upload ? 'success' : validationResult.ok ? 'warning' : 'error';

    return (
      <Alert
        message={validationResult.message || (validationResult.ok ? 'OpenList 验证完成' : 'OpenList 验证失败')}
        description={
          <div>
            <div>服务：{validationResult.service_ready ? '可访问' : '不可访问'}</div>
            <div>Token：{validationResult.auth_checked ? (validationResult.auth_ok ? '有效' : '无效或权限不足') : '未验证'}</div>
            <div>存储：{validationResult.storage_checked ? (validationResult.storage_ok ? '可访问' : '不可用') : '未验证'}</div>
            {validationResult.endpoint && <div>地址：<code>{validationResult.endpoint}</code></div>}
            {storages.length > 0 && (
              <div>
                已读取存储：{storages.map((storage: any) => storage.mount_path || storage.name).filter(Boolean).join('、')}
              </div>
            )}
            {errors.length > 0 && (
              <ul style={{ margin: '6px 0 0 18px', padding: 0 }}>
                {errors.map((error: string) => <li key={error}>{error}</li>)}
              </ul>
            )}
          </div>
        }
        type={type}
        showIcon
        style={{ marginBottom: 16 }}
      />
    );
  };

  return (
    <Card
      title={<><CloudUploadOutlined /> 云盘上传</>}
      size="small"
      style={{ marginBottom: 16 }}
      extra={
        <Space size={6} wrap>
          <Tag color={usesExternalOpenList ? 'blue' : 'purple'}>
            {usesExternalOpenList ? '外部 OpenList' : '内置 OpenList'}
          </Tag>
          <Tag color={isEnabled ? 'green' : 'default'}>
            {isEnabled ? '已启用' : '未启用'}
          </Tag>
        </Space>
      }
    >
      <Alert
        message="云盘自动上传功能"
        description={
          <>
            录制完成后自动上传到网盘。需要先在{' '}
            <a href="/remotetools/tool/openlist/" target="_blank" rel="noopener noreferrer">
              OpenList 管理页面
            </a>{' '}
            配置网盘存储。
          </>
        }
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
      />
      <Alert
        message={
          usesExternalOpenList
            ? '当前使用外部 OpenList'
            : '当前使用内置 OpenList'
        }
        description={
          usesExternalOpenList
            ? (
              <>
                已填写外部 OpenList 地址，上传和存储检查会连接到 <code>{externalURL}</code>；清空外部地址后会改用内置 OpenList。
              </>
            )
            : '未填写外部 OpenList 地址，程序会通过 RemoteTools 准备并使用内置 OpenList。'
        }
        type={usesExternalOpenList ? 'success' : 'info'}
        showIcon
        style={{ marginBottom: 16 }}
      />
      <ConfigField
        label="外部 OpenList 地址"
        description="如果你已经用 Docker 或其他方式运行了 OpenList，可以填写服务地址，例如：http://127.0.0.1:5244；留空则由本程序通过 RemoteTools 准备内置 OpenList。"
      >
        <Form.Item name={['openlist', 'external_url']} noStyle>
          <Input placeholder="例如: http://127.0.0.1:5244" style={{ width: 360 }} />
        </Form.Item>
      </ConfigField>
      <ConfigField
        label="外部 OpenList Token"
        description="外部 OpenList 的 API Token，用于读取存储列表、健康检查和后续上传；只打开管理页面时可以留空。"
      >
        <Form.Item name={['openlist', 'external_token']} noStyle>
          <Input.Password placeholder="OpenList API Token" style={{ width: 360 }} />
        </Form.Item>
      </ConfigField>
      <ConfigField
        label="连接验证"
        description="使用当前表单里的地址、Token 和存储位置进行验证；无需先保存配置。"
      >
        <Button onClick={handleValidateOpenList} loading={validationLoading}>
          检测 OpenList
        </Button>
      </ConfigField>
      {renderValidationResult()}
      <Divider style={{ margin: '12px 0', fontSize: 12 }}>内置 OpenList 设置</Divider>
      <ConfigField
        label="内置 OpenList 端口"
        description="仅在外部 OpenList 地址留空时生效；修改后通常需要重启程序。"
      >
        <Form.Item name={['openlist', 'port']} noStyle>
          <InputNumber min={1} max={65535} placeholder="5244" style={{ width: 160 }} />
        </Form.Item>
      </ConfigField>
      <ConfigField
        label="内置 OpenList 数据目录"
        description="仅内置 OpenList 生效；留空使用默认目录。"
      >
        <Form.Item name={['openlist', 'data_path']} noStyle>
          <Input placeholder="留空使用默认目录" style={{ width: 400 }} />
        </Form.Item>
      </ConfigField>
      <Divider style={{ margin: '12px 0', fontSize: 12 }}>上传设置</Divider>
      <ConfigField
        label="启用云上传"
        description="开启后录制完成的视频会自动上传到配置的网盘"
      >
        <Form.Item name={['on_record_finished', 'cloud_upload', 'enable']} valuePropName="checked" noStyle>
          <Switch />
        </Form.Item>
      </ConfigField>
      <ConfigField
        label="上传时机"
        description="选择何时开始上传：立即上传原始文件，或等待后处理（修复/转码）完成后上传"
      >
        <Form.Item name={['on_record_finished', 'upload_timing']} noStyle>
          <Select style={{ width: 250 }} placeholder="选择上传时机">
            <Select.Option value="">使用默认（立即）</Select.Option>
            <Select.Option value="immediate">立即上传原始文件</Select.Option>
            <Select.Option value="after_process">后处理完成后上传</Select.Option>
          </Select>
        </Form.Item>
      </ConfigField>
      <ConfigField
        label="存储位置"
        description="在 OpenList 中配置的存储位置或挂载名，例如：115、阿里云盘"
      >
        <Form.Item name={['on_record_finished', 'cloud_upload', 'storage_name']} noStyle>
          <Input placeholder="例如: 115" style={{ width: 200 }} />
        </Form.Item>
      </ConfigField>
      <ConfigField
        label="额外存储位置"
        description="可选，多目标上传时使用；输入存储位置后按回车添加。"
      >
        <Form.Item name={['on_record_finished', 'cloud_upload', 'additional_storages']} noStyle>
          <Select mode="tags" placeholder="输入存储位置后按回车添加" style={{ width: 360 }} />
        </Form.Item>
      </ConfigField>
      <ConfigField
        label="上传路径模板"
        description="支持变量: {{ .Platform }}, {{ .HostName }}, {{ .RoomName }}, {{ .FileName }}, {{ .Ext }}"
      >
        <Form.Item name={['on_record_finished', 'cloud_upload', 'upload_path_tmpl']} noStyle>
          <TextArea
            rows={2}
            placeholder="/录播归档/{{ .Platform }}/{{ .HostName }}/{{ .RoomName }}/{{ .FileName }}"
            style={{ width: 500 }}
          />
        </Form.Item>
      </ConfigField>
      <ConfigField
        label="上传后删除本地文件"
        description="上传成功后自动删除本地文件以节省空间"
      >
        <Form.Item name={['on_record_finished', 'cloud_upload', 'delete_after_upload']} valuePropName="checked" noStyle>
          <Switch />
        </Form.Item>
      </ConfigField>
    </Card>
  );
};

export default CloudUploadSettings;
