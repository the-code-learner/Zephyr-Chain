package lightapi

import "encoding/json"

func (v validatorDTO) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID        string `json:"id"`
		PublicKey []byte `json:"publicKey"`
		Power     string `json:"power"`
	}{
		ID: v.ID, PublicKey: v.PublicKey, Power: v.Power,
	})
}
