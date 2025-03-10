package commands

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/status-im/status-go/eth-node/types"
	"github.com/status-im/status-go/signal"
)

func preparePersonalSignRequest(dApp signal.ConnectorDApp, challenge, address string) (RPCRequest, error) {
	params := map[string]interface{}{
		"challenge": challenge,
		"address":   address,
	}

	return ConstructRPCRequest("personal_sign", []interface{}{params}, &dApp)
}

func TestFailToPersonalSignWithMissingDAppFields(t *testing.T) {
	db, close := SetupTestDB(t)
	defer close()

	cmd := &PersonalSignCommand{Db: db}

	// Missing DApp fields
	request, err := ConstructRPCRequest("personal_sign", []interface{}{}, nil)
	assert.NoError(t, err)

	result, err := cmd.Execute(request)
	assert.Equal(t, ErrRequestMissingDAppData, err)
	assert.Empty(t, result)
}

func TestFailToPersonalSignForUnpermittedDApp(t *testing.T) {
	db, close := SetupTestDB(t)
	defer close()

	cmd := &PersonalSignCommand{Db: db}

	request, err := preparePersonalSignRequest(testDAppData,
		"0x506c65617365207369676e2074686973206d65737361676520746f20636f6e6669726d20796f7572206964656e746974792e",
		"0x4B0897b0513FdBeEc7C469D9aF4fA6C0752aBea7",
	)
	assert.NoError(t, err)

	result, err := cmd.Execute(request)
	assert.Equal(t, ErrDAppIsNotPermittedByUser, err)
	assert.Empty(t, result)
}

func TestFailToPersonalSignWithoutParams(t *testing.T) {
	db, close := SetupTestDB(t)
	defer close()

	cmd := &PersonalSignCommand{Db: db}

	request, err := ConstructRPCRequest("personal_sign", nil, &testDAppData)
	assert.NoError(t, err)

	result, err := cmd.Execute(request)
	assert.Equal(t, ErrEmptyRPCParams, err)
	assert.Empty(t, result)
}

func TestFailToPersonalSignWithSignalTimout(t *testing.T) {
	db, close := SetupTestDB(t)
	defer close()

	clientHandler := NewClientSideHandler()

	cmd := &PersonalSignCommand{
		Db:            db,
		ClientHandler: clientHandler,
	}

	err := PersistDAppData(db, testDAppData, types.Address{0x01}, uint64(0x1))
	assert.NoError(t, err)

	request, err := preparePersonalSignRequest(testDAppData,
		"0x506c65617365207369676e2074686973206d65737361676520746f20636f6e6669726d20796f7572206964656e746974792e",
		"0x4B0897b0513FdBeEc7C469D9aF4fA6C0752aBea7",
	)
	assert.NoError(t, err)

	backupWalletResponseMaxInterval := WalletResponseMaxInterval
	WalletResponseMaxInterval = 1 * time.Millisecond

	_, err = cmd.Execute(request)
	assert.Equal(t, ErrWalletResponseTimeout, err)
	WalletResponseMaxInterval = backupWalletResponseMaxInterval
}

func TestPersonalSignWithSignalAccepted(t *testing.T) {
	db, close := SetupTestDB(t)
	defer close()

	fakedSignature := "0x051"

	clientHandler := NewClientSideHandler()

	cmd := &PersonalSignCommand{
		Db:            db,
		ClientHandler: clientHandler,
	}

	err := PersistDAppData(db, testDAppData, types.Address{0x01}, uint64(0x1))
	assert.NoError(t, err)

	challenge := "0x506c65617365207369676e2074686973206d65737361676520746f20636f6e6669726d20796f7572206964656e746974792e"
	address := "0x4B0897b0513FdBeEc7C469D9aF4fA6C0752aBea7"
	request, err := preparePersonalSignRequest(testDAppData, challenge, address)
	assert.NoError(t, err)

	signal.SetMobileSignalHandler(signal.MobileSignalHandler(func(s []byte) {
		var evt EventType
		err := json.Unmarshal(s, &evt)
		assert.NoError(t, err)

		switch evt.Type {
		case signal.EventConnectorPersonalSign:
			var ev signal.ConnectorPersonalSignSignal
			err := json.Unmarshal(evt.Event, &ev)
			assert.NoError(t, err)
			assert.Equal(t, ev.Challenge, challenge)
			assert.Equal(t, ev.Address, address)

			err = clientHandler.PersonalSignAccepted(PersonalSignAcceptedArgs{
				Signature: fakedSignature,
				RequestID: ev.RequestID,
			})
			assert.NoError(t, err)
		}
	}))

	response, err := cmd.Execute(request)
	assert.NoError(t, err)
	assert.Equal(t, response, fakedSignature)
}

func TestPersonalSignWithSignalRejected(t *testing.T) {
	db, close := SetupTestDB(t)
	defer close()

	clientHandler := NewClientSideHandler()

	cmd := &PersonalSignCommand{
		Db:            db,
		ClientHandler: clientHandler,
	}

	err := PersistDAppData(db, testDAppData, types.Address{0x01}, uint64(0x1))
	assert.NoError(t, err)

	challenge := "0x506c65617365207369676e2074686973206d65737361676520746f20636f6e6669726d20796f7572206964656e746974792e"
	address := "0x4B0897b0513FdBeEc7C469D9aF4fA6C0752aBea7"
	request, err := preparePersonalSignRequest(testDAppData, challenge, address)
	assert.NoError(t, err)

	signal.SetMobileSignalHandler(signal.MobileSignalHandler(func(s []byte) {
		var evt EventType
		err := json.Unmarshal(s, &evt)
		assert.NoError(t, err)

		switch evt.Type {
		case signal.EventConnectorPersonalSign:
			var ev signal.ConnectorPersonalSignSignal
			err := json.Unmarshal(evt.Event, &ev)
			assert.NoError(t, err)

			err = clientHandler.PersonalSignRejected(RejectedArgs{
				RequestID: ev.RequestID,
			})
			assert.NoError(t, err)
		}
	}))

	_, err = cmd.Execute(request)
	assert.Equal(t, ErrPersonalSignRejectedByUser, err)
}
