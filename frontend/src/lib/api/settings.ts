import { api } from './client';
import type { PlatformSetting, PlatformSettingMutation } from './types';

// Whitelisted setting keys mirrored from the backend. Frontend uses these
// to render labels and pick the right input control per key.
export const SETTING_ACME_EMAIL = 'acme.email';
export const SETTING_ACME_STAGING = 'acme.staging';
export const SETTING_HTTPS_REDIRECT = 'https.redirect_enabled';
export const SETTING_SMTP_HOST = 'smtp.host';
export const SETTING_SMTP_PORT = 'smtp.port';
export const SETTING_SMTP_USER = 'smtp.user';
export const SETTING_SMTP_PASS = 'smtp.pass';
export const SETTING_SMTP_FROM = 'smtp.from';
export const SETTING_SMTP_STARTTLS = 'smtp.starttls';

export const settingsApi = {
	list: () => api.get<PlatformSetting[]>('/settings'),
	upsert: (key: string, value: string) =>
		api.put<PlatformSettingMutation>(`/settings/${key}`, { value }),
	remove: (key: string) =>
		api.delete<PlatformSettingMutation>(`/settings/${key}`)
};
