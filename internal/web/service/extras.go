// Package service holds the business logic behind the panel HTTP handlers.
package service

// SettingStoreAdapter adapts SettingService to the minimal key/value surface
// that the extra-core manager needs, hiding the private settings internals.
type SettingStoreAdapter struct {
	settingService SettingService
}

// NewSettingStoreAdapter returns an adapter around the given settings service.
func NewSettingStoreAdapter(s SettingService) *SettingStoreAdapter {
	return &SettingStoreAdapter{settingService: s}
}

// GetString implements extra.SettingsStore.
func (a *SettingStoreAdapter) GetString(key string) (string, error) {
	return a.settingService.GetRawSetting(key)
}

// SetString implements extra.SettingsStore.
func (a *SettingStoreAdapter) SetString(key, value string) error {
	return a.settingService.SetRawSetting(key, value)
}
