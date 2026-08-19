package execution

import "github.com/zephyr-chain/zephyr-chain/internal/v2/object"

func stampCoinOutput(spec object.OutputSpec, height uint64) (object.OutputSpec, error) {
	if spec.Kind != object.KindCoin {
		return spec, nil
	}
	coin, err := object.ParseCoin(spec.Data)
	if err != nil {
		return object.OutputSpec{}, err
	}
	coin.CreatedHeight = height
	stamped := spec
	stamped.Data = coin.MarshalBinary()
	return stamped, nil
}
