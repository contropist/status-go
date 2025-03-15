package fees

import (
	"context"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	gaspriceproxy "github.com/status-im/status-go/contracts/gas-price-proxy"
	"github.com/status-im/status-go/services/wallet/common"
)

type FeeHistory struct {
	BaseFeePerGas []string   `json:"baseFeePerGas"`
	GasUsedRatio  []float64  `json:"gasUsedRatio"`
	OldestBlock   string     `json:"oldestBlock"`
	Reward        [][]string `json:"reward,omitempty"`
}

func (fh *FeeHistory) isEIP1559Compatible() bool {
	if len(fh.BaseFeePerGas) == 0 {
		return false
	}

	for _, fee := range fh.BaseFeePerGas {
		if fee != "0x0" {
			return true
		}
	}

	return false
}

func (f *FeeManager) getFeeHistory(ctx context.Context, chainID uint64, blockCount uint64, newestBlock string, rewardPercentiles []int) (feeHistory *FeeHistory, err error) {
	feeHistory = &FeeHistory{}
	err = f.RPCClient.Call(feeHistory, chainID, "eth_feeHistory", blockCount, newestBlock, rewardPercentiles)
	if err != nil {
		return nil, err
	}

	return feeHistory, nil
}

// GetL1Fee returns L1 fee for placing a transaction to L1 chain, appicable only for txs made from L2.
func (f *FeeManager) GetL1Fee(ctx context.Context, chainID uint64, input []byte) (uint64, error) {
	if chainID == common.EthereumMainnet || chainID == common.EthereumSepolia {
		return 0, nil
	}

	ethClient, err := f.RPCClient.EthClient(chainID)
	if err != nil {
		return 0, err
	}

	contractAddress, err := gaspriceproxy.ContractAddress(chainID)
	if err != nil {
		return 0, err
	}

	contract, err := gaspriceproxy.NewGaspriceproxy(contractAddress, ethClient)
	if err != nil {
		return 0, err
	}

	callOpt := &bind.CallOpts{}

	result, err := contract.GetL1Fee(callOpt, input)
	if err != nil {
		return 0, err
	}

	return result.Uint64(), nil
}
