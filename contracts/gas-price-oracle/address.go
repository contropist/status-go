package gaspriceoracle

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"

	wallet_common "github.com/status-im/status-go/services/wallet/common"
)

var ErrorNotAvailableOnChainID = errors.New("not available for chainID")

// Addresses of the gas price oracle contract `OVM_GasPriceOracle` on different chains.
var contractAddressByChainID = map[uint64]common.Address{
	wallet_common.OptimismMainnet: common.HexToAddress("0x8527c030424728cF93E72bDbf7663281A44Eeb22"),
	wallet_common.OptimismSepolia: common.HexToAddress("0x5230210c2b4995FD5084b0F5FD0D7457aebb5010"),
	wallet_common.ArbitrumMainnet: common.HexToAddress("0x8527c030424728cF93E72bDbf7663281A44Eeb22"),
	wallet_common.ArbitrumSepolia: common.HexToAddress("0x5230210c2b4995FD5084b0F5FD0D7457aebb5010"),
	wallet_common.BaseMainnet:     common.HexToAddress("0x8527c030424728cF93E72bDbf7663281A44Eeb22"),
	wallet_common.BaseSepolia:     common.HexToAddress("0x5230210c2b4995FD5084b0F5FD0D7457aebb5010"),
}

// We stopped uisng `OVM_GasPriceOracle` contract, cause it returns significantly higher gas prices than the actual ones.
// But we don't remove this code for now.
func ContractAddress(chainID uint64) (common.Address, error) {
	addr, exists := contractAddressByChainID[chainID]
	if !exists {
		return common.Address{}, ErrorNotAvailableOnChainID
	}
	return addr, nil
}
