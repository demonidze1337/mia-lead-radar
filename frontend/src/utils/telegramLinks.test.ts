import { describe, expect, it } from 'vitest';
import { buildMessageLink, buildProfileLink, cleanUsername, normalizeChannelPathID } from './telegramLinks';

describe('telegramLinks', () => {
  it('builds deterministic message links', () => {
    expect(buildMessageLink({ chat_username: '@DevChat', msg_id: 12 })).toBe('https://t.me/DevChat/12');
    expect(buildMessageLink({ tg_chat_id: -1001234567890, msg_id: 7 })).toBe('https://t.me/c/1234567890/7');
    expect(buildMessageLink({ msg_link: 'https://t.me/chat/5' })).toBe('https://t.me/chat/5');
    expect(buildMessageLink({ msg_id: 0 })).toBeNull();
  });

  it('builds profile links with id fallback', () => {
    expect(buildProfileLink({ author_username: '@buyer' })).toBe('https://t.me/buyer');
    expect(buildProfileLink({ author_id: 42 })).toBe('https://t.me/@id42');
    expect(buildProfileLink({})).toBeNull();
  });

  it('normalizes handles and channel ids', () => {
    expect(cleanUsername('  @@Name ')).toBe('Name');
    expect(normalizeChannelPathID(-100123)).toBe('123');
    expect(normalizeChannelPathID(123)).toBe('123');
  });
});
