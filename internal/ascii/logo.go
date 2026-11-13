package ascii

import (
	"fmt"
	"strings"

	"github.com/helviojunior/nullops/internal/version"
)

// Logo returns the nullops ascii logo
func Logo() string {
	txt := `
{G}                ____{O}                
{G}   ____  __  __/ / /{O}___  ____  _____
{G}  / __ \/ / / / / /{O} __ \/ __ \/ ___/
{G} / / / / /_/ / / /{O} /_/ / /_/ (__  ) 
{G}/_/ /_/\__,_/_/_/{O}\____/ .___/____/  
{G}                 {O}    /_/  {B}
`
	v := fmt.Sprintf("NullOps - Web Resource Manager | Ver: %s-%s", version.Version, version.GitHash)
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
