package agent

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

// 改写为字符串拼接前后内容必须一字不差（摘要取自改写前的原始反引号字符串）。
func TestPromptTextsUnchanged(t *testing.T) {
	for name, c := range map[string]struct {
		s    string
		sum  string
		size int
	}{
		"DefaultSystemPrompt": {DefaultSystemPrompt, "4d92353f3b1183a7625fdfa072f4daf75ee1bb3969160403826439bfc86d132d", 742},
		"planModeNote":        {planModeNote, "77b12f5b7ce9c986a0357f96ce4a0c4b06cc18d27f99be79604f963c44607150", 840},
	} {
		if got := fmt.Sprintf("%x", sha256.Sum256([]byte(c.s))); got != c.sum || len(c.s) != c.size {
			t.Errorf("%s 内容变了：%s len=%d", name, got, len(c.s))
		}
	}
}
