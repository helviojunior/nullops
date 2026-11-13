package ascii

import (
	"fmt"
	"strings"

	"github.com/helviojunior/fmweb/internal/version"
)

// Logo returns the fmweb ascii logo
func Logo() string {
	txt := `
{G}    ____                {O}        __  
{G}   / __/___ ___ {O}_      _____  / /_ 
{G}  / /_/ __ ` + "`" + `__ \{O} | /| / / _ \/ __ \
{G} / __/ / / / / /{O} |/ |/ /  __/ /_/ /
{G}/_/ /_/ /_/ /_/{O}|__/|__/\___/_.___/ {B}
`
	v := fmt.Sprintf("Web File Manager - Ver: %s-%s", version.Version, version.GitHash)
	r := 38 - len(v)
	if r < 0 {
		r = 0
	}
	txt += strings.Repeat(" ", r)
	txt += v + "{W}"
	txt = strings.ReplaceAll(txt, "{G}", "\033[32m")
	txt = strings.ReplaceAll(txt, "{B}", "\033[36m")
	txt = strings.ReplaceAll(txt, "{O}", "\033[33m")
	txt = strings.ReplaceAll(txt, "{W}", "\033[0m")
	return fmt.Sprintln(txt)
}

// LogoHelp returns the logo, with help
func LogoHelp(s string) string {
	return Logo() + "\n\n" + s
}
