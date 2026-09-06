// Package chatwrightplugin embeds Chatwright's publishable Agent Skills bundle.
//
// The command package consumes this immutable snapshot so an installed
// `chatwright` binary can synchronize its matching skills without a source
// checkout.
package chatwrightplugin

import "embed"

// SkillsFS contains the complete publishable skills tree.
//
//go:embed all:skills
var SkillsFS embed.FS
