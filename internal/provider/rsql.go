package provider

import "strings"

// rsqlQuote quotes a value for an RSQL filter so names with spaces or reserved
// characters are accepted. Backslashes are escaped as well: RSQL treats a
// backslash inside a quoted value as an escape, so leaving one unescaped
// changes the meaning of the filter or makes the API reject it.
func rsqlQuote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}
