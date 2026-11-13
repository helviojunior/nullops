package ascii

import (
	"fmt"
	"strings"

	"github.com/helviojunior/nullops/internal/version"
)

// The icon: the terminal prompt of the NullOps logo, a red stepped ">" with a
// white "_" cursor. Each line is split in segments so the colors never shift
// the alignment.
var iconLines = [][2]string{ // {red chevron, white cursor}
	{`  __`, ``},
	{`  \ \__`, ``},
	{`   \_  \__`, ``},
	{`     \__  \__`, ``},
	{`        \__  \__`, ``},
	{`           >    >`, ``},
	{`        __/  __/`, ``},
	{`     __/  __/`, ``},
	{`   _/  __/`, ``},
	{`  / __/`, `    _____________`},
	{` /_/`, `      (_____________)`},
}

// "NullOps" in the slant figlet font: "Null" white, "Ops" red (as in the logo).
var nameLines = [][2]string{ // {white, red}
	{`                ____`, ``},
	{`   ____  __  __/ / /`, `___  ____  _____`},
	{`  / __ \/ / / / / /`, ` __ \/ __ \/ ___/`},
	{` / / / / /_/ / / /`, ` /_/ / /_/ (__  ) `},
	{`/_/ /_/\__,_/_/_/`, `\____/ .___/____/  `},
	{`                 `, `    /_/`},
}

const (
	red   = "\033[91m"
	white = "\033[97m"
	gray  = "\033[90m"
	reset = "\033[0m"

	iconWidth = 28 // visible columns reserved for the icon
	nameTop   = 2  // icon line where the name starts
)

// Logo returns the nullops ascii logo
func Logo() string {
	var b strings.Builder
	b.WriteString("\n")
	for i, l := range iconLines {
		pad := iconWidth - len(l[0]) - len(l[1])
		if pad < 1 {
			pad = 1
		}
		b.WriteString(red + l[0] + white + l[1] + reset + strings.Repeat(" ", pad))
		switch j := i - nameTop; {
		case j >= 0 && j < len(nameLines):
			b.WriteString(white + nameLines[j][0] + red + nameLines[j][1] + reset)
		case j == len(nameLines)+1:
			b.WriteString(gray + "Web Resource Manager" + reset)
		case j == len(nameLines)+2:
			b.WriteString(gray + fmt.Sprintf("Ver: %s-%s", version.Version, version.GitHash) + reset)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// LogoHelp returns the logo, with help
func LogoHelp(s string) string {
	return Logo() + "\n\n" + s
}
