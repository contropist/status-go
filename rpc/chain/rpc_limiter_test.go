package chain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func setupTest() (*InMemRequestsMapStorage, RequestLimiter) {
	storage := NewInMemRequestsMapStorage()
	rl := NewRequestLimiter(storage)
	return storage, rl
}

func TestSetLimit(t *testing.T) {
	storage, rl := setupTest()

	// Define test inputs
	tag := "testTag"
	maxRequests := 10
	interval := time.Second

	// Call the SetLimit method
	err := rl.SetLimit(tag, maxRequests, interval)
	require.NoError(t, err)

	// Verify that the data was saved to storage correctly
	data, err := storage.Get(tag)
	require.NoError(t, err)
	require.Equal(t, tag, data.Tag)
	require.Equal(t, interval, data.Period)
	require.Equal(t, maxRequests, data.MaxReqs)
	require.Equal(t, 0, data.NumReqs)
}

func TestGetLimit(t *testing.T) {
	storage, rl := setupTest()

	// Define test inputs
	data := &LimitData{
		Tag:     "testTag",
		Period:  time.Second,
		MaxReqs: 10,
		NumReqs: 1,
	}
	err := storage.Set(data)
	require.NoError(t, err)

	// Call the GetLimit method
	ret, err := rl.GetLimit(data.Tag)
	require.NoError(t, err)

	// Verify the returned data
	require.Equal(t, data, ret)
}

func TestDeleteLimit(t *testing.T) {
	storage, rl := setupTest()

	// Define test inputs
	tag := "testTag"
	data := &LimitData{
		Tag:     tag,
		Period:  time.Second,
		MaxReqs: 10,
		NumReqs: 1,
	}
	err := storage.Set(data)
	require.NoError(t, err)

	// Call the DeleteLimit method
	err = rl.DeleteLimit(tag)
	require.NoError(t, err)

	// Verify that the data was deleted from storage
	limit, _ := storage.Get(tag)
	require.Nil(t, limit)

	// Test double delete
	err = rl.DeleteLimit(tag)
	require.NoError(t, err)
}

func TestAllowWithinPeriod(t *testing.T) {
	storage, rl := setupTest()

	// Define test inputs
	tag := "testTag"
	maxRequests := 10
	interval := time.Second

	// Set up the storage with test data
	data := &LimitData{
		Tag:       tag,
		Period:    interval,
		CreatedAt: time.Now(),
		MaxReqs:   maxRequests,
	}
	err := storage.Set(data)
	require.NoError(t, err)

	// Call the Allow method
	for i := 0; i < maxRequests; i++ {
		allow, err := rl.Allow(tag)
		require.NoError(t, err)

		// Verify the result
		require.True(t, allow)
	}

	// Call the Allow method again
	allow, err := rl.Allow(tag)
	require.ErrorIs(t, err, ErrRequestsOverLimit)
	require.False(t, allow)
}

func TestAllowWhenPeriodPassed(t *testing.T) {
	storage, rl := setupTest()

	// Define test inputs
	tag := "testTag"
	maxRequests := 10
	interval := time.Second

	// Set up the storage with test data
	data := &LimitData{
		Tag:       tag,
		Period:    interval,
		CreatedAt: time.Now().Add(-interval),
		MaxReqs:   maxRequests,
		NumReqs:   maxRequests,
	}
	err := storage.Set(data)
	require.NoError(t, err)

	// Call the Allow method
	allow, err := rl.Allow(tag)
	require.NoError(t, err)

	// Verify the result
	require.True(t, allow)
}

func TestAllowRestrictInfinitelyWhenLimitReached(t *testing.T) {
	storage, rl := setupTest()

	// Define test inputs
	tag := "testTag"
	maxRequests := 10

	// Set up the storage with test data
	data := &LimitData{
		Tag:       tag,
		Period:    LimitInfinitely,
		CreatedAt: time.Now(),
		MaxReqs:   maxRequests,
		NumReqs:   maxRequests,
	}
	err := storage.Set(data)
	require.NoError(t, err)

	// Call the Allow method
	allow, err := rl.Allow(tag)
	require.ErrorIs(t, err, ErrRequestsOverLimit)

	// Verify the result
	require.False(t, allow)
}

func TestAllowWhenLimitNotReachedForInfinitePeriod(t *testing.T) {
	storage, rl := setupTest()

	// Define test inputs
	tag := "testTag"
	maxRequests := 10

	// Set up the storage with test data
	data := &LimitData{
		Tag:       tag,
		Period:    LimitInfinitely,
		CreatedAt: time.Now(),
		MaxReqs:   maxRequests,
		NumReqs:   maxRequests - 1,
	}
	err := storage.Set(data)
	require.NoError(t, err)

	// Call the Allow method
	allow, err := rl.Allow(tag)
	require.NoError(t, err)

	// Verify the result
	require.True(t, allow)
}
