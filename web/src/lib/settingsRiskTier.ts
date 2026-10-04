import { PlatformSettings, PlatformSettingsUpdateRequest } from '@/api/types';

const RISK_BEARING_KEYS: (keyof PlatformSettingsUpdateRequest)[] = [
  'broker_api_key',
  'broker_api_secret',
  'broker_testnet_api_key',
  'broker_testnet_api_secret',
  'testnet',
  'mode',
];

export function isRiskBearingChange(
  original: PlatformSettings,
  next: PlatformSettingsUpdateRequest
): boolean {
  return RISK_BEARING_KEYS.some((key) => {
    const nextVal = next[key];
    const origVal = original[key as keyof PlatformSettings];
    if (nextVal === undefined) {
      return false;
    }
    return nextVal !== origVal;
  });
}
