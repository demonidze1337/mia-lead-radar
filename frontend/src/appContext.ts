import { createContext, useContext } from 'react';
import type { RuntimeSettings, SetupStatus } from './api/client';

export type ViewKey = 'dashboard' | 'leads' | 'radar' | 'terminal' | 'consultant' | 'chats' | 'analytics' | 'settings';

export interface AppContextValue {
  setup: SetupStatus;
  settings: RuntimeSettings;
  refreshSetup: () => Promise<void>;
  refreshSettings: () => Promise<void>;
  navigate: (view: ViewKey) => void;
  openSetup: () => void;
  logout: () => Promise<void>;
}

export const AppContext = createContext<AppContextValue | null>(null);

export function useApp(): AppContextValue {
  const value = useContext(AppContext);
  if (!value) throw new Error('useApp must be used inside App');
  return value;
}
