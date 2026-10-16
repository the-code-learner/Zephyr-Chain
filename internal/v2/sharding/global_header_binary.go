package sharding

// MarshalBinary returns the canonical wire representation accepted by
// ParseGlobalHeader. Validation is performed before returning bytes so durable
// journal records cannot encode a header that the recovery path would reject.
func (h GlobalHeader) MarshalBinary() ([]byte, error) {
	raw := h.CanonicalBytes()
	if _, err := ParseGlobalHeader(raw); err != nil {
		return nil, err
	}
	return raw, nil
}
