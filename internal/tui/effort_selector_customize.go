package tui

// effortSelectorStyle selects the presentation of the /effort picker.
type effortSelectorStyle string

const (
	effortSelectorCustom effortSelectorStyle = "custom"
	effortSelectorList   effortSelectorStyle = "list"
)

func normalizeEffortSelectorStyle(style string) effortSelectorStyle {
	switch effortSelectorStyle(style) {
	case effortSelectorList:
		return effortSelectorList
	default:
		return effortSelectorCustom
	}
}
