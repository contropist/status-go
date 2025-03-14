// Code generated by MockGen. DO NOT EDIT.
// Source: transactions/transactor.go

// Package mock_transactor is a generated GoMock package.
package mock_transactor

import (
	big "math/big"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"

	common "github.com/ethereum/go-ethereum/common"
	types "github.com/ethereum/go-ethereum/core/types"
	account "github.com/status-im/status-go/account"
	types0 "github.com/status-im/status-go/eth-node/types"
	params "github.com/status-im/status-go/params"
	rpc "github.com/status-im/status-go/rpc"
	common0 "github.com/status-im/status-go/services/wallet/common"
	transactions "github.com/status-im/status-go/transactions"
)

// MockTransactorIface is a mock of TransactorIface interface.
type MockTransactorIface struct {
	ctrl     *gomock.Controller
	recorder *MockTransactorIfaceMockRecorder
}

// MockTransactorIfaceMockRecorder is the mock recorder for MockTransactorIface.
type MockTransactorIfaceMockRecorder struct {
	mock *MockTransactorIface
}

// NewMockTransactorIface creates a new mock instance.
func NewMockTransactorIface(ctrl *gomock.Controller) *MockTransactorIface {
	mock := &MockTransactorIface{ctrl: ctrl}
	mock.recorder = &MockTransactorIfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockTransactorIface) EXPECT() *MockTransactorIfaceMockRecorder {
	return m.recorder
}

// AddSignatureToTransaction mocks base method.
func (m *MockTransactorIface) AddSignatureToTransaction(chainID uint64, tx *types.Transaction, sig []byte) (*types.Transaction, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AddSignatureToTransaction", chainID, tx, sig)
	ret0, _ := ret[0].(*types.Transaction)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// AddSignatureToTransaction indicates an expected call of AddSignatureToTransaction.
func (mr *MockTransactorIfaceMockRecorder) AddSignatureToTransaction(chainID, tx, sig interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddSignatureToTransaction", reflect.TypeOf((*MockTransactorIface)(nil).AddSignatureToTransaction), chainID, tx, sig)
}

// BuildTransactionWithSignature mocks base method.
func (m *MockTransactorIface) BuildTransactionWithSignature(chainID uint64, args transactions.SendTxArgs, sig []byte) (*types.Transaction, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BuildTransactionWithSignature", chainID, args, sig)
	ret0, _ := ret[0].(*types.Transaction)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// BuildTransactionWithSignature indicates an expected call of BuildTransactionWithSignature.
func (mr *MockTransactorIfaceMockRecorder) BuildTransactionWithSignature(chainID, args, sig interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BuildTransactionWithSignature", reflect.TypeOf((*MockTransactorIface)(nil).BuildTransactionWithSignature), chainID, args, sig)
}

// EstimateGas mocks base method.
func (m *MockTransactorIface) EstimateGas(network *params.Network, from, to common.Address, value *big.Int, input []byte) (uint64, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "EstimateGas", network, from, to, value, input)
	ret0, _ := ret[0].(uint64)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// EstimateGas indicates an expected call of EstimateGas.
func (mr *MockTransactorIfaceMockRecorder) EstimateGas(network, from, to, value, input interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "EstimateGas", reflect.TypeOf((*MockTransactorIface)(nil).EstimateGas), network, from, to, value, input)
}

// NextNonce mocks base method.
func (m *MockTransactorIface) NextNonce(rpcClient rpc.ClientInterface, chainID uint64, from types0.Address) (uint64, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "NextNonce", rpcClient, chainID, from)
	ret0, _ := ret[0].(uint64)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// NextNonce indicates an expected call of NextNonce.
func (mr *MockTransactorIfaceMockRecorder) NextNonce(rpcClient, chainID, from interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "NextNonce", reflect.TypeOf((*MockTransactorIface)(nil).NextNonce), rpcClient, chainID, from)
}

// SendRawTransaction mocks base method.
func (m *MockTransactorIface) SendRawTransaction(chainID uint64, rawTx string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SendRawTransaction", chainID, rawTx)
	ret0, _ := ret[0].(error)
	return ret0
}

// SendRawTransaction indicates an expected call of SendRawTransaction.
func (mr *MockTransactorIfaceMockRecorder) SendRawTransaction(chainID, rawTx interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SendRawTransaction", reflect.TypeOf((*MockTransactorIface)(nil).SendRawTransaction), chainID, rawTx)
}

// SendTransaction mocks base method.
func (m *MockTransactorIface) SendTransaction(sendArgs transactions.SendTxArgs, verifiedAccount *account.SelectedExtKey, lastUsedNonce int64) (types0.Hash, uint64, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SendTransaction", sendArgs, verifiedAccount, lastUsedNonce)
	ret0, _ := ret[0].(types0.Hash)
	ret1, _ := ret[1].(uint64)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// SendTransaction indicates an expected call of SendTransaction.
func (mr *MockTransactorIfaceMockRecorder) SendTransaction(sendArgs, verifiedAccount, lastUsedNonce interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SendTransaction", reflect.TypeOf((*MockTransactorIface)(nil).SendTransaction), sendArgs, verifiedAccount, lastUsedNonce)
}

// SendTransactionWithChainID mocks base method.
func (m *MockTransactorIface) SendTransactionWithChainID(chainID uint64, sendArgs transactions.SendTxArgs, lastUsedNonce int64, verifiedAccount *account.SelectedExtKey) (types0.Hash, uint64, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SendTransactionWithChainID", chainID, sendArgs, lastUsedNonce, verifiedAccount)
	ret0, _ := ret[0].(types0.Hash)
	ret1, _ := ret[1].(uint64)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// SendTransactionWithChainID indicates an expected call of SendTransactionWithChainID.
func (mr *MockTransactorIfaceMockRecorder) SendTransactionWithChainID(chainID, sendArgs, lastUsedNonce, verifiedAccount interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SendTransactionWithChainID", reflect.TypeOf((*MockTransactorIface)(nil).SendTransactionWithChainID), chainID, sendArgs, lastUsedNonce, verifiedAccount)
}

// SendTransactionWithSignature mocks base method.
func (m *MockTransactorIface) SendTransactionWithSignature(from common.Address, symbol string, multiTransactionID common0.MultiTransactionIDType, tx *types.Transaction) (types0.Hash, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SendTransactionWithSignature", from, symbol, multiTransactionID, tx)
	ret0, _ := ret[0].(types0.Hash)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// SendTransactionWithSignature indicates an expected call of SendTransactionWithSignature.
func (mr *MockTransactorIfaceMockRecorder) SendTransactionWithSignature(from, symbol, multiTransactionID, tx interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SendTransactionWithSignature", reflect.TypeOf((*MockTransactorIface)(nil).SendTransactionWithSignature), from, symbol, multiTransactionID, tx)
}

// StoreAndTrackPendingTx mocks base method.
func (m *MockTransactorIface) StoreAndTrackPendingTx(from common.Address, symbol string, chainID uint64, multiTransactionID common0.MultiTransactionIDType, tx *types.Transaction) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "StoreAndTrackPendingTx", from, symbol, chainID, multiTransactionID, tx)
	ret0, _ := ret[0].(error)
	return ret0
}

// StoreAndTrackPendingTx indicates an expected call of StoreAndTrackPendingTx.
func (mr *MockTransactorIfaceMockRecorder) StoreAndTrackPendingTx(from, symbol, chainID, multiTransactionID, tx interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "StoreAndTrackPendingTx", reflect.TypeOf((*MockTransactorIface)(nil).StoreAndTrackPendingTx), from, symbol, chainID, multiTransactionID, tx)
}

// ValidateAndBuildTransaction mocks base method.
func (m *MockTransactorIface) ValidateAndBuildTransaction(chainID uint64, sendArgs transactions.SendTxArgs, lastUsedNonce int64) (*types.Transaction, uint64, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ValidateAndBuildTransaction", chainID, sendArgs, lastUsedNonce)
	ret0, _ := ret[0].(*types.Transaction)
	ret1, _ := ret[1].(uint64)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// ValidateAndBuildTransaction indicates an expected call of ValidateAndBuildTransaction.
func (mr *MockTransactorIfaceMockRecorder) ValidateAndBuildTransaction(chainID, sendArgs, lastUsedNonce interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ValidateAndBuildTransaction", reflect.TypeOf((*MockTransactorIface)(nil).ValidateAndBuildTransaction), chainID, sendArgs, lastUsedNonce)
}
