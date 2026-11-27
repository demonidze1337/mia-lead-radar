import { describe, expect, it } from 'vitest';
import { LANGUAGES, translations } from './index';

describe('i18n dictionaries', () => {
  it('covers all 14 languages', () => {
    expect(LANGUAGES).toHaveLength(14);
    for (const language of LANGUAGES) {
      expect(translations[language.code]).toBeTruthy();
      expect(translations[language.code].dashboard).toBeTruthy();
      expect(translations[language.code].leads).toBeTruthy();
    }
  });

  it('has RTL Arabic labels', () => {
    expect(translations.ar.dashboard).toBe('لوحة التحكم');
  });
});
