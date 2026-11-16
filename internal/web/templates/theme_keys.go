package templates

func themeFamilyKey(family string) string {
	//i18n:use theme.*
	return "theme." + family
}

func themeModeKey(mode string) string {
	return "theme." + mode
}
