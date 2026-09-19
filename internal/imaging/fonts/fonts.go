// Package fonts provides embedded font data for emoji rendering.
package fonts

import (
	_ "embed"
)

// NotoEmojiTTF is the embedded Noto Emoji (monochrome, variable weight) font.
// Licensed under the SIL Open Font License 1.1 – see OFL.txt.
//
//go:embed NotoEmoji.ttf
var NotoEmojiTTF []byte
