import { BOYFRIEND_LIVE_BASE_URL, normalizeRoomInput } from './normalize-room-input';

describe('normalizeRoomInput', () => {
    test('默认将纯房间号补全为 BoyFriend 直播间链接', () => {
        expect(normalizeRoomInput('  tommyjoyer  ', true))
            .toBe(`${BOYFRIEND_LIVE_BASE_URL}/tommyjoyer`);
    });

    test.each([
        'https://zh.boyfriend.show/whc',
        'https://live.bilibili.com/6',
        'HTTP://example.com/room',
    ])('保持完整链接不变：%s', (url) => {
        expect(normalizeRoomInput(url, true)).toBe(url);
    });

    test('关闭自动补全时保持纯房间号不变', () => {
        expect(normalizeRoomInput('tommyjoyer', false)).toBe('tommyjoyer');
    });

    test('去除房间号前多余的斜杠', () => {
        expect(normalizeRoomInput('/tommyjoyer', true))
            .toBe(`${BOYFRIEND_LIVE_BASE_URL}/tommyjoyer`);
    });

    test('空白输入不会生成无效链接', () => {
        expect(normalizeRoomInput('   ', true)).toBe('');
    });
});
