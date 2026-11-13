package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/agent-sandbox/agent-sandbox/pkg/config"
)

// TrimUserName normalises an API token to the user identity it stands for. E2B SDKs >= 2.27
// force an "e2b_" prefix on keys, so "e2b_X" and "X" are the same user (see IsTokenAllowed).
// Compare tokens for identity through this function, never as raw strings.
func TrimUserName(token string) string {
	return strings.TrimPrefix(token, "e2b_")
}

// IsAdmin reports whether token is the configured system (admin) token. An exact match is
// required: other tokens that merely start with "sys-" are ordinary users. All admin checks
// go through this function so the rule lives in one place.
func IsAdmin(token string) bool {
	sys := config.Cfg.GetSystemToken()
	return sys != "" && TrimUserName(token) == TrimUserName(sys)
}

func ExtractToken(r *http.Request) string {
	token := strings.TrimSpace(r.Header.Get("X-Api-Key"))
	if token != "" {
		return token
	}
	return strings.TrimSpace(r.URL.Query().Get("api_key"))
}

func IsTokenAllowed(token string) bool {
	if token == "" {
		return false
	}
	for _, allowed := range config.Cfg.GetAPITokens() {
		// e2b@2.27.0, Validate the E2B API key format client-side. SDKs now throw an AuthenticationError / AuthenticationException with an example token (e.g. e2b_0000000000000000000000000000000000000000) when the key does not start with e2b_ followed by hex characters(https://github.com/e2b-dev/E2B/releases/tag/e2b%402.27.0)
		// compatible with E2B new API key format, ignore the prefix when compare
		// legal:
		//client: e2b_000000000000000000, server: e2b_000000000000000000
		//client: e2b_000000000000000000, server: 000000000000000000
		//client: 000000000000000000, server: 000000000000000000
		if TrimUserName(token) == TrimUserName(allowed) {
			return true
		}
	}
	return false
}

func ValidateRequestToken(r *http.Request) (string, bool) {
	token := ExtractToken(r)
	if !IsTokenAllowed(token) {
		return "", false
	}

	return token, true
}

func GetUserTokenFromContext(ctx context.Context) string {
	value := ctx.Value("user")
	user, ok := value.(string)
	if !ok {
		return ""
	}
	return user
}
