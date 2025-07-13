// (c) 2024, SuperNet Labs. All rights reserved.
// See the file LICENSE for licensing terms.

package kvstore

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/core/extstate"
	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/subnet-evm/utils"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var (
	testKey1   = common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	testKey2   = common.HexToHash("0xfedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321")
	testValue1 = []byte("test value 1")
	testValue2 = []byte("test value 2")
	testAddr   = common.HexToAddress("0x1000000000000000000000000000000000000000")
	caller     = common.HexToAddress("0x2000000000000000000000000000000000000000")
)

// Helper function to create a mock accessible state for testing
func newMockAccessibleState() contract.AccessibleState {
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

func TestKVStore_Set(t *testing.T) {
	tests := []struct {
		name          string
		key           common.Hash
		value         []byte
		expectedGas   uint64
		expectSuccess bool
		readOnly      bool
	}{
		{
			name:          "successful set",
			key:           testKey1,
			value:         testValue1,
			expectedGas:   SetGasCost + uint64(len(testValue1))*GasPerByte,
			expectSuccess: true,
			readOnly:      false,
		},
		{
			name:          "set with empty value",
			key:           testKey2,
			value:         []byte{},
			expectedGas:   SetGasCost,
			expectSuccess: true,
			readOnly:      false,
		},
		{
			name:          "read only should fail",
			key:           testKey1,
			value:         testValue1,
			expectedGas:   0,
			expectSuccess: false,
			readOnly:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newMockAccessibleState()

			// Pack input
			input, err := KVStoreABI.Pack("set", tt.key, tt.value)
			require.NoError(t, err)

			suppliedGas := uint64(100000)
			ret, remainingGas, err := setKV(state, caller, testAddr, input, suppliedGas, tt.readOnly)

			if tt.expectSuccess {
				require.NoError(t, err)

				// Verify return value (should be true)
				var success bool
				err = KVStoreABI.UnpackIntoInterface(&success, "set", ret)
				require.NoError(t, err)
				require.True(t, success)

				// Verify gas consumption
				expectedRemainingGas := suppliedGas - tt.expectedGas
				require.Equal(t, expectedRemainingGas, remainingGas)

				// Verify state changes (only if not read-only)
				if !tt.readOnly {
					dataKey := getDataKey(tt.key)
					valueHash := state.GetStateDB().GetState(testAddr, dataKey)
					expectedHash := crypto.Keccak256Hash(tt.value)
					require.Equal(t, expectedHash, valueHash)
				}
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestKVStore_Get(t *testing.T) {
	state := newMockAccessibleState()

	// First set a value
	setInput, err := KVStoreABI.Pack("set", testKey1, testValue1)
	require.NoError(t, err)

	_, _, err = setKV(state, caller, testAddr, setInput, 100000, false)
	require.NoError(t, err)

	tests := []struct {
		name          string
		key           common.Hash
		expectedValue []byte
		expectSuccess bool
	}{
		{
			name:          "get existing key",
			key:           testKey1,
			expectedValue: testValue1,
			expectSuccess: true,
		},
		{
			name:          "get non-existent key",
			key:           testKey2,
			expectedValue: []byte{},
			expectSuccess: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Pack input
			input, err := KVStoreABI.Pack("get", tt.key)
			require.NoError(t, err)

			suppliedGas := uint64(100000)
			ret, remainingGas, err := getKV(state, caller, testAddr, input, suppliedGas, false)

			if tt.expectSuccess {
				require.NoError(t, err)

				// Verify return value
				var value []byte
				err = KVStoreABI.UnpackIntoInterface(&value, "get", ret)
				require.NoError(t, err)
				require.Equal(t, tt.expectedValue, value)

				// Verify gas consumption
				expectedGas := GetGasCost + uint64(len(tt.expectedValue))*GasPerByte
				expectedRemainingGas := suppliedGas - expectedGas
				require.Equal(t, expectedRemainingGas, remainingGas)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestKVStore_Exists(t *testing.T) {
	state := newMockAccessibleState()

	// First set a value
	setInput, err := KVStoreABI.Pack("set", testKey1, testValue1)
	require.NoError(t, err)

	_, _, err = setKV(state, caller, testAddr, setInput, 100000, false)
	require.NoError(t, err)

	tests := []struct {
		name           string
		key            common.Hash
		expectedExists bool
	}{
		{
			name:           "existing key",
			key:            testKey1,
			expectedExists: true,
		},
		{
			name:           "non-existent key",
			key:            testKey2,
			expectedExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Pack input
			input, err := KVStoreABI.Pack("exists", tt.key)
			require.NoError(t, err)

			suppliedGas := uint64(100000)
			ret, remainingGas, err := existsKV(state, caller, testAddr, input, suppliedGas, false)

			require.NoError(t, err)

			// Verify return value
			var exists bool
			err = KVStoreABI.UnpackIntoInterface(&exists, "exists", ret)
			require.NoError(t, err)
			require.Equal(t, tt.expectedExists, exists)

			// Verify gas consumption
			expectedRemainingGas := suppliedGas - ExistsGasCost
			require.Equal(t, expectedRemainingGas, remainingGas)
		})
	}
}

func TestKVStore_Delete(t *testing.T) {
	state := newMockAccessibleState()

	// First set a value
	setInput, err := KVStoreABI.Pack("set", testKey1, testValue1)
	require.NoError(t, err)

	_, _, err = setKV(state, caller, testAddr, setInput, 100000, false)
	require.NoError(t, err)

	tests := []struct {
		name            string
		key             common.Hash
		expectedSuccess bool
		readOnly        bool
	}{
		{
			name:            "delete existing key",
			key:             testKey1,
			expectedSuccess: true,
			readOnly:        false,
		},
		{
			name:            "delete non-existent key",
			key:             testKey2,
			expectedSuccess: false,
			readOnly:        false,
		},
		{
			name:            "delete in read-only mode",
			key:             testKey1,
			expectedSuccess: false,
			readOnly:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Pack input
			input, err := KVStoreABI.Pack("delete", tt.key)
			require.NoError(t, err)

			suppliedGas := uint64(100000)
			ret, remainingGas, err := deleteKV(state, caller, testAddr, input, suppliedGas, tt.readOnly)

			if tt.readOnly {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Verify return value
			var success bool
			err = KVStoreABI.UnpackIntoInterface(&success, "delete", ret)
			require.NoError(t, err)
			require.Equal(t, tt.expectedSuccess, success)

			// Verify gas consumption
			expectedRemainingGas := suppliedGas - DeleteGasCost
			require.Equal(t, expectedRemainingGas, remainingGas)

			// If deletion was successful, verify the key no longer exists
			if tt.expectedSuccess {
				dataKey := getDataKey(tt.key)
				valueHash := state.GetStateDB().GetState(testAddr, dataKey)
				require.Equal(t, common.Hash{}, valueHash)
			}
		})
	}
}

func TestKVStore_Keys(t *testing.T) {
	state := newMockAccessibleState()

	// Set multiple values
	keys := []common.Hash{testKey1, testKey2}
	values := [][]byte{testValue1, testValue2}

	for i, key := range keys {
		setInput, err := KVStoreABI.Pack("set", key, values[i])
		require.NoError(t, err)

		_, _, err = setKV(state, caller, testAddr, setInput, 100000, false)
		require.NoError(t, err)
	}

	// Test keys function
	input, err := KVStoreABI.Pack("keys")
	require.NoError(t, err)

	suppliedGas := uint64(100000)
	ret, remainingGas, err := keysKV(state, caller, testAddr, input, suppliedGas, false)

	require.NoError(t, err)

	// Verify return value
	var returnedKeys [][32]byte
	err = KVStoreABI.UnpackIntoInterface(&returnedKeys, "keys", ret)
	require.NoError(t, err)
	require.Len(t, returnedKeys, 2)

	// Verify gas consumption
	expectedGas := KeysBaseGasCost + uint64(len(returnedKeys))*KeysPerKeyGas
	expectedRemainingGas := suppliedGas - expectedGas
	require.Equal(t, expectedRemainingGas, remainingGas)
}

func TestKVStore_ValueSizeLimit(t *testing.T) {
	state := newMockAccessibleState()

	// Test with maximum allowed value size
	maxValue := make([]byte, MaxValueSize)
	for i := range maxValue {
		maxValue[i] = byte(i % 256)
	}

	setInput, err := KVStoreABI.Pack("set", testKey1, maxValue)
	require.NoError(t, err)

	_, _, err = setKV(state, caller, testAddr, setInput, 3000000, false)
	require.NoError(t, err)

	// Test with oversized value
	oversizedValue := make([]byte, MaxValueSize+1)
	setInput, err = KVStoreABI.Pack("set", testKey2, oversizedValue)
	require.NoError(t, err)

	_, _, err = setKV(state, caller, testAddr, setInput, 3000000, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "value exceeds maximum size")
}

func TestKVStore_GasLimits(t *testing.T) {
	state := newMockAccessibleState()

	// Test insufficient gas for set operation
	setInput, err := KVStoreABI.Pack("set", testKey1, testValue1)
	require.NoError(t, err)

	insufficientGas := uint64(SetGasCost - 1)
	_, _, err = setKV(state, caller, testAddr, setInput, insufficientGas, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of gas")

	// Test insufficient gas for exists operation
	existsInput, err := KVStoreABI.Pack("exists", testKey1)
	require.NoError(t, err)

	insufficientGas = uint64(ExistsGasCost - 1)
	_, _, err = existsKV(state, caller, testAddr, existsInput, insufficientGas, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of gas")
}

func TestKVStore_InvalidInputs(t *testing.T) {
	state := newMockAccessibleState()

	tests := []struct {
		name     string
		function func(contract.AccessibleState, common.Address, common.Address, []byte, uint64, bool) ([]byte, uint64, error)
		input    []byte
	}{
		{
			name:     "set with short input",
			function: setKV,
			input:    []byte{0x01, 0x02}, // too short
		},
		{
			name:     "get with wrong length",
			function: getKV,
			input:    []byte{0x01, 0x02}, // wrong length
		},
		{
			name:     "exists with wrong length",
			function: existsKV,
			input:    []byte{0x01, 0x02}, // wrong length
		},
		{
			name:     "delete with wrong length",
			function: deleteKV,
			input:    []byte{0x01, 0x02}, // wrong length
		},
		{
			name:     "keys with wrong length",
			function: keysKV,
			input:    []byte{0x01, 0x02}, // should be empty
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := tt.function(state, caller, testAddr, tt.input, 100000, false)
			require.Error(t, err)
		})
	}
}

func TestKVStore_Events(t *testing.T) {
	state := newMockAccessibleState()

	// Test KeySet event
	setInput, err := KVStoreABI.Pack("set", testKey1, testValue1)
	require.NoError(t, err)

	_, _, err = setKV(state, caller, testAddr, setInput, 100000, false)
	require.NoError(t, err)

	// Verify KeySet event was emitted
	logsTopics, logsData := state.GetStateDB().GetLogData()
	require.Len(t, logsTopics, 1)
	require.Len(t, logsData, 1)
	require.Equal(t, KVStoreABI.Events["KeySet"].ID, logsTopics[0][0])
	require.Equal(t, testKey1, logsTopics[0][1])

	// Test KeyDeleted event
	deleteInput, err := KVStoreABI.Pack("delete", testKey1)
	require.NoError(t, err)

	_, _, err = deleteKV(state, caller, testAddr, deleteInput, 100000, false)
	require.NoError(t, err)

	// Verify KeyDeleted event was emitted
	logsTopics, logsData = state.GetStateDB().GetLogData()
	require.Len(t, logsTopics, 2) // KeySet + KeyDeleted
	require.Len(t, logsData, 2)
	require.Equal(t, KVStoreABI.Events["KeyDeleted"].ID, logsTopics[1][0])
	require.Equal(t, testKey1, logsTopics[1][1])
}

func TestKVStore_CRUD_Integration(t *testing.T) {
	state := newMockAccessibleState()

	// Create
	setInput, err := KVStoreABI.Pack("set", testKey1, testValue1)
	require.NoError(t, err)

	ret, _, err := setKV(state, caller, testAddr, setInput, 100000, false)
	require.NoError(t, err)

	var success bool
	err = KVStoreABI.UnpackIntoInterface(&success, "set", ret)
	require.NoError(t, err)
	require.True(t, success)

	// Read
	getInput, err := KVStoreABI.Pack("get", testKey1)
	require.NoError(t, err)

	ret, _, err = getKV(state, caller, testAddr, getInput, 100000, false)
	require.NoError(t, err)

	var value []byte
	err = KVStoreABI.UnpackIntoInterface(&value, "get", ret)
	require.NoError(t, err)
	require.Equal(t, testValue1, value)

	// Update
	setInput, err = KVStoreABI.Pack("set", testKey1, testValue2)
	require.NoError(t, err)

	ret, _, err = setKV(state, caller, testAddr, setInput, 100000, false)
	require.NoError(t, err)

	err = KVStoreABI.UnpackIntoInterface(&success, "set", ret)
	require.NoError(t, err)
	require.True(t, success)

	// Verify update
	ret, _, err = getKV(state, caller, testAddr, getInput, 100000, false)
	require.NoError(t, err)

	err = KVStoreABI.UnpackIntoInterface(&value, "get", ret)
	require.NoError(t, err)
	require.Equal(t, testValue2, value)

	// Delete
	deleteInput, err := KVStoreABI.Pack("delete", testKey1)
	require.NoError(t, err)

	ret, _, err = deleteKV(state, caller, testAddr, deleteInput, 100000, false)
	require.NoError(t, err)

	err = KVStoreABI.UnpackIntoInterface(&success, "delete", ret)
	require.NoError(t, err)
	require.True(t, success)

	// Verify deletion
	ret, _, err = getKV(state, caller, testAddr, getInput, 100000, false)
	require.NoError(t, err)

	err = KVStoreABI.UnpackIntoInterface(&value, "get", ret)
	require.NoError(t, err)
	require.Empty(t, value)
}

func TestKVStore_KeyOverwrite(t *testing.T) {
	state := newMockAccessibleState()

	// Set initial value
	setInput, err := KVStoreABI.Pack("set", testKey1, testValue1)
	require.NoError(t, err)

	ret, _, err := setKV(state, caller, testAddr, setInput, 100000, false)
	require.NoError(t, err)

	var success bool
	err = KVStoreABI.UnpackIntoInterface(&success, "set", ret)
	require.NoError(t, err)
	require.True(t, success)

	// Overwrite with new value
	newValue := []byte("overwritten value")
	setInput, err = KVStoreABI.Pack("set", testKey1, newValue)
	require.NoError(t, err)

	ret, _, err = setKV(state, caller, testAddr, setInput, 100000, false)
	require.NoError(t, err)

	err = KVStoreABI.UnpackIntoInterface(&success, "set", ret)
	require.NoError(t, err)
	require.True(t, success)

	// Verify overwrite
	getInput, err := KVStoreABI.Pack("get", testKey1)
	require.NoError(t, err)

	ret, _, err = getKV(state, caller, testAddr, getInput, 100000, false)
	require.NoError(t, err)

	var value []byte
	err = KVStoreABI.UnpackIntoInterface(&value, "get", ret)
	require.NoError(t, err)
	require.Equal(t, newValue, value)

	// Verify key count didn't increase (should still be 1)
	keysInput, err := KVStoreABI.Pack("keys")
	require.NoError(t, err)

	ret, _, err = keysKV(state, caller, testAddr, keysInput, 100000, false)
	require.NoError(t, err)

	var keys [][32]byte
	err = KVStoreABI.UnpackIntoInterface(&keys, "keys", ret)
	require.NoError(t, err)
	require.Len(t, keys, 1)
}

func TestKVStore_EmptyKey(t *testing.T) {
	state := newMockAccessibleState()

	// Test with zero key
	zeroKey := common.Hash{}
	setInput, err := KVStoreABI.Pack("set", zeroKey, testValue1)
	require.NoError(t, err)

	ret, _, err := setKV(state, caller, testAddr, setInput, 100000, false)
	require.NoError(t, err)

	var success bool
	err = KVStoreABI.UnpackIntoInterface(&success, "set", ret)
	require.NoError(t, err)
	require.True(t, success)

	// Verify retrieval
	getInput, err := KVStoreABI.Pack("get", zeroKey)
	require.NoError(t, err)

	ret, _, err = getKV(state, caller, testAddr, getInput, 100000, false)
	require.NoError(t, err)

	var value []byte
	err = KVStoreABI.UnpackIntoInterface(&value, "get", ret)
	require.NoError(t, err)
	require.Equal(t, testValue1, value)
}

func TestKVStore_LargeKeySet(t *testing.T) {
	state := newMockAccessibleState()

	// Set multiple keys to test key enumeration limits
	numKeys := 5
	keys := make([]common.Hash, numKeys)
	values := make([][]byte, numKeys)

	for i := 0; i < numKeys; i++ {
		keys[i] = common.BytesToHash([]byte(fmt.Sprintf("key_%d", i)))
		values[i] = []byte(fmt.Sprintf("value_%d", i))

		setInput, err := KVStoreABI.Pack("set", keys[i], values[i])
		require.NoError(t, err)

		_, _, err = setKV(state, caller, testAddr, setInput, 100000, false)
		require.NoError(t, err)
	}

	// Test keys enumeration
	keysInput, err := KVStoreABI.Pack("keys")
	require.NoError(t, err)

	ret, _, err := keysKV(state, caller, testAddr, keysInput, 100000, false)
	require.NoError(t, err)

	var returnedKeys [][32]byte
	err = KVStoreABI.UnpackIntoInterface(&returnedKeys, "keys", ret)
	require.NoError(t, err)
	require.Len(t, returnedKeys, numKeys)

	// Verify all keys are present
	keySet := make(map[common.Hash]bool)
	for _, key := range keys {
		keySet[key] = true
	}

	for _, returnedKey := range returnedKeys {
		hash := common.BytesToHash(returnedKey[:])
		require.True(t, keySet[hash], "Unexpected key returned: %x", returnedKey)
		delete(keySet, hash)
	}
	require.Empty(t, keySet, "Some keys were not returned")
}
