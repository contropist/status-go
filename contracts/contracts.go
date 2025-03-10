package contracts

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/status-im/status-go/contracts/balancechecker"
	"github.com/status-im/status-go/contracts/directory"
	"github.com/status-im/status-go/contracts/ethscan"
	"github.com/status-im/status-go/contracts/ierc20"
	"github.com/status-im/status-go/contracts/registrar"
	"github.com/status-im/status-go/contracts/resolver"
	"github.com/status-im/status-go/contracts/snt"
	"github.com/status-im/status-go/contracts/stickers"
	"github.com/status-im/status-go/rpc"
)

type ContractMakerIface interface {
	NewEthScan(chainID uint64) (ethscan.BalanceScannerIface, uint, error)
	NewERC20(chainID uint64, contractAddr common.Address) (ierc20.IERC20Iface, error)
	NewERC20Caller(chainID uint64, contractAddr common.Address) (ierc20.IERC20CallerIface, error)
	// TODO extend with other contracts
}

type ContractMaker struct {
	RPCClient rpc.ClientInterface
}

func NewContractMaker(client rpc.ClientInterface) (*ContractMaker, error) {
	if client == nil {
		return nil, errors.New("could not initialize ContractMaker with an rpc client")
	}
	return &ContractMaker{RPCClient: client}, nil
}

func (c *ContractMaker) NewRegistryWithAddress(chainID uint64, address common.Address) (*resolver.ENSRegistryWithFallback, error) {
	backend, err := c.RPCClient.EthClient(chainID)
	if err != nil {
		return nil, err
	}

	return resolver.NewENSRegistryWithFallback(
		address,
		backend,
	)
}

func (c *ContractMaker) NewRegistry(chainID uint64) (*resolver.ENSRegistryWithFallback, error) {
	contractAddr, err := resolver.ContractAddress(chainID)
	if err != nil {
		return nil, err
	}
	return c.NewRegistryWithAddress(chainID, contractAddr)
}

func (c *ContractMaker) NewPublicResolver(chainID uint64, resolverAddress *common.Address) (*resolver.PublicResolver, error) {
	backend, err := c.RPCClient.EthClient(chainID)
	if err != nil {
		return nil, err
	}

	return resolver.NewPublicResolver(*resolverAddress, backend)
}

func (c *ContractMaker) NewUsernameRegistrar(chainID uint64, contractAddr common.Address) (*registrar.UsernameRegistrar, error) {
	backend, err := c.RPCClient.EthClient(chainID)
	if err != nil {
		return nil, err
	}

	return registrar.NewUsernameRegistrar(
		contractAddr,
		backend,
	)
}

func (c *ContractMaker) NewERC20(chainID uint64, contractAddr common.Address) (ierc20.IERC20Iface, error) {
	backend, err := c.RPCClient.EthClient(chainID)
	if err != nil {
		return nil, err
	}

	return ierc20.NewIERC20(
		contractAddr,
		backend,
	)
}
func (c *ContractMaker) NewERC20Caller(chainID uint64, contractAddr common.Address) (ierc20.IERC20CallerIface, error) {
	backend, err := c.RPCClient.EthClient(chainID)
	if err != nil {
		return nil, err
	}

	return ierc20.NewIERC20Caller(contractAddr, backend)
}

func (c *ContractMaker) NewSNT(chainID uint64) (*snt.SNT, error) {
	contractAddr, err := snt.ContractAddress(chainID)
	if err != nil {
		return nil, err
	}

	backend, err := c.RPCClient.EthClient(chainID)
	if err != nil {
		return nil, err
	}

	return snt.NewSNT(contractAddr, backend)
}

func (c *ContractMaker) NewStickerType(chainID uint64) (*stickers.StickerType, error) {
	contractAddr, err := stickers.StickerTypeContractAddress(chainID)
	if err != nil {
		return nil, err
	}

	backend, err := c.RPCClient.EthClient(chainID)
	if err != nil {
		return nil, err
	}

	return stickers.NewStickerType(
		contractAddr,
		backend,
	)
}

func (c *ContractMaker) NewStickerMarket(chainID uint64) (*stickers.StickerMarket, error) {
	contractAddr, err := stickers.StickerMarketContractAddress(chainID)
	if err != nil {
		return nil, err
	}

	backend, err := c.RPCClient.EthClient(chainID)
	if err != nil {
		return nil, err
	}

	return stickers.NewStickerMarket(
		contractAddr,
		backend,
	)
}

func (c *ContractMaker) NewStickerPack(chainID uint64) (*stickers.StickerPack, error) {
	contractAddr, err := stickers.StickerPackContractAddress(chainID)
	if err != nil {
		return nil, err
	}

	backend, err := c.RPCClient.EthClient(chainID)
	if err != nil {
		return nil, err
	}

	return stickers.NewStickerPack(
		contractAddr,
		backend,
	)
}

func (c *ContractMaker) NewDirectory(chainID uint64) (*directory.Directory, error) {
	contractAddr, err := directory.ContractAddress(chainID)
	if err != nil {
		return nil, err
	}

	backend, err := c.RPCClient.EthClient(chainID)
	if err != nil {
		return nil, err
	}

	return directory.NewDirectory(
		contractAddr,
		backend,
	)
}

func (c *ContractMaker) NewEthScan(chainID uint64) (ethscan.BalanceScannerIface, uint, error) {
	contractAddr, err := ethscan.ContractAddress(chainID)
	if err != nil {
		return nil, 0, err
	}

	contractCreatedAt, err := ethscan.ContractCreatedAt(chainID)
	if err != nil {
		return nil, 0, err
	}

	backend, err := c.RPCClient.EthClient(chainID)
	if err != nil {
		return nil, 0, err
	}

	scanner, err := ethscan.NewBalanceScanner(
		contractAddr,
		backend,
	)

	return scanner, contractCreatedAt, err
}

func (c *ContractMaker) NewBalanceChecker(chainID uint64) (*balancechecker.BalanceChecker, error) {
	contractAddr, err := balancechecker.ContractAddress(chainID)
	if err != nil {
		return nil, err
	}

	backend, err := c.RPCClient.EthClient(chainID)
	if err != nil {
		return nil, err
	}
	return balancechecker.NewBalanceChecker(
		contractAddr,
		backend,
	)
}
