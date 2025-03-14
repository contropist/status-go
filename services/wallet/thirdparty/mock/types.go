// Code generated by MockGen. DO NOT EDIT.
// Source: services/wallet/thirdparty/types.go

// Package mock_thirdparty is a generated GoMock package.
package mock_thirdparty

import (
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"

	thirdparty "github.com/status-im/status-go/services/wallet/thirdparty"
)

// MockMarketDataProvider is a mock of MarketDataProvider interface.
type MockMarketDataProvider struct {
	ctrl     *gomock.Controller
	recorder *MockMarketDataProviderMockRecorder
}

// MockMarketDataProviderMockRecorder is the mock recorder for MockMarketDataProvider.
type MockMarketDataProviderMockRecorder struct {
	mock *MockMarketDataProvider
}

// NewMockMarketDataProvider creates a new mock instance.
func NewMockMarketDataProvider(ctrl *gomock.Controller) *MockMarketDataProvider {
	mock := &MockMarketDataProvider{ctrl: ctrl}
	mock.recorder = &MockMarketDataProviderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockMarketDataProvider) EXPECT() *MockMarketDataProviderMockRecorder {
	return m.recorder
}

// FetchHistoricalDailyPrices mocks base method.
func (m *MockMarketDataProvider) FetchHistoricalDailyPrices(symbol, currency string, limit int, allData bool, aggregate int) ([]thirdparty.HistoricalPrice, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "FetchHistoricalDailyPrices", symbol, currency, limit, allData, aggregate)
	ret0, _ := ret[0].([]thirdparty.HistoricalPrice)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// FetchHistoricalDailyPrices indicates an expected call of FetchHistoricalDailyPrices.
func (mr *MockMarketDataProviderMockRecorder) FetchHistoricalDailyPrices(symbol, currency, limit, allData, aggregate interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "FetchHistoricalDailyPrices", reflect.TypeOf((*MockMarketDataProvider)(nil).FetchHistoricalDailyPrices), symbol, currency, limit, allData, aggregate)
}

// FetchHistoricalHourlyPrices mocks base method.
func (m *MockMarketDataProvider) FetchHistoricalHourlyPrices(symbol, currency string, limit, aggregate int) ([]thirdparty.HistoricalPrice, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "FetchHistoricalHourlyPrices", symbol, currency, limit, aggregate)
	ret0, _ := ret[0].([]thirdparty.HistoricalPrice)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// FetchHistoricalHourlyPrices indicates an expected call of FetchHistoricalHourlyPrices.
func (mr *MockMarketDataProviderMockRecorder) FetchHistoricalHourlyPrices(symbol, currency, limit, aggregate interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "FetchHistoricalHourlyPrices", reflect.TypeOf((*MockMarketDataProvider)(nil).FetchHistoricalHourlyPrices), symbol, currency, limit, aggregate)
}

// FetchPrices mocks base method.
func (m *MockMarketDataProvider) FetchPrices(symbols, currencies []string) (map[string]map[string]float64, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "FetchPrices", symbols, currencies)
	ret0, _ := ret[0].(map[string]map[string]float64)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// FetchPrices indicates an expected call of FetchPrices.
func (mr *MockMarketDataProviderMockRecorder) FetchPrices(symbols, currencies interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "FetchPrices", reflect.TypeOf((*MockMarketDataProvider)(nil).FetchPrices), symbols, currencies)
}

// FetchTokenDetails mocks base method.
func (m *MockMarketDataProvider) FetchTokenDetails(symbols []string) (map[string]thirdparty.TokenDetails, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "FetchTokenDetails", symbols)
	ret0, _ := ret[0].(map[string]thirdparty.TokenDetails)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// FetchTokenDetails indicates an expected call of FetchTokenDetails.
func (mr *MockMarketDataProviderMockRecorder) FetchTokenDetails(symbols interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "FetchTokenDetails", reflect.TypeOf((*MockMarketDataProvider)(nil).FetchTokenDetails), symbols)
}

// FetchTokenMarketValues mocks base method.
func (m *MockMarketDataProvider) FetchTokenMarketValues(symbols []string, currency string) (map[string]thirdparty.TokenMarketValues, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "FetchTokenMarketValues", symbols, currency)
	ret0, _ := ret[0].(map[string]thirdparty.TokenMarketValues)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// FetchTokenMarketValues indicates an expected call of FetchTokenMarketValues.
func (mr *MockMarketDataProviderMockRecorder) FetchTokenMarketValues(symbols, currency interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "FetchTokenMarketValues", reflect.TypeOf((*MockMarketDataProvider)(nil).FetchTokenMarketValues), symbols, currency)
}

// ID mocks base method.
func (m *MockMarketDataProvider) ID() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ID")
	ret0, _ := ret[0].(string)
	return ret0
}

// ID indicates an expected call of ID.
func (mr *MockMarketDataProviderMockRecorder) ID() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ID", reflect.TypeOf((*MockMarketDataProvider)(nil).ID))
}

// MockDecoderProvider is a mock of DecoderProvider interface.
type MockDecoderProvider struct {
	ctrl     *gomock.Controller
	recorder *MockDecoderProviderMockRecorder
}

// MockDecoderProviderMockRecorder is the mock recorder for MockDecoderProvider.
type MockDecoderProviderMockRecorder struct {
	mock *MockDecoderProvider
}

// NewMockDecoderProvider creates a new mock instance.
func NewMockDecoderProvider(ctrl *gomock.Controller) *MockDecoderProvider {
	mock := &MockDecoderProvider{ctrl: ctrl}
	mock.recorder = &MockDecoderProviderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockDecoderProvider) EXPECT() *MockDecoderProviderMockRecorder {
	return m.recorder
}

// Run mocks base method.
func (m *MockDecoderProvider) Run(data string) (*thirdparty.DataParsed, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Run", data)
	ret0, _ := ret[0].(*thirdparty.DataParsed)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Run indicates an expected call of Run.
func (mr *MockDecoderProviderMockRecorder) Run(data interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Run", reflect.TypeOf((*MockDecoderProvider)(nil).Run), data)
}
