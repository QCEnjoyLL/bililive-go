export const BOYFRIEND_LIVE_BASE_URL = 'https://zh.boyfriend.show';

const URL_WITH_SCHEME_PATTERN = /^[a-z][a-z\d+.-]*:\/\//i;

export function normalizeRoomInput(value: string, autoCompleteBoyfriend: boolean): string {
    const input = value.trim();
    if (!autoCompleteBoyfriend || input === '' || URL_WITH_SCHEME_PATTERN.test(input)) {
        return input;
    }
    return `${BOYFRIEND_LIVE_BASE_URL}/${input.replace(/^\/+/, '')}`;
}
