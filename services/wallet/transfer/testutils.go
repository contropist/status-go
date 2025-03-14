package transfer

import (
	"database/sql"
	"fmt"
	"math/big"
	"testing"

	eth_common "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/status-im/status-go/services/wallet/bigint"
	"github.com/status-im/status-go/services/wallet/common"
	"github.com/status-im/status-go/services/wallet/testutils"
	"github.com/status-im/status-go/services/wallet/token"

	"github.com/stretchr/testify/require"
)

type TestTransaction struct {
	Hash               eth_common.Hash
	ChainID            common.ChainID
	From               eth_common.Address // [sender]
	Timestamp          int64
	BlkNumber          int64
	Success            bool
	Nonce              uint64
	Contract           eth_common.Address
	MultiTransactionID common.MultiTransactionIDType
}

type TestTransfer struct {
	TestTransaction
	To    eth_common.Address // [address]
	Value int64
	Token *token.Token
}

type TestCollectibleTransfer struct {
	TestTransfer
	TestCollectible
}

func SeedToToken(seed int) *token.Token {
	tokenIndex := seed % len(TestTokens)
	return TestTokens[tokenIndex]
}

func TestTrToToken(t *testing.T, tt *TestTransaction) (token *token.Token, isNative bool) {
	// Sanity check that none of the markers changed and they should be equal to seed
	require.Equal(t, tt.Timestamp, tt.BlkNumber)

	tokenIndex := int(tt.Timestamp) % len(TestTokens)
	isNative = testutils.SliceContains(NativeTokenIndices, tokenIndex)

	return TestTokens[tokenIndex], isNative
}

func generateTestTransaction(seed int) TestTransaction {
	token := SeedToToken(seed)
	return TestTransaction{
		Hash:      eth_common.HexToHash(fmt.Sprintf("0x1%d", seed)),
		ChainID:   common.ChainID(token.ChainID),
		From:      eth_common.HexToAddress(fmt.Sprintf("0x2%d", seed)),
		Timestamp: int64(seed),
		BlkNumber: int64(seed),
		Success:   true,
		Nonce:     uint64(seed),
		// In practice this is last20Bytes(Keccak256(RLP(From, nonce)))
		Contract:           eth_common.HexToAddress(fmt.Sprintf("0x4%d", seed)),
		MultiTransactionID: common.NoMultiTransactionID,
	}
}

func generateTestTransfer(seed int) TestTransfer {
	tokenIndex := seed % len(TestTokens)
	token := TestTokens[tokenIndex]
	return TestTransfer{
		TestTransaction: generateTestTransaction(seed),
		To:              eth_common.HexToAddress(fmt.Sprintf("0x3%d", seed)),
		Value:           int64(seed),
		Token:           token,
	}
}

// Will be used in tests to generate a collectible transfer
// nolint:unused
func generateTestCollectibleTransfer(seed int) TestCollectibleTransfer {
	collectibleIndex := seed % len(TestCollectibles)
	collectible := TestCollectibles[collectibleIndex]
	tr := TestCollectibleTransfer{
		TestTransfer: TestTransfer{
			TestTransaction: generateTestTransaction(seed),
			To:              eth_common.HexToAddress(fmt.Sprintf("0x3%d", seed)),
			Value:           int64(seed),
			Token: &token.Token{
				Address: collectible.TokenAddress,
				Name:    "Collectible",
				ChainID: uint64(collectible.ChainID),
			},
		},
		TestCollectible: collectible,
	}
	tr.TestTransaction.ChainID = collectible.ChainID
	return tr
}

func GenerateTestSendMultiTransaction(tr TestTransfer) MultiTransaction {
	return MultiTransaction{
		ID:          multiTransactionIDGenerator(),
		Type:        MultiTransactionSend,
		FromAddress: tr.From,
		ToAddress:   tr.To,
		FromAsset:   tr.Token.Symbol,
		ToAsset:     tr.Token.Symbol,
		FromAmount:  (*hexutil.Big)(big.NewInt(tr.Value)),
		ToAmount:    (*hexutil.Big)(big.NewInt(0)),
		Timestamp:   uint64(tr.Timestamp),
	}
}

func GenerateTestSwapMultiTransaction(tr TestTransfer, toToken string, toAmount int64) MultiTransaction {
	return MultiTransaction{
		ID:          multiTransactionIDGenerator(),
		Type:        MultiTransactionSwap,
		FromAddress: tr.From,
		ToAddress:   tr.To,
		FromAsset:   tr.Token.Symbol,
		ToAsset:     toToken,
		FromAmount:  (*hexutil.Big)(big.NewInt(tr.Value)),
		ToAmount:    (*hexutil.Big)(big.NewInt(toAmount)),
		Timestamp:   uint64(tr.Timestamp),
	}
}

func GenerateTestBridgeMultiTransaction(fromTr, toTr TestTransfer) MultiTransaction {
	return MultiTransaction{
		ID:          multiTransactionIDGenerator(),
		Type:        MultiTransactionBridge,
		FromAddress: fromTr.From,
		ToAddress:   toTr.To,
		FromAsset:   fromTr.Token.Symbol,
		ToAsset:     toTr.Token.Symbol,
		FromAmount:  (*hexutil.Big)(big.NewInt(fromTr.Value)),
		ToAmount:    (*hexutil.Big)(big.NewInt(toTr.Value)),
		Timestamp:   uint64(fromTr.Timestamp),
	}
}

func GenerateTestApproveMultiTransaction(tr TestTransfer) MultiTransaction {
	return MultiTransaction{
		ID:          multiTransactionIDGenerator(),
		Type:        MultiTransactionApprove,
		FromAddress: tr.From,
		ToAddress:   tr.To,
		FromAsset:   tr.Token.Symbol,
		ToAsset:     tr.Token.Symbol,
		FromAmount:  (*hexutil.Big)(big.NewInt(tr.Value)),
		ToAmount:    (*hexutil.Big)(big.NewInt(0)),
		Timestamp:   uint64(tr.Timestamp),
	}
}

// GenerateTestTransfers will generate transaction based on the TestTokens index and roll over if there are more than
// len(TestTokens) transactions
func GenerateTestTransfers(tb testing.TB, db *sql.DB, firstStartIndex int, count int) (result []TestTransfer, fromAddresses, toAddresses []eth_common.Address) {
	for i := firstStartIndex; i < (firstStartIndex + count); i++ {
		tr := generateTestTransfer(i)
		fromAddresses = append(fromAddresses, tr.From)
		toAddresses = append(toAddresses, tr.To)
		result = append(result, tr)
	}
	return
}

type TestCollectible struct {
	TokenAddress eth_common.Address
	TokenID      *big.Int
	ChainID      common.ChainID
}

var TestCollectibles = []TestCollectible{
	TestCollectible{
		TokenAddress: eth_common.HexToAddress("0x97a04fda4d97c6e3547d66b572e29f4a4ff40392"),
		TokenID:      big.NewInt(1),
		ChainID:      1,
	},
	TestCollectible{ // Same token ID as above but different address
		TokenAddress: eth_common.HexToAddress("0x2cec8879915cdbd80c88d8b1416aa9413a24ddfa"),
		TokenID:      big.NewInt(1),
		ChainID:      1,
	},
	TestCollectible{ // TokenID (big.Int) value 0 might be problematic if not handled properly
		TokenAddress: eth_common.HexToAddress("0x97a04fda4d97c6e3547d66b572e29f4a4ff4ABCD"),
		TokenID:      big.NewInt(0),
		ChainID:      420,
	},
	TestCollectible{
		TokenAddress: eth_common.HexToAddress("0x1dea7a3e04849840c0eb15fd26a55f6c40c4a69b"),
		TokenID:      big.NewInt(11),
		ChainID:      5,
	},
	TestCollectible{ // Same address as above but different token ID
		TokenAddress: eth_common.HexToAddress("0x1dea7a3e04849840c0eb15fd26a55f6c40c4a69b"),
		TokenID:      big.NewInt(12),
		ChainID:      5,
	},
}

var EthMainnet = token.Token{
	Address: eth_common.HexToAddress("0x"),
	Name:    "Ether",
	Symbol:  "ETH",
	ChainID: 1,
}

var EthGoerli = token.Token{
	Address: eth_common.HexToAddress("0x"),
	Name:    "Ether",
	Symbol:  "ETH",
	ChainID: 5,
}

var EthOptimism = token.Token{
	Address: eth_common.HexToAddress("0x"),
	Name:    "Ether",
	Symbol:  "ETH",
	ChainID: 10,
}

var UsdcMainnet = token.Token{
	Address: eth_common.HexToAddress("0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"),
	Name:    "USD Coin",
	Symbol:  "USDC",
	ChainID: 1,
}

var UsdcGoerli = token.Token{
	Address: eth_common.HexToAddress("0x98339d8c260052b7ad81c28c16c0b98420f2b46a"),
	Name:    "USD Coin",
	Symbol:  "USDC",
	ChainID: 5,
}

var UsdcOptimism = token.Token{
	Address: eth_common.HexToAddress("0x7f5c764cbc14f9669b88837ca1490cca17c31607"),
	Name:    "USD Coin",
	Symbol:  "USDC",
	ChainID: 10,
}

var SntMainnet = token.Token{
	Address: eth_common.HexToAddress("0x744d70fdbe2ba4cf95131626614a1763df805b9e"),
	Name:    "Status Network Token",
	Symbol:  "SNT",
	ChainID: 1,
}

var DaiMainnet = token.Token{
	Address: eth_common.HexToAddress("0xf2edF1c091f683E3fb452497d9a98A49cBA84666"),
	Name:    "DAI Stablecoin",
	Symbol:  "DAI",
	ChainID: 5,
}

var DaiGoerli = token.Token{
	Address: eth_common.HexToAddress("0xf2edF1c091f683E3fb452497d9a98A49cBA84666"),
	Name:    "DAI Stablecoin",
	Symbol:  "DAI",
	ChainID: 5,
}

// TestTokens contains ETH/Mainnet, ETH/Goerli, ETH/Optimism, USDC/Mainnet, USDC/Goerli, USDC/Optimism, SNT/Mainnet, DAI/Mainnet, DAI/Goerli
var TestTokens = []*token.Token{
	&EthMainnet, &EthGoerli, &EthOptimism, &UsdcMainnet, &UsdcGoerli, &UsdcOptimism, &SntMainnet, &DaiMainnet, &DaiGoerli,
}

func LookupTokenIdentity(chainID uint64, address eth_common.Address, native bool) *token.Token {
	for _, token := range TestTokens {
		if token.ChainID == chainID && token.Address == address && token.IsNative() == native {
			return token
		}
	}
	return nil
}

var NativeTokenIndices = []int{0, 1, 2}

func InsertTestTransfer(tb testing.TB, db *sql.DB, address eth_common.Address, tr *TestTransfer) {
	token := TestTokens[int(tr.Timestamp)%len(TestTokens)]
	InsertTestTransferWithOptions(tb, db, address, tr, &TestTransferOptions{
		TokenAddress: token.Address,
	})
}

type TestTransferOptions struct {
	TokenAddress     eth_common.Address
	TokenID          *big.Int
	NullifyAddresses []eth_common.Address
	Tx               *types.Transaction
	Receipt          *types.Receipt
}

func GenerateTxField(data []byte) *types.Transaction {
	return types.NewTx(&types.DynamicFeeTx{
		Data: data,
	})
}

func InsertTestTransferWithOptions(tb testing.TB, db *sql.DB, address eth_common.Address, tr *TestTransfer, opt *TestTransferOptions) {
	var (
		tx *sql.Tx
	)
	tx, err := db.Begin()
	require.NoError(tb, err)
	defer func() {
		if err == nil {
			err = tx.Commit()
			return
		}
		_ = tx.Rollback()
	}()

	blkHash := eth_common.HexToHash("4")

	block := blockDBFields{
		chainID:     uint64(tr.ChainID),
		account:     address,
		blockNumber: big.NewInt(tr.BlkNumber),
		blockHash:   blkHash,
	}

	// Respect `FOREIGN KEY(network_id,address,blk_hash)` of `transfers` table
	err = insertBlockDBFields(tx, block)
	require.NoError(tb, err)

	receiptStatus := uint64(0)
	if tr.Success {
		receiptStatus = 1
	}

	tokenType := "eth"
	if (opt.TokenAddress != eth_common.Address{}) {
		if opt.TokenID == nil {
			tokenType = "erc20"
		} else {
			tokenType = "erc721"
		}
	}

	// Workaround to simulate writing of NULL values for addresses
	txTo := &tr.To
	txFrom := &tr.From
	for i := 0; i < len(opt.NullifyAddresses); i++ {
		if opt.NullifyAddresses[i] == tr.To {
			txTo = nil
		}
		if opt.NullifyAddresses[i] == tr.From {
			txFrom = nil
		}
	}

	transfer := transferDBFields{
		chainID:            uint64(tr.ChainID),
		id:                 tr.Hash,
		txHash:             &tr.Hash,
		address:            address,
		blockHash:          blkHash,
		blockNumber:        big.NewInt(tr.BlkNumber),
		sender:             tr.From,
		transferType:       common.Type(tokenType),
		timestamp:          uint64(tr.Timestamp),
		multiTransactionID: tr.MultiTransactionID,
		baseGasFees:        "0x0",
		receiptStatus:      &receiptStatus,
		txValue:            big.NewInt(tr.Value),
		txFrom:             txFrom,
		txTo:               txTo,
		txNonce:            &tr.Nonce,
		tokenAddress:       &opt.TokenAddress,
		contractAddress:    &tr.Contract,
		tokenID:            opt.TokenID,
		transaction:        opt.Tx,
		receipt:            opt.Receipt,
	}
	err = updateOrInsertTransfersDBFields(tx, []transferDBFields{transfer})
	require.NoError(tb, err)
}

func InsertTestPendingTransaction(tb testing.TB, db *sql.DB, tr *TestTransfer) {
	_, err := db.Exec(`
		INSERT INTO pending_transactions (network_id, hash, timestamp, from_address, to_address,
			symbol, gas_price, gas_limit, value, data, type, additional_data, multi_transaction_id
		) VALUES (?, ?, ?, ?, ?, 'ETH', 0, 0, ?, '', 'eth', '', ?)`,
		tr.ChainID, tr.Hash, tr.Timestamp, tr.From, tr.To, (*bigint.SQLBigIntBytes)(big.NewInt(tr.Value)), tr.MultiTransactionID)
	require.NoError(tb, err)
}

func InsertTestMultiTransaction(tb testing.TB, db *sql.DB, tr *MultiTransaction) common.MultiTransactionIDType {
	if tr.FromAsset == "" {
		tr.FromAsset = testutils.EthSymbol
	}
	if tr.ToAsset == "" {
		tr.ToAsset = testutils.EthSymbol
	}

	tr.ID = multiTransactionIDGenerator()
	multiTxDB := NewMultiTransactionDB(db)
	err := multiTxDB.CreateMultiTransaction(tr)
	require.NoError(tb, err)
	return tr.ID
}

// For using in tests only outside the package
func SaveTransfersMarkBlocksLoaded(database *Database, chainID uint64, address eth_common.Address, transfers []Transfer, blocks []*big.Int) error {
	return saveTransfersMarkBlocksLoaded(database.client, chainID, address, transfers, blocks)
}

func SetMultiTransactionIDGenerator(f func() common.MultiTransactionIDType) {
	multiTransactionIDGenerator = f
}

func StaticIDCounter() (f func() common.MultiTransactionIDType) {
	var i int
	f = func() common.MultiTransactionIDType {
		i++
		return common.MultiTransactionIDType(i)
	}
	return
}

type InMemMultiTransactionStorage struct {
	storage map[common.MultiTransactionIDType]*MultiTransaction
}

func NewInMemMultiTransactionStorage() *InMemMultiTransactionStorage {
	return &InMemMultiTransactionStorage{
		storage: make(map[common.MultiTransactionIDType]*MultiTransaction),
	}
}

func (s *InMemMultiTransactionStorage) CreateMultiTransaction(multiTx *MultiTransaction) error {
	s.storage[multiTx.ID] = multiTx
	return nil
}

func (s *InMemMultiTransactionStorage) GetMultiTransaction(id common.MultiTransactionIDType) (*MultiTransaction, error) {
	multiTx, ok := s.storage[id]
	if !ok {
		return nil, nil
	}
	return multiTx, nil
}

func (s *InMemMultiTransactionStorage) UpdateMultiTransaction(multiTx *MultiTransaction) error {
	s.storage[multiTx.ID] = multiTx
	return nil
}

func (s *InMemMultiTransactionStorage) DeleteMultiTransaction(id common.MultiTransactionIDType) error {
	delete(s.storage, id)
	return nil
}

func (s *InMemMultiTransactionStorage) ReadMultiTransactions(details *MultiTxDetails) ([]*MultiTransaction, error) {
	var multiTxs []*MultiTransaction
	for _, multiTx := range s.storage {
		if len(details.IDs) > 0 && !testutils.SliceContains(details.IDs, multiTx.ID) {
			continue
		}

		if (details.AnyAddress != eth_common.Address{}) &&
			(multiTx.FromAddress != details.AnyAddress && multiTx.ToAddress != details.AnyAddress) {
			continue
		}

		if (details.FromAddress != eth_common.Address{}) && multiTx.FromAddress != details.FromAddress {
			continue
		}

		if (details.ToAddress != eth_common.Address{}) && multiTx.ToAddress != details.ToAddress {
			continue
		}

		if details.ToChainID != 0 && multiTx.ToNetworkID != details.ToChainID {
			continue
		}

		if details.Type != MultiTransactionDBTypeInvalid && multiTx.Type != mtDBTypeToMTType(details.Type) {
			continue
		}

		if details.CrossTxID != "" && multiTx.CrossTxID != details.CrossTxID {
			continue
		}

		multiTxs = append(multiTxs, multiTx)
	}
	return multiTxs, nil
}
