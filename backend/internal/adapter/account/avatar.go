package account

import "strings"

const filePublicIDPrefix = "file_"

func IsFilePublicID(s string) bool {
	return strings.HasPrefix(s, filePublicIDPrefix)
}
