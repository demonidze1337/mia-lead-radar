import WebApp from '@twa-dev/sdk';

export interface LeadLinkTarget {
  tg_chat_id?: number;
  chat_username?: string;
  msg_id?: number;
  msg_link?: string;
  author_id?: number;
  author_username?: string;
}

/**
 * Normalizes a Telegram username (strips leading @ and whitespace).
 */
export function cleanUsername(username?: string): string {
  if (!username) return '';
  return username.trim().replace(/^@+/, '');
}

/**
 * Strips the -100 supergroup/channel prefix used by Bot API / TDLib IDs when
 * constructing https://t.me/c/<internal_id>/<msg_id> deep links.
 */
export function normalizeChannelPathID(tgChatID?: number): string {
  if (!tgChatID || tgChatID === 0) return '';
  const abs = Math.abs(Math.trunc(tgChatID)).toString();
  if (tgChatID < 0 && abs.startsWith('100') && abs.length > 3) {
    return abs.slice(3);
  }
  return abs;
}

/**
 * Returns a deterministic direct link to the message inside the Telegram chat.
 * Opening the chat message loads the author in Telegram Desktop and Web clients
 * even when the author has no public @username.
 */
export function buildMessageLink(target: LeadLinkTarget): string | null {
  if (target.msg_link && target.msg_link.trim() !== '') {
    return target.msg_link.trim();
  }
  const msgID = target.msg_id ?? 0;
  if (msgID <= 0) return null;

  const chatUser = cleanUsername(target.chat_username);
  if (chatUser) {
    return `https://t.me/${chatUser}/${msgID}`;
  }
  const channelID = normalizeChannelPathID(target.tg_chat_id);
  if (channelID) {
    return `https://t.me/c/${channelID}/${msgID}`;
  }
  return null;
}

/**
 * Returns a deterministic profile link:
 * 1) https://t.me/<username> when username exists
 * 2) https://t.me/@id<author_id> fallback when no username exists
 */
export function buildProfileLink(target: LeadLinkTarget): string | null {
  const user = cleanUsername(target.author_username);
  if (user) {
    return `https://t.me/${user}`;
  }
  if (target.author_id && target.author_id > 0) {
    return `https://t.me/@id${Math.trunc(target.author_id)}`;
  }
  return null;
}

/**
 * Returns true if running inside a real Telegram WebApp container with initData.
 */
export function isTelegramMiniApp(): boolean {
  try {
    return Boolean(typeof window !== 'undefined' && WebApp && WebApp.initData);
  } catch {
    return false;
  }
}

/**
 * Opens a Telegram URL deterministically across Desktop, Web, Mobile, and Mini App
 * without setTimeout races or mutating window.location.href.
 */
export function openTelegramTarget(url: string | null | undefined): boolean {
  if (!url || !url.trim()) return false;
  const cleanUrl = url.trim();

  if (isTelegramMiniApp() && (cleanUrl.startsWith('https://t.me/') || cleanUrl.startsWith('tg://'))) {
    try {
      if (typeof WebApp.openTelegramLink === 'function') {
        WebApp.openTelegramLink(cleanUrl);
        return true;
      }
    } catch {
      // Fall through cleanly to window.open below.
    }
  }

  try {
    const win = window.open(cleanUrl, '_blank', 'noopener,noreferrer');
    if (win) return true;
  } catch {
    // Ignore popup blocker exceptions and return false so UI can offer copy fallback.
  }
  return false;
}

/**
 * Copies handle (@username) or numeric user ID to clipboard.
 */
export async function copyAuthorIdentifier(target: LeadLinkTarget): Promise<string | null> {
  const user = cleanUsername(target.author_username);
  const value = user ? `@${user}` : target.author_id && target.author_id > 0 ? String(target.author_id) : '';
  if (!value) return null;

  try {
    if (navigator?.clipboard?.writeText) {
      await navigator.clipboard.writeText(value);
      return value;
    }
  } catch {
    // Fall through to legacy textarea fallback.
  }

  try {
    const textarea = document.createElement('textarea');
    textarea.value = value;
    textarea.setAttribute('readonly', '');
    textarea.style.position = 'fixed';
    textarea.style.opacity = '0';
    document.body.appendChild(textarea);
    textarea.select();
    document.execCommand('copy');
    document.body.removeChild(textarea);
    return value;
  } catch {
    return null;
  }
}
