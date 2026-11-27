import WebApp from '@twa-dev/sdk';

let audioContext: AudioContext | null = null;

function context(): AudioContext | null {
  if (typeof window === 'undefined') return null;
  const Ctor = window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
  if (!Ctor) return null;
  if (!audioContext) audioContext = new Ctor();
  return audioContext;
}

/** Gentle WebAudio chime for high-signal leads; safe under autoplay policies. */
export function playLeadChime(enabled: boolean): void {
  if (!enabled) return;
  try {
    const ctx = context();
    if (!ctx) return;
    if (ctx.state === 'suspended') void ctx.resume().catch(() => undefined);
    const now = ctx.currentTime;
    const gain = ctx.createGain();
    gain.gain.setValueAtTime(0.0001, now);
    gain.gain.exponentialRampToValueAtTime(0.06, now + 0.03);
    gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.55);
    gain.connect(ctx.destination);
    [660, 880].forEach((frequency, index) => {
      const oscillator = ctx.createOscillator();
      oscillator.type = 'sine';
      oscillator.frequency.value = frequency;
      oscillator.connect(gain);
      oscillator.start(now + index * 0.08);
      oscillator.stop(now + 0.6);
    });
  } catch {
    // Audio is a progressive enhancement; never break the UI.
  }
}

/** Optional Telegram haptic pulse, ignored outside the Mini App. */
export function triggerHaptic(enabled: boolean, type: 'light' | 'medium' | 'success' = 'light'): void {
  if (!enabled) return;
  try {
    if (WebApp.HapticFeedback) {
      if (type === 'success') WebApp.HapticFeedback.notificationOccurred('success');
      else WebApp.HapticFeedback.impactOccurred(type);
    }
  } catch {
    // Haptics unsupported in browser.
  }
}
