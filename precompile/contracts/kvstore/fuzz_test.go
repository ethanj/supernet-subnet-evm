// (c) 2024, SuperNet Labs. All rights reserved.
// See the file LICENSE for licensing terms.

package kvstore

import (
	"math/big"
	"math/rand"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/core/extstate"
	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/subnet-evm/utils"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// Helper function to create a mock accessible state for fuzz testing
func newMockAccessibleStateForFuzz() contract.AccessibleState {
	ctrl := gomock.NewController(&testing.T{})
	state := extstate.NewTestStateDB(&testing.T{})

	mockChainConfig := precompileconfig.NewMockChainConfig(ctrl)
	mockChainConfig.EXPECT().GetFeeConfig().AnyTimes().Return(commontype.ValidTestFeeConfig)
	mockChainConfig.EXPECT().AllowedFeeRecipients().AnyTimes().Return(false)
	mockChainConfig.EXPECT().IsDurango(gomock.Any()).AnyTimes().Return(true)

	blockContext := contract.NewMockBlockContext(ctrl)
	blockContext.EXPECT().Number().Return(big.NewInt(0)).AnyTimes()
	blockContext.EXPECT().Timestamp().Return(uint64(time.Now().Unix())).AnyTimes()

	snowContext := utils.TestSnowContext()

	accessibleState := contract.NewMockAccessibleState(ctrl)
	accessibleState.EXPECT().GetStateDB().Return(state).AnyTimes()
	accessibleState.EXPECT().GetBlockContext().Return(blockContext).AnyTimes()
	accessibleState.EXPECT().GetSnowContext().Return(snowContext).AnyTimes()
	accessibleState.EXPECT().GetChainConfig().Return(mockChainConfig).AnyTimes()

	return accessibleState
}

func FuzzKVStore_SetGet(f *testing.F) {
	// Add seed corpus
	f.Add([]byte("test key"), []byte("test value"))
	f.Add([]byte(""), []byte(""))
	f.Add([]byte("a"), []byte("b"))

	f.Fuzz(func(t *testing.T, keyBytes, valueBytes []byte) {
		// Skip if key is not 32 bytes (required for bytes32)
		if len(keyBytes) != 32 {
			t.Skip("Key must be exactly 32 bytes")
		}

		// Skip if value is too large
		if len(valueBytes) > MaxValueSize {
			t.Skip("Value too large")
		}

		state := newMockAccessibleStateForFuzz()
		key := common.BytesToHash(keyBytes)

		// Test set operation
		setInput, err := KVStoreABI.Pack("set", key, valueBytes)
		require.NoError(t, err)

		ret, _, err := setKV(state, caller, testAddr, setInput, 1000000, false)
		require.NoError(t, err)

		var success bool
		err = KVStoreABI.UnpackIntoInterface(&success, "set", ret)
		require.NoError(t, err)
		require.True(t, success)

		// Test get operation
		getInput, err := KVStoreABI.Pack("get", key)
		require.NoError(t, err)

		ret, _, err = getKV(state, caller, testAddr, getInput, 1000000, false)
		require.NoError(t, err)

		var retrievedValue []byte
		err = KVStoreABI.UnpackIntoInterface(&retrievedValue, "get", ret)
		require.NoError(t, err)
		require.Equal(t, valueBytes, retrievedValue)

		// Test exists operation
		existsInput, err := KVStoreABI.Pack("exists", key)
		require.NoError(t, err)

		ret, _, err = existsKV(state, caller, testAddr, existsInput, 1000000, false)
		require.NoError(t, err)

		var exists bool
		err = KVStoreABI.UnpackIntoInterface(&exists, "exists", ret)
		require.NoError(t, err)
		require.True(t, exists)
	})
}

func FuzzKVStore_Delete(f *testing.F) {
	// Add seed corpus
	f.Add([]byte("test key for deletion"))
	f.Add([]byte("another test key"))

	f.Fuzz(func(t *testing.T, keyBytes []byte) {
		// Skip if key is not 32 bytes
		if len(keyBytes) != 32 {
			t.Skip("Key must be exactly 32 bytes")
		}

		state := newMockAccessibleStateForFuzz()
		key := common.BytesToHash(keyBytes)
		testValue := []byte("test value for deletion")

		// First set a value
		setInput, err := KVStoreABI.Pack("set", key, testValue)
		require.NoError(t, err)

		_, _, err = setKV(state, caller, testAddr, setInput, 1000000, false)
		require.NoError(t, err)

		// Verify it exists
		existsInput, err := KVStoreABI.Pack("exists", key)
		require.NoError(t, err)

		ret, _, err := existsKV(state, caller, testAddr, existsInput, 1000000, false)
		require.NoError(t, err)

		var exists bool
		err = KVStoreABI.UnpackIntoInterface(&exists, "exists", ret)
		require.NoError(t, err)
		require.True(t, exists)

		// Delete the key
		deleteInput, err := KVStoreABI.Pack("delete", key)
		require.NoError(t, err)

		ret, _, err = deleteKV(state, caller, testAddr, deleteInput, 1000000, false)
		require.NoError(t, err)

		var success bool
		err = KVStoreABI.UnpackIntoInterface(&success, "delete", ret)
		require.NoError(t, err)
		require.True(t, success)

		// Verify it no longer exists
		ret, _, err = existsKV(state, caller, testAddr, existsInput, 1000000, false)
		require.NoError(t, err)

		err = KVStoreABI.UnpackIntoInterface(&exists, "exists", ret)
		require.NoError(t, err)
		require.False(t, exists)

		// Verify get returns empty
		getInput, err := KVStoreABI.Pack("get", key)
		require.NoError(t, err)

		ret, _, err = getKV(state, caller, testAddr, getInput, 1000000, false)
		require.NoError(t, err)

		var retrievedValue []byte
		err = KVStoreABI.UnpackIntoInterface(&retrievedValue, "get", ret)
		require.NoError(t, err)
		require.Empty(t, retrievedValue)
	})
}

func FuzzKVStore_MultipleOperations(f *testing.F) {
	// Add seed corpus
	f.Add(uint64(42), uint64(100))
	f.Add(uint64(1), uint64(10))
	f.Add(uint64(0), uint64(5))

	f.Fuzz(func(t *testing.T, seed, numOps uint64) {
		if numOps > 50 { // Limit operations to prevent timeout
			t.Skip("Too many operations")
		}

		state := newMockAccessibleStateForFuzz()
		rng := rand.New(rand.NewSource(int64(seed)))

		keys := make([]common.Hash, 0)
		values := make(map[common.Hash][]byte)

		for i := uint64(0); i < numOps; i++ {
			operation := rng.Intn(4) // 0=set, 1=get, 2=exists, 3=delete

			var key common.Hash
			if len(keys) > 0 && rng.Intn(2) == 0 {
				// Use existing key
				key = keys[rng.Intn(len(keys))]
			} else {
				// Generate new key
				keyBytes := make([]byte, 32)
				rng.Read(keyBytes)
				key = common.BytesToHash(keyBytes)
			}

			switch operation {
			case 0: // set
				valueSize := rng.Intn(1000) // Random value size up to 1KB
				value := make([]byte, valueSize)
				rng.Read(value)

				setInput, err := KVStoreABI.Pack("set", key, value)
				require.NoError(t, err)

				ret, _, err := setKV(state, caller, testAddr, setInput, 1000000, false)
				require.NoError(t, err)

				var success bool
				err = KVStoreABI.UnpackIntoInterface(&success, "set", ret)
				require.NoError(t, err)
				require.True(t, success)

				// Track the key and value
				if _, exists := values[key]; !exists {
					keys = append(keys, key)
				}
				values[key] = value

			case 1: // get
				getInput, err := KVStoreABI.Pack("get", key)
				require.NoError(t, err)

				ret, _, err := getKV(state, caller, testAddr, getInput, 1000000, false)
				require.NoError(t, err)

				var retrievedValue []byte
				err = KVStoreABI.UnpackIntoInterface(&retrievedValue, "get", ret)
				require.NoError(t, err)

				// Verify the retrieved value matches expected
				expectedValue, exists := values[key]
				if exists {
					require.Equal(t, expectedValue, retrievedValue)
				} else {
					require.Empty(t, retrievedValue)
				}

			case 2: // exists
				existsInput, err := KVStoreABI.Pack("exists", key)
				require.NoError(t, err)

				ret, _, err := existsKV(state, caller, testAddr, existsInput, 1000000, false)
				require.NoError(t, err)

				var exists bool
				err = KVStoreABI.UnpackIntoInterface(&exists, "exists", ret)
				require.NoError(t, err)

				// Verify existence matches expectations
				_, expectedExists := values[key]
				require.Equal(t, expectedExists, exists)

			case 3: // delete
				deleteInput, err := KVStoreABI.Pack("delete", key)
				require.NoError(t, err)

				ret, _, err := deleteKV(state, caller, testAddr, deleteInput, 1000000, false)
				require.NoError(t, err)

				var success bool
				err = KVStoreABI.UnpackIntoInterface(&success, "delete", ret)
				require.NoError(t, err)

				// Verify success matches expected existence
				_, expectedExists := values[key]
				require.Equal(t, expectedExists, success)

				// Remove from tracking if it existed
				if expectedExists {
					delete(values, key)
					// Remove from keys slice
					for j, k := range keys {
						if k == key {
							keys = append(keys[:j], keys[j+1:]...)
							break
						}
					}
				}
			}
		}
	})
}

func FuzzKVStore_GasConsumption(f *testing.F) {
	// Add seed corpus for different value sizes
	f.Add(0)
	f.Add(100)
	f.Add(1000)
	f.Add(MaxValueSize)

	f.Fuzz(func(t *testing.T, valueSize int) {
		if valueSize < 0 || valueSize > MaxValueSize {
			t.Skip("Invalid value size")
		}

		state := newMockAccessibleStateForFuzz()

		// Generate test data
		key := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
		value := make([]byte, valueSize)
		for i := range value {
			value[i] = byte(i % 256)
		}

		// Test set gas consumption
		setInput, err := KVStoreABI.Pack("set", key, value)
		require.NoError(t, err)

		// Calculate adequate gas for the operation
		suppliedGas := SetGasCost + uint64(valueSize)*GasPerByte + 100000 // Add buffer
		ret, remainingGas, err := setKV(state, caller, testAddr, setInput, suppliedGas, false)
		require.NoError(t, err)

		var success bool
		err = KVStoreABI.UnpackIntoInterface(&success, "set", ret)
		require.NoError(t, err)
		require.True(t, success)

		// Verify gas consumption matches expected formula
		expectedGas := SetGasCost + uint64(valueSize)*GasPerByte
		actualGasUsed := suppliedGas - remainingGas
		require.Equal(t, expectedGas, actualGasUsed)

		// Test get gas consumption
		getInput, err := KVStoreABI.Pack("get", key)
		require.NoError(t, err)

		// Calculate adequate gas for get operation
		getSuppliedGas := GetGasCost + uint64(valueSize)*GasPerByte + 100000 // Add buffer
		ret, remainingGas, err = getKV(state, caller, testAddr, getInput, getSuppliedGas, false)
		require.NoError(t, err)

		var retrievedValue []byte
		err = KVStoreABI.UnpackIntoInterface(&retrievedValue, "get", ret)
		require.NoError(t, err)
		require.Equal(t, value, retrievedValue)

		// Verify get gas consumption
		expectedGetGas := GetGasCost + uint64(valueSize)*GasPerByte
		actualGetGasUsed := getSuppliedGas - remainingGas
		require.Equal(t, expectedGetGas, actualGetGasUsed)
	})
}
