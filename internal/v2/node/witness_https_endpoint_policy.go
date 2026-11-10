package node

import (
	"net/url"
	"path"
	"strings"
)

// canonicalWitnessHTTPSPath binds an operator-provisioned HTTPS endpoint to
// exactly one request-target representation. Decoded/encoded path aliases,
// dot segments and repeated separators can be normalized differently by
// an HTTP client, intermediary and custody/floor server. Reject them before
// any network request rather than trusting accidental URL normalization.
// A trailing slash is valid only when it is otherwise canonical.
func canonicalWitnessHTTPSPath(u *url.URL) bool {
	if u == nil || u.Path == "" || !strings.HasPrefix(u.Path, "/") ||
		u.RawPath != "" || u.EscapedPath() != u.Path {
		return false
	}
	clean := path.Clean(u.Path)
	return clean == u.Path || (strings.HasSuffix(u.Path, "/") && clean+"/" == u.Path)
}
