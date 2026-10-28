package platform

import "golang.org/x/sys/windows"

func uiLanguages() []string {
	langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil {
		return envLanguages()
	}
	return langs
}
