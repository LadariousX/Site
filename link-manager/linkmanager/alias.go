package linkmanager

import (
	"crypto/rand"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

// ShortLinkHost is the public prefix a short link's alias is appended to.
// Override with PUBLIC_LINK_HOST (e.g. "http://localhost:8080/l/" for local
// development) so links generated off the production domain still resolve.
var ShortLinkHost = getEnv("PUBLIC_LINK_HOST", "https://laydenb.com/l/")

// ShortLinkHostDisplay is ShortLinkHost with its scheme stripped, for
// compact UI labels (e.g. "laydenb.com/l/" instead of "https://laydenb.com/l/").
func ShortLinkHostDisplay() string {
	if i := strings.Index(ShortLinkHost, "://"); i != -1 {
		return ShortLinkHost[i+3:]
	}
	return ShortLinkHost
}

// ParseLinkID parses a link's numeric ID from a path value.
func ParseLinkID(s string) (uint, error) {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(n), nil
}

// aliasPattern is the strict charset allowed for both generated and
// user-supplied aliases. It is validated before the alias ever touches the
// filesystem, ruling out path traversal via the alias itself. Minimum length
// 3 avoids issues observed with 1-character aliases.
var aliasPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,64}$`)

func randDigit() (byte, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(10))
	if err != nil {
		return 0, err
	}
	return '0' + byte(n.Int64()), nil
}

func randLetter() (byte, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(26))
	if err != nil {
		return 0, err
	}
	return 'a' + byte(n.Int64()), nil
}

// GenerateAlias returns a random alias in the form "ddlll-ddlll" (two
// digits, three lowercase letters, a hyphen, then the same again again),
// e.g. "42abc-77xyz", for when the user leaves the alias field blank.
func GenerateAlias() (string, error) {
	out := make([]byte, 0, 11)
	appendGroup := func() error {
		for i := 0; i < 2; i++ {
			d, err := randDigit()
			if err != nil {
				return err
			}
			out = append(out, d)
		}
		for i := 0; i < 3; i++ {
			l, err := randLetter()
			if err != nil {
				return err
			}
			out = append(out, l)
		}
		return nil
	}

	if err := appendGroup(); err != nil {
		return "", err
	}
	out = append(out, '-')
	if err := appendGroup(); err != nil {
		return "", err
	}

	return string(out), nil
}

// ValidAlias reports whether alias uses the strict charset link-manager
// allows for both generated and user-supplied aliases.
func ValidAlias(alias string) bool {
	return aliasPattern.MatchString(alias)
}
