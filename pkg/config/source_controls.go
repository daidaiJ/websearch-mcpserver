package config

import "strings"

// APISourceEnabled controls API providers independently of their stored keys.
// Omitted switches preserve existing behavior, including anonymous AnySearch.
func (c Config) APISourceEnabled(name string) bool {
	var enabled *bool
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "baidu":
		enabled = c.Baidu.Enabled
	case "tavily":
		enabled = c.Tavily.Enabled
	case "exa":
		enabled = c.Exa.Enabled
	case "anysearch":
		enabled = c.Anysearch.Enabled
	case "doubao":
		enabled = c.Doubao.Enabled
	case "tinyfish":
		enabled = c.TinyFish.Enabled
	case "keenable":
		enabled = c.Keenable.Enabled
	default:
		return false
	}
	return enabled == nil || *enabled
}
