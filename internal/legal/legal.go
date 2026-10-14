package legal

import (
	"embed"
	"strings"
)

//go:embed notices
var notices embed.FS

// Text provides licence notices in the executable, including offline runs.
func Text() string {
	entries, _ := notices.ReadDir("notices")
	var b strings.Builder
	for _, entry := range entries {
		data, _ := notices.ReadFile("notices/" + entry.Name())
		b.WriteString(entry.Name())
		b.WriteString("\n\n")
		b.Write(data)
		b.WriteString("\n\n")
	}
	return b.String()
}
