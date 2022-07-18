package canal

import "regexp"

func FilterOther(e []byte) bool {
	matched, _ := regexp.MatchString(DropTempTab, string(e))
	if matched {
		return matched
	}

	matched, _ = regexp.MatchString(DropGlobalTempTab, string(e))
	if matched {
		return matched
	}
	return false
}
