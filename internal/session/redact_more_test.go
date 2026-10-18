package session

import (
	"strings"
	"testing"
)

func TestRedactMoreSecretShapes(t *testing.T) {
	cases := map[string]string{
		"ghp":    "token ghp_" + strings.Repeat("a1B2", 9),
		"pat":    "github_pat_" + strings.Repeat("X9_y", 15),
		"awsid":  "id AKIA" + strings.Repeat("Q", 16),
		"awssec": "aws_secret_access_key = " + strings.Repeat("abCD12/+", 5),
		"pem":    "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----",
	}
	secrets := map[string]string{
		"ghp": strings.Repeat("a1B2", 9), "pat": strings.Repeat("X9_y", 15),
		"awsid": strings.Repeat("Q", 16), "awssec": strings.Repeat("abCD12/+", 5),
		"pem": "b3BlbnNzaC1rZXktdjEAAAAA",
	}
	for name, in := range cases {
		out := RedactSecrets(in)
		if strings.Contains(out, secrets[name]) {
			t.Errorf("%s not redacted: %q", name, out)
		}
	}
}
