package approve

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// ErrCredentialToken reports that a remembered rule holds a token shaped like a credential. The
// project file may be committed, and a secret that is pushed cannot be recalled.
var ErrCredentialToken = errors.New("an allow rule must not hold a credential-shaped token")

// CredentialPlaceholder stands in for a credential-shaped token wherever a message would
// otherwise print it.
const CredentialPlaceholder = "<credential>"

// credentialBoundary is what may precede a known token prefix. It keeps `task-...` from reading
// as an `sk-` key, and still lets `--token=ghp_...` and `TOKEN: ghp_...` match.
const credentialBoundary = `(?:^|[^A-Za-z0-9_-])`

// flagPrefix is what may glue a header to its option: `--header=` or a short flag such as `-H`.
const flagPrefix = `(?:--[a-z][a-z0-9-]*=|-[a-z])?`

const bearerValue = `[A-Za-z0-9._~+/=-]{16,}`

var (
	// headerRE matches a whole `Name: value` token, with an optional `--header=` style prefix.
	headerRE = regexp.MustCompile(`(?i)^` + flagPrefix + `\s*(?:proxy-authorization|authorization|cookie)\s*:\s*\S`)

	// headerNameRE matches a token that is only the header name. The value is the next token.
	headerNameRE = regexp.MustCompile(`(?i)^` + flagPrefix + `\s*(?:proxy-authorization|authorization|cookie)\s*:\s*$`)

	bearerRE      = regexp.MustCompile(`(?i)(?:^|[\s=:"'])bearer\s+(` + bearerValue + `)`)
	bearerValueRE = regexp.MustCompile(`^` + bearerValue + `$`)
	bearerFlagRE  = regexp.MustCompile(`(?i)^--oauth2-bearer=\S`)

	// userinfoRE matches `scheme://user:password@`. The user may be empty, as in
	// `redis://:password@host`. A user with no password is left alone, since `ssh://git@host`
	// names an account and not a secret.
	userinfoRE = regexp.MustCompile(`(?:^|[^A-Za-z0-9+.-])[A-Za-z][A-Za-z0-9+.-]*://[^/\s?#@:]*:[^/\s?#@]+@`)
)

// prefixPattern is an anchored token prefix with the shape of what follows it. The optional ok
// check rejects bodies that look like ordinary words.
type prefixPattern struct {
	re *regexp.Regexp
	ok func(body string) bool
}

var prefixPatterns = []prefixPattern{
	{re: regexp.MustCompile(credentialBoundary + `(gh[pousr]_[A-Za-z0-9]{36,})`)},
	{re: regexp.MustCompile(credentialBoundary + `(github_pat_[A-Za-z0-9_]{50,})`)},
	{re: regexp.MustCompile(credentialBoundary + `(glpat-[A-Za-z0-9_-]{20,})`)},
	{re: regexp.MustCompile(credentialBoundary + `(npm_[A-Za-z0-9]{36,})`)},
	{re: regexp.MustCompile(credentialBoundary + `((?:AKIA|ASIA)[A-Z0-9]{16})(?:$|[^A-Za-z0-9])`)},
	{re: regexp.MustCompile(credentialBoundary + `(AIza[A-Za-z0-9_-]{35})`)},
	{re: regexp.MustCompile(credentialBoundary + `(xox[abeprs]-[A-Za-z0-9-]{10,})`)},
	{re: regexp.MustCompile(credentialBoundary + `(xapp-[0-9]-[A-Za-z0-9-]{10,})`)},
	{re: regexp.MustCompile(credentialBoundary + `((?:sk|rk)_(?:live|test)_[A-Za-z0-9]{24,})`)},
	{re: regexp.MustCompile(credentialBoundary + `(eyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]+)`)},
	{re: regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},

	// A branch or a word can start with `sk-`, so the body must also look random: long, with
	// both a digit and an uppercase letter.
	{re: regexp.MustCompile(credentialBoundary + `(sk-[A-Za-z0-9_-]{32,})`), ok: randomLooking},
}

// CredentialPositions returns the zero-based indexes of the tokens that look like a credential.
// It reads shapes and not secrets. A shape is a header value, a bearer value, URL userinfo, or a
// known token prefix. It cannot see `mysql -phunter2`, `--password hunter2`, `curl -u user:pass`,
// query-parameter tokens, or an opaque value, and no result from it means a rule is clean.
//
// A header name or `Bearer` in a token of its own marks the token after it, so a header split
// across two tokens is caught on its value.
func CredentialPositions(tokens []string) []int {
	var positions []int

	for i, token := range tokens {
		switch {
		case credentialToken(token):
			positions = append(positions, i)
		case i > 0 && credentialPrecedes(tokens[i-1], token):
			positions = append(positions, i)
		}
	}

	return positions
}

// RedactCredentials returns tokens with each credential-shaped one replaced by
// CredentialPlaceholder, for a message that must not echo it.
func RedactCredentials(tokens []string) []string {
	out := append([]string(nil), tokens...)
	for _, i := range CredentialPositions(tokens) {
		out[i] = CredentialPlaceholder
	}

	return out
}

// CredentialText reports whether free text, such as a rule field that did not parse, holds a
// credential-shaped piece.
func CredentialText(text string) bool {
	pieces := strings.FieldsFunc(text, func(r rune) bool {
		return strings.ContainsRune("[],\"'", r) || unicode.IsSpace(r)
	})

	return credentialToken(text) || len(CredentialPositions(pieces)) > 0
}

func credentialToken(token string) bool {
	if headerRE.MatchString(token) || bearerFlagRE.MatchString(token) || userinfoRE.MatchString(token) {
		return true
	}

	for _, m := range bearerRE.FindAllStringSubmatch(token, -1) {
		if hasDigit(m[1]) {
			return true
		}
	}

	for _, p := range prefixPatterns {
		m := p.re.FindStringSubmatch(token)
		if m == nil {
			continue
		}

		if p.ok == nil || p.ok(m[1]) {
			return true
		}
	}

	return false
}

// credentialPrecedes reports whether prev makes the token after it a credential value.
func credentialPrecedes(prev, token string) bool {
	switch {
	case strings.TrimSpace(token) == "":
		return false
	case headerNameRE.MatchString(prev):
		return true
	case strings.EqualFold(prev, "--oauth2-bearer"):
		return true
	case strings.EqualFold(prev, "bearer"):
		return bearerValueRE.MatchString(token) && hasDigit(token)
	}

	return false
}

func randomLooking(body string) bool {
	upper := false
	for _, r := range body {
		if unicode.IsUpper(r) {
			upper = true
		}
	}

	return upper && hasDigit(body)
}

func hasDigit(s string) bool {
	return strings.ContainsFunc(s, unicode.IsDigit)
}

// DescribePositions names zero-based token positions for a message, one-based and without the
// tokens: "token 3" or "tokens 2 and 5".
func DescribePositions(positions []int) string {
	numbers := make([]string, len(positions))
	for i, p := range positions {
		numbers[i] = strconv.Itoa(p + 1)
	}

	switch len(numbers) {
	case 0:
		return "no token"
	case 1:
		return "token " + numbers[0]
	case 2:
		return "tokens " + numbers[0] + " and " + numbers[1]
	}

	return "tokens " + strings.Join(numbers[:len(numbers)-1], ", ") + ", and " + numbers[len(numbers)-1]
}
