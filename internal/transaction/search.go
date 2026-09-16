package transaction

import "strings"

const likeEscape = `\`

//nolint:gochecknoglobals // immutable replacer, not runtime state.
var likeEscaper = strings.NewReplacer(
	likeEscape, likeEscape+likeEscape,
	"%", likeEscape+"%",
	"_", likeEscape+"_",
)

// NOTE: without this a user searching "50%" gets every row, because % is the LIKE wildcard.
func escapeLikePattern(s string) string { return "%" + likeEscaper.Replace(s) + "%" }
