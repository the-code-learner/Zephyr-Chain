package consensus

// MarshalCertificate is the functional counterpart to ParseCertificate and
// preserves the existing Certificate.MarshalBinary canonical encoding.
func MarshalCertificate(c Certificate) ([]byte, error) {
	return c.MarshalBinary()
}
