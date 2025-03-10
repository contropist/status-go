package transfer

import (
	"fmt"
	"math/big"
	"time"

	ethTypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/event"
	"github.com/status-im/status-go/account"
	"github.com/status-im/status-go/eth-node/crypto"
	"github.com/status-im/status-go/eth-node/types"
	"github.com/status-im/status-go/multiaccounts/accounts"
	"github.com/status-im/status-go/params"
	wallet_common "github.com/status-im/status-go/services/wallet/common"
	"github.com/status-im/status-go/services/wallet/router/pathprocessor"
	"github.com/status-im/status-go/transactions"
)

type SignatureDetails struct {
	R string `json:"r"`
	S string `json:"s"`
	V string `json:"v"`
}

type TransactionDescription struct {
	chainID   uint64
	from      common.Address
	builtTx   *ethTypes.Transaction
	signature []byte
}

type TransactionManager struct {
	storage        MultiTransactionStorage
	gethManager    *account.GethManager
	transactor     transactions.TransactorIface
	config         *params.NodeConfig
	accountsDB     accounts.AccountsStorage
	pendingTracker *transactions.PendingTxTracker
	eventFeed      *event.Feed

	multiTransactionForKeycardSigning *MultiTransaction
	multipathTransactionsData         []*pathprocessor.MultipathProcessorTxArgs
	transactionsForKeycardSigning     map[common.Hash]*TransactionDescription
}

type MultiTransactionStorage interface {
	CreateMultiTransaction(tx *MultiTransaction) error
	ReadMultiTransactions(details *MultiTxDetails) ([]*MultiTransaction, error)
	UpdateMultiTransaction(tx *MultiTransaction) error
	DeleteMultiTransaction(id wallet_common.MultiTransactionIDType) error
}

func NewTransactionManager(
	storage MultiTransactionStorage,
	gethManager *account.GethManager,
	transactor transactions.TransactorIface,
	config *params.NodeConfig,
	accountsDB accounts.AccountsStorage,
	pendingTxManager *transactions.PendingTxTracker,
	eventFeed *event.Feed,
) *TransactionManager {
	return &TransactionManager{
		storage:        storage,
		gethManager:    gethManager,
		transactor:     transactor,
		config:         config,
		accountsDB:     accountsDB,
		pendingTracker: pendingTxManager,
		eventFeed:      eventFeed,
	}
}

var (
	emptyHash = common.Hash{}
)

type MultiTransactionType uint8

const (
	MultiTransactionSend = iota
	MultiTransactionSwap
	MultiTransactionBridge
	MultiTransactionApprove
	MultiTransactionTypeInvalid = 255
)

type MultiTransaction struct {
	ID            wallet_common.MultiTransactionIDType `json:"id"`
	Timestamp     uint64                               `json:"timestamp"`
	FromNetworkID uint64                               `json:"fromNetworkID"`
	ToNetworkID   uint64                               `json:"toNetworkID"`
	FromTxHash    common.Hash                          `json:"fromTxHash"`
	ToTxHash      common.Hash                          `json:"toTxHash"`
	FromAddress   common.Address                       `json:"fromAddress"`
	ToAddress     common.Address                       `json:"toAddress"`
	FromAsset     string                               `json:"fromAsset"`
	ToAsset       string                               `json:"toAsset"`
	FromAmount    *hexutil.Big                         `json:"fromAmount"`
	ToAmount      *hexutil.Big                         `json:"toAmount"`
	Type          MultiTransactionType                 `json:"type"`
	CrossTxID     string
}

type MultiTransactionCommand struct {
	FromAddress common.Address       `json:"fromAddress"`
	ToAddress   common.Address       `json:"toAddress"`
	FromAsset   string               `json:"fromAsset"`
	ToAsset     string               `json:"toAsset"`
	FromAmount  *hexutil.Big         `json:"fromAmount"`
	ToAmount    *hexutil.Big         `json:"toAmount"`
	Type        MultiTransactionType `json:"type"`
}

type MultiTransactionCommandResult struct {
	ID     int64                   `json:"id"`
	Hashes map[uint64][]types.Hash `json:"hashes"`
}

type TransactionIdentity struct {
	ChainID wallet_common.ChainID `json:"chainId"`
	Hash    common.Hash           `json:"hash"`
	Address common.Address        `json:"address"`
}

type TxResponse struct {
	KeyUID        string                  `json:"keyUid,omitempty"`
	Address       types.Address           `json:"address,omitempty"`
	AddressPath   string                  `json:"addressPath,omitempty"`
	SignOnKeycard bool                    `json:"signOnKeycard,omitempty"`
	ChainID       uint64                  `json:"chainId,omitempty"`
	MessageToSign interface{}             `json:"messageToSign,omitempty"`
	TxArgs        transactions.SendTxArgs `json:"txArgs,omitempty"`
	RawTx         string                  `json:"rawTx,omitempty"`
	TxHash        common.Hash             `json:"txHash,omitempty"`
}

func NewMultiTransaction(timestamp uint64, fromNetworkID, toNetworkID uint64, fromTxHash, toTxHash common.Hash, fromAddress, toAddress common.Address, fromAsset, toAsset string, fromAmount, toAmount *hexutil.Big, txType MultiTransactionType, crossTxID string) *MultiTransaction {
	if timestamp == 0 {
		timestamp = uint64(time.Now().Unix())
	}

	return &MultiTransaction{
		ID:            multiTransactionIDGenerator(),
		Timestamp:     timestamp,
		FromNetworkID: fromNetworkID,
		ToNetworkID:   toNetworkID,
		FromTxHash:    fromTxHash,
		ToTxHash:      toTxHash,
		FromAddress:   fromAddress,
		ToAddress:     toAddress,
		FromAsset:     fromAsset,
		ToAsset:       toAsset,
		FromAmount:    fromAmount,
		ToAmount:      toAmount,
		Type:          txType,
		CrossTxID:     crossTxID,
	}
}

func (tm *TransactionManager) SignMessage(message types.HexBytes, account *types.Key) (string, error) {
	if account == nil || account.PrivateKey == nil {
		return "", fmt.Errorf("account or private key is nil")
	}

	signature, err := crypto.Sign(message[:], account.PrivateKey)

	return types.EncodeHex(signature), err
}

func (tm *TransactionManager) BuildTransaction(chainID uint64, sendArgs transactions.SendTxArgs) (response *TxResponse, err error) {
	account, err := tm.accountsDB.GetAccountByAddress(sendArgs.From)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve account: %w", err)
	}

	kp, err := tm.accountsDB.GetKeypairByKeyUID(account.KeyUID)
	if err != nil {
		return nil, err
	}

	txBeingSigned, _, err := tm.transactor.ValidateAndBuildTransaction(chainID, sendArgs, -1)
	if err != nil {
		return nil, err
	}

	// Set potential missing fields that were added while building the transaction
	if sendArgs.Value == nil {
		value := hexutil.Big(*txBeingSigned.Value())
		sendArgs.Value = &value
	}
	if sendArgs.Nonce == nil {
		nonce := hexutil.Uint64(txBeingSigned.Nonce())
		sendArgs.Nonce = &nonce
	}
	if sendArgs.Gas == nil {
		gas := hexutil.Uint64(txBeingSigned.Gas())
		sendArgs.Gas = &gas
	}
	if sendArgs.GasPrice == nil {
		gasPrice := hexutil.Big(*txBeingSigned.GasPrice())
		sendArgs.GasPrice = &gasPrice
	}

	if sendArgs.IsDynamicFeeTx() {
		if sendArgs.MaxPriorityFeePerGas == nil {
			maxPriorityFeePerGas := hexutil.Big(*txBeingSigned.GasTipCap())
			sendArgs.MaxPriorityFeePerGas = &maxPriorityFeePerGas
		}
		if sendArgs.MaxFeePerGas == nil {
			maxFeePerGas := hexutil.Big(*txBeingSigned.GasFeeCap())
			sendArgs.MaxFeePerGas = &maxFeePerGas
		}
	}

	signer := ethTypes.NewLondonSigner(new(big.Int).SetUint64(chainID))

	return &TxResponse{
		KeyUID:        account.KeyUID,
		Address:       account.Address,
		AddressPath:   account.Path,
		SignOnKeycard: kp.MigratedToKeycard(),
		ChainID:       chainID,
		MessageToSign: signer.Hash(txBeingSigned),
		TxArgs:        sendArgs,
	}, nil
}

func (tm *TransactionManager) BuildRawTransaction(chainID uint64, sendArgs transactions.SendTxArgs, signature []byte) (response *TxResponse, err error) {
	tx, err := tm.transactor.BuildTransactionWithSignature(chainID, sendArgs, signature)
	if err != nil {
		return nil, err
	}

	data, err := tx.MarshalBinary()
	if err != nil {
		return nil, err
	}

	return &TxResponse{
		ChainID: chainID,
		TxArgs:  sendArgs,
		RawTx:   types.EncodeHex(data),
		TxHash:  tx.Hash(),
	}, nil
}

func (tm *TransactionManager) SendTransactionWithSignature(chainID uint64, sendArgs transactions.SendTxArgs, signature []byte) (hash types.Hash, err error) {
	txWithSignature, err := tm.transactor.BuildTransactionWithSignature(chainID, sendArgs, signature)
	if err != nil {
		return hash, err
	}

	hash, err = tm.transactor.SendTransactionWithSignature(common.Address(sendArgs.From), sendArgs.Symbol, sendArgs.MultiTransactionID, txWithSignature)
	return hash, err
}
