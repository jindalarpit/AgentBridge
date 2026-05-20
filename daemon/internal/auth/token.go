package auth

// FormatTokenName generates a token name in the format "Daemon ({hostname})".
// The hostname is truncated so that the total name length never exceeds 100 characters.
// Since the prefix "Daemon (" is 8 characters and the suffix ")" is 1 character,
// the hostname portion is limited to at most 91 characters.
// Additionally, the hostname itself is capped at 253 characters (DNS limit),
// but the 91-character limit for the name takes precedence.
func FormatTokenName(hostname string) string {
	const (
		prefix       = "Daemon ("
		suffix       = ")"
		maxTotal     = 100
		maxHostname  = 253
		maxInName    = maxTotal - len(prefix) - len(suffix) // 91
	)

	// Apply DNS hostname limit first
	h := hostname
	if len(h) > maxHostname {
		h = h[:maxHostname]
	}

	// Apply name length limit (more restrictive)
	if len(h) > maxInName {
		h = h[:maxInName]
	}

	return prefix + h + suffix
}
