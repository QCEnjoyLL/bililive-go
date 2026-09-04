import { Modal, Input, Switch } from 'antd';
import React from 'react';
import API from '../../utils/api';
import { BOYFRIEND_LIVE_BASE_URL, normalizeRoomInput } from './normalize-room-input';

const api = new API();

interface Props {
    refresh?: any;
    children?: React.ReactNode;
}

class AddRoomDialog extends React.Component<Props> {
    state = {
        ModalText: '请输入房间号或完整直播间 URL',
        visible: false,
        confirmLoading: false,
        textView: '',
        autoCompleteBoyfriend: true,
    };

    showModal = () => {
        this.setState({
            ModalText: '请输入房间号或完整直播间 URL',
            visible: true,
            confirmLoading: false,
            autoCompleteBoyfriend: true,
        });
    };

    handleOk = () => {
        if (this.state.confirmLoading) {
            return;
        }
        const roomURL = normalizeRoomInput(this.state.textView, this.state.autoCompleteBoyfriend);
        if (roomURL === '') {
            alert('请输入房间号或完整直播间 URL');
            return;
        }

        this.setState({
            ModalText: '正在添加直播间......',
            confirmLoading: true,
        });

        api.addNewRoom(roomURL)
            .then((rsp) => {
                // 保存设置
                api.saveSettingsInBackground();
                this.setState({
                    visible: false,
                    confirmLoading: false,
                    textView: ''
                });
                this.props.refresh?.();
            })
            .catch(err => {
                alert(`添加直播间失败:\n${err}`);
                this.setState({
                    visible: false,
                    confirmLoading: false,
                    textView: ''
                });
            })
    };

    handleCancel = () => {
        this.setState({
            visible: false,
            textView: ''
        });
    };

    textChange = (e: any) => {
        this.setState({
            textView: e.target.value
        })
    }

    autoCompleteBoyfriendChange = (checked: boolean) => {
        this.setState({
            autoCompleteBoyfriend: checked,
        });
    }

    render() {
        const { visible, confirmLoading, ModalText, textView, autoCompleteBoyfriend } = this.state;
        return (
            <div>
                <Modal
                    title="添加直播间"
                    open={visible}
                    onOk={this.handleOk}
                    confirmLoading={confirmLoading}
                    onCancel={this.handleCancel}>
                    <p>{ModalText}</p>
                    <Input
                        size="large"
                        value={textView}
                        placeholder="tommyjoyer 或 https://live.bilibili.com/6"
                        onChange={this.textChange}
                        onPressEnter={this.handleOk}
                    />
                    <div style={{ marginTop: 16, display: 'flex', alignItems: 'flex-start', gap: 10 }}>
                        <Switch
                            checked={autoCompleteBoyfriend}
                            onChange={this.autoCompleteBoyfriendChange}
                            aria-label="自动补全 BoyFriend 直播间链接"
                        />
                        <div>
                            <div>自动补全 BoyFriend 直播间链接</div>
                            <div style={{ marginTop: 4, color: 'var(--bgo-muted, #8c8c8c)', fontSize: 12 }}>
                                输入房间号时自动添加 {BOYFRIEND_LIVE_BASE_URL}/ 前缀；完整链接不受影响。
                            </div>
                        </div>
                    </div>
                </Modal>
            </div>
        );
    }
}

export default AddRoomDialog;
