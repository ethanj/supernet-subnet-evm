// (c) 2024, SuperNet Labs. All rights reserved.
// See the file LICENSE for licensing terms.

package kvstore

import (
	_ "embed"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/subnet-evm/precompile/contract"
)

const (
	// Gas costs based on specification
	BaseGasCost     = 2100
	GasPerByte      = 68
	SetGasCost      = 20000
	GetGasCost      = 800
	ExistsGasCost   = 400
	DeleteGasCost   = 5000
	KeysBaseGasCost = 2000
	KeysPerKeyGas   = 400

	// Storage limits
	MaxValueSize   = 32 * 1024   // 32KB
	MaxAccountSize = 1024 * 1024 // 1MB
	MaxKeysPerCall = 100

	// Input lengths for validation
	SetInputMinLen = common.HashLength + 32 // key + dynamic bytes offset
	GetInputLen    = common.HashLength      // key only
	ExistsInputLen = common.HashLength      // key only
	DeleteInputLen = common.HashLength      // key only
	KeysInputLen   = 0                      // no inputs
)

var (
	// Singleton StatefulPrecompiledContract for KV storage operations.
	ContractKVStorePrecompile contract.StatefulPrecompiledContract = createKVStorePrecompile()

	// Error definitions
	ErrInvalidInputLength   = errors.New("invalid input length")
	ErrValueTooLarge        = errors.New("value exceeds maximum size")
	ErrKeyNotFound          = errors.New("key not found")
	ErrStorageLimitExceeded = errors.New("account storage limit exceeded")

	// KVStoreRawABI contains the raw ABI of KVStore contract.
	//go:embed contract.abi
	KVStoreRawABI string

	KVStoreABI = contract.ParseABI(KVStoreRawABI)

	// Storage key prefixes
	dataPrefix = []byte("data_")
	countKey   = common.BytesToHash([]byte("_count"))
	keysPrefix = []byte("keys_")
)

// SetInput represents the input for the set function
type SetInput struct {
	Key   common.Hash
	Value []byte
}

// createKVStorePrecompile returns a StatefulPrecompiledContract for KV storage operations.
func createKVStorePrecompile() contract.StatefulPrecompiledContract {
	var functions []*contract.StatefulPrecompileFunction

	functions = append(functions, contract.NewStatefulPrecompileFunction(
		crypto.Keccak256Hash([]byte("set(bytes32,bytes)")).Bytes()[:4],
		setKV,
	))

	functions = append(functions, contract.NewStatefulPrecompileFunction(
		crypto.Keccak256Hash([]byte("get(bytes32)")).Bytes()[:4],
		getKV,
	))

	functions = append(functions, contract.NewStatefulPrecompileFunction(
		crypto.Keccak256Hash([]byte("exists(bytes32)")).Bytes()[:4],
		existsKV,
	))

	functions = append(functions, contract.NewStatefulPrecompileFunction(
		crypto.Keccak256Hash([]byte("delete(bytes32)")).Bytes()[:4],
		deleteKV,
	))

	functions = append(functions, contract.NewStatefulPrecompileFunction(
		crypto.Keccak256Hash([]byte("keys()")).Bytes()[:4],
		keysKV,
	))

	// Fallback function
	fallback := func(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
		return nil, suppliedGas, fmt.Errorf("invalid function selector")
	}

	statefulContract, err := contract.NewStatefulPrecompileContract(fallback, functions)
	if err != nil {
		panic(err)
	}
	return statefulContract
}

// setKV implements the set(bytes32,bytes) function
func setKV(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	// Start metrics collection
	startTime := time.Now()
	metrics := GetMetricsCollector()
	defer func() {
		executionTime := time.Since(startTime)
		gasUsed := suppliedGas - remainingGas
		success := err == nil
		valueSize := 0
		if len(input) >= SetInputMinLen {
			// Extract value size from input for metrics
			if unpackedInput, unpackErr := KVStoreABI.UnpackInput("set", input, false); unpackErr == nil && len(unpackedInput) > 1 {
				if value, ok := unpackedInput[1].([]byte); ok {
					valueSize = len(value)
				}
			}
		}
		metrics.RecordSetOperation(gasUsed, valueSize, executionTime, success)
	}()

	if readOnly {
		return nil, suppliedGas, vm.ErrWriteProtection
	}

	if len(input) < SetInputMinLen {
		return nil, suppliedGas, ErrInvalidInputLength
	}

	// Parse input using ABI (skip first 4 bytes which are function selector)
	unpackedInput, err := KVStoreABI.UnpackInput("set", input[4:], false)
	if err != nil {
		return nil, suppliedGas, fmt.Errorf("failed to unpack input: %w", err)
	}

	key := unpackedInput[0].([32]byte)
	value := unpackedInput[1].([]byte)

	// Validate value size
	if len(value) > MaxValueSize {
		return nil, suppliedGas, ErrValueTooLarge
	}

	// Calculate gas cost
	gasCost := SetGasCost + uint64(len(value))*GasPerByte
	if suppliedGas < gasCost {
		return nil, suppliedGas, vm.ErrOutOfGas
	}

	// Check if key already exists
	keyHash := common.BytesToHash(key[:])
	dataKey := getDataKey(keyHash)

	stateDB := accessibleState.GetStateDB()
	existingValue := stateDB.GetState(addr, dataKey)
	keyExists := existingValue != (common.Hash{})

	// Store the value
	valueHash := crypto.Keccak256Hash(value)
	stateDB.SetState(addr, dataKey, valueHash)

	// Store the actual data in a separate location
	valueKey := getValueKey(keyHash)
	storeBytes(stateDB, addr, valueKey, value)

	// If this is a new key, add to keys list first, then increment count
	if !keyExists {
		addToKeysList(stateDB, addr, keyHash)
		incrementKeyCount(stateDB, addr)
	}

	// Emit KeySet event
	topics := []common.Hash{
		KVStoreABI.Events["KeySet"].ID,
		keyHash,
		valueHash,
		common.BytesToHash(caller.Bytes()),
	}
	stateDB.AddLog(&types.Log{
		Address: addr,
		Topics:  topics,
		Data:    []byte{},
	})

	// Return true (success)
	output, err := KVStoreABI.PackOutput("set", true)
	if err != nil {
		return nil, suppliedGas - gasCost, fmt.Errorf("failed to pack output: %w", err)
	}

	return output, suppliedGas - gasCost, nil
}

// getKV implements the get(bytes32) function
func getKV(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	// Start metrics collection
	startTime := time.Now()
	metrics := GetMetricsCollector()
	defer func() {
		executionTime := time.Since(startTime)
		gasUsed := suppliedGas - remainingGas
		success := err == nil
		metrics.RecordGetOperation(gasUsed, executionTime, success)
	}()

	if len(input) != GetInputLen+4 { // +4 for function selector
		return nil, suppliedGas, ErrInvalidInputLength
	}

	// Parse input (skip first 4 bytes which are function selector)
	unpackedInput, err := KVStoreABI.UnpackInput("get", input[4:], false)
	if err != nil {
		return nil, suppliedGas, fmt.Errorf("failed to unpack input: %w", err)
	}

	key := unpackedInput[0].([32]byte)
	keyHash := common.BytesToHash(key[:])

	// Check if key exists
	stateDB := accessibleState.GetStateDB()
	dataKey := getDataKey(keyHash)
	valueHash := stateDB.GetState(addr, dataKey)

	if valueHash == (common.Hash{}) {
		// Key doesn't exist, return empty bytes
		output, err := KVStoreABI.PackOutput("get", []byte{})
		if err != nil {
			return nil, suppliedGas, fmt.Errorf("failed to pack output: %w", err)
		}
		return output, suppliedGas - GetGasCost, nil
	}

	// Retrieve the actual value
	valueKey := getValueKey(keyHash)
	value := loadBytes(stateDB, addr, valueKey)

	// Calculate gas cost
	gasCost := GetGasCost + uint64(len(value))*GasPerByte
	if suppliedGas < gasCost {
		return nil, suppliedGas, vm.ErrOutOfGas
	}

	output, err := KVStoreABI.PackOutput("get", value)
	if err != nil {
		return nil, suppliedGas - gasCost, fmt.Errorf("failed to pack output: %w", err)
	}

	return output, suppliedGas - gasCost, nil
}

// Helper functions for storage key generation
func getDataKey(key common.Hash) common.Hash {
	return crypto.Keccak256Hash(append(dataPrefix, key.Bytes()...))
}

func getValueKey(key common.Hash) common.Hash {
	return crypto.Keccak256Hash(append(dataPrefix, append(key.Bytes(), []byte("_value")...)...))
}

func getKeysIndexKey(index *big.Int) common.Hash {
	return crypto.Keccak256Hash(append(keysPrefix, index.Bytes()...))
}

// Helper functions for byte storage/retrieval
func storeBytes(stateDB contract.StateDB, addr common.Address, baseKey common.Hash, data []byte) {
	// Store length first
	lengthKey := crypto.Keccak256Hash(append(baseKey.Bytes(), []byte("_len")...))
	stateDB.SetState(addr, lengthKey, common.BigToHash(big.NewInt(int64(len(data)))))

	// Store data in chunks of 32 bytes
	for i := 0; i < len(data); i += 32 {
		end := i + 32
		if end > len(data) {
			end = len(data)
		}

		chunk := make([]byte, 32)
		copy(chunk, data[i:end])

		chunkKey := crypto.Keccak256Hash(append(baseKey.Bytes(), big.NewInt(int64(i/32)).Bytes()...))
		stateDB.SetState(addr, chunkKey, common.BytesToHash(chunk))
	}
}

func loadBytes(stateDB contract.StateDB, addr common.Address, baseKey common.Hash) []byte {
	// Load length first
	lengthKey := crypto.Keccak256Hash(append(baseKey.Bytes(), []byte("_len")...))
	lengthHash := stateDB.GetState(addr, lengthKey)
	length := lengthHash.Big().Int64()

	if length == 0 {
		return []byte{}
	}

	// Load data chunks
	data := make([]byte, length)
	for i := int64(0); i < length; i += 32 {
		chunkKey := crypto.Keccak256Hash(append(baseKey.Bytes(), big.NewInt(i/32).Bytes()...))
		chunk := stateDB.GetState(addr, chunkKey)

		end := i + 32
		if end > length {
			end = length
		}

		copy(data[i:end], chunk.Bytes()[:end-i])
	}

	return data
}

func incrementKeyCount(stateDB contract.StateDB, addr common.Address) {
	currentCount := stateDB.GetState(addr, countKey).Big()
	newCount := new(big.Int).Add(currentCount, big.NewInt(1))
	stateDB.SetState(addr, countKey, common.BigToHash(newCount))

	// Update metrics
	metrics := GetMetricsCollector()
	metrics.UpdateKeyCount(newCount.Int64())
}

func addToKeysList(stateDB contract.StateDB, addr common.Address, key common.Hash) {
	currentCount := stateDB.GetState(addr, countKey).Big()
	indexKey := getKeysIndexKey(currentCount)
	stateDB.SetState(addr, indexKey, key)
}

// existsKV implements the exists(bytes32) function
func existsKV(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	// Start metrics collection
	startTime := time.Now()
	metrics := GetMetricsCollector()
	defer func() {
		executionTime := time.Since(startTime)
		gasUsed := suppliedGas - remainingGas
		success := err == nil
		metrics.RecordExistsOperation(gasUsed, executionTime, success)
	}()

	if len(input) != ExistsInputLen+4 { // +4 for function selector
		return nil, suppliedGas, ErrInvalidInputLength
	}

	if suppliedGas < ExistsGasCost {
		return nil, suppliedGas, vm.ErrOutOfGas
	}

	// Parse input (skip first 4 bytes which are function selector)
	unpackedInput, err := KVStoreABI.UnpackInput("exists", input[4:], false)
	if err != nil {
		return nil, suppliedGas, fmt.Errorf("failed to unpack input: %w", err)
	}

	key := unpackedInput[0].([32]byte)
	keyHash := common.BytesToHash(key[:])

	// Check if key exists
	stateDB := accessibleState.GetStateDB()
	dataKey := getDataKey(keyHash)
	valueHash := stateDB.GetState(addr, dataKey)

	exists := valueHash != (common.Hash{})

	output, err := KVStoreABI.PackOutput("exists", exists)
	if err != nil {
		return nil, suppliedGas - ExistsGasCost, fmt.Errorf("failed to pack output: %w", err)
	}

	return output, suppliedGas - ExistsGasCost, nil
}

// deleteKV implements the delete(bytes32) function
func deleteKV(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	// Start metrics collection
	startTime := time.Now()
	metrics := GetMetricsCollector()
	defer func() {
		executionTime := time.Since(startTime)
		gasUsed := suppliedGas - remainingGas
		success := err == nil
		metrics.RecordDeleteOperation(gasUsed, executionTime, success)
	}()

	if readOnly {
		return nil, suppliedGas, vm.ErrWriteProtection
	}

	if len(input) != DeleteInputLen+4 { // +4 for function selector
		return nil, suppliedGas, ErrInvalidInputLength
	}

	if suppliedGas < DeleteGasCost {
		return nil, suppliedGas, vm.ErrOutOfGas
	}

	// Parse input (skip first 4 bytes which are function selector)
	unpackedInput, err := KVStoreABI.UnpackInput("delete", input[4:], false)
	if err != nil {
		return nil, suppliedGas, fmt.Errorf("failed to unpack input: %w", err)
	}

	key := unpackedInput[0].([32]byte)
	keyHash := common.BytesToHash(key[:])

	// Check if key exists
	stateDB := accessibleState.GetStateDB()
	dataKey := getDataKey(keyHash)
	valueHash := stateDB.GetState(addr, dataKey)

	if valueHash == (common.Hash{}) {
		// Key doesn't exist, return false
		output, err := KVStoreABI.PackOutput("delete", false)
		if err != nil {
			return nil, suppliedGas - DeleteGasCost, fmt.Errorf("failed to pack output: %w", err)
		}
		return output, suppliedGas - DeleteGasCost, nil
	}

	// Delete the key-value pair
	stateDB.SetState(addr, dataKey, common.Hash{})

	// Delete the actual value data
	valueKey := getValueKey(keyHash)
	deleteBytes(stateDB, addr, valueKey)

	// Remove from keys list and decrement count
	removeFromKeysList(stateDB, addr, keyHash)
	decrementKeyCount(stateDB, addr)

	// Emit KeyDeleted event
	topics := []common.Hash{
		KVStoreABI.Events["KeyDeleted"].ID,
		keyHash,
		common.BytesToHash(caller.Bytes()),
	}
	stateDB.AddLog(&types.Log{
		Address: addr,
		Topics:  topics,
		Data:    []byte{},
	})

	output, err := KVStoreABI.PackOutput("delete", true)
	if err != nil {
		return nil, suppliedGas - DeleteGasCost, fmt.Errorf("failed to pack output: %w", err)
	}

	return output, suppliedGas - DeleteGasCost, nil
}

// keysKV implements the keys() function
func keysKV(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	// Start metrics collection
	startTime := time.Now()
	metrics := GetMetricsCollector()
	defer func() {
		executionTime := time.Since(startTime)
		gasUsed := suppliedGas - remainingGas
		success := err == nil
		keyCount := 0
		if success {
			// Get key count from state
			stateDB := accessibleState.GetStateDB()
			keyCount = int(stateDB.GetState(addr, countKey).Big().Int64())
		}
		metrics.RecordKeysOperation(gasUsed, keyCount, executionTime, success)
	}()

	if len(input) != KeysInputLen+4 { // +4 for function selector
		return nil, suppliedGas, ErrInvalidInputLength
	}

	stateDB := accessibleState.GetStateDB()
	totalCount := stateDB.GetState(addr, countKey).Big().Int64()

	// Limit the number of keys returned
	keysToReturn := totalCount
	if keysToReturn > MaxKeysPerCall {
		keysToReturn = MaxKeysPerCall
	}

	// Calculate gas cost
	gasCost := KeysBaseGasCost + uint64(keysToReturn)*KeysPerKeyGas
	if suppliedGas < gasCost {
		return nil, suppliedGas, vm.ErrOutOfGas
	}

	// Collect keys
	keys := make([][32]byte, 0, keysToReturn)
	for i := int64(0); i < keysToReturn; i++ {
		indexKey := getKeysIndexKey(big.NewInt(i))
		keyHash := stateDB.GetState(addr, indexKey)
		if keyHash != (common.Hash{}) {
			keys = append(keys, keyHash)
		}
	}

	output, err := KVStoreABI.PackOutput("keys", keys)
	if err != nil {
		return nil, suppliedGas - gasCost, fmt.Errorf("failed to pack output: %w", err)
	}

	return output, suppliedGas - gasCost, nil
}

// Helper functions for key management
func deleteBytes(stateDB contract.StateDB, addr common.Address, baseKey common.Hash) {
	// Clear length
	lengthKey := crypto.Keccak256Hash(append(baseKey.Bytes(), []byte("_len")...))
	stateDB.SetState(addr, lengthKey, common.Hash{})

	// Note: In a production implementation, we would also clear the data chunks
	// For simplicity, we're just clearing the length marker
}

func decrementKeyCount(stateDB contract.StateDB, addr common.Address) {
	currentCount := stateDB.GetState(addr, countKey).Big()
	if currentCount.Cmp(big.NewInt(0)) > 0 {
		newCount := new(big.Int).Sub(currentCount, big.NewInt(1))
		stateDB.SetState(addr, countKey, common.BigToHash(newCount))

		// Update metrics
		metrics := GetMetricsCollector()
		metrics.UpdateKeyCount(newCount.Int64())
	}
}

func removeFromKeysList(stateDB contract.StateDB, addr common.Address, keyToRemove common.Hash) {
	totalCount := stateDB.GetState(addr, countKey).Big().Int64()

	// Find the key in the list and replace it with the last key
	for i := int64(0); i < totalCount; i++ {
		indexKey := getKeysIndexKey(big.NewInt(i))
		storedKey := stateDB.GetState(addr, indexKey)

		if storedKey == keyToRemove {
			// Replace with the last key
			if i < totalCount-1 {
				lastIndexKey := getKeysIndexKey(big.NewInt(totalCount - 1))
				lastKey := stateDB.GetState(addr, lastIndexKey)
				stateDB.SetState(addr, indexKey, lastKey)
				stateDB.SetState(addr, lastIndexKey, common.Hash{})
			} else {
				// This is the last key, just clear it
				stateDB.SetState(addr, indexKey, common.Hash{})
			}
			break
		}
	}
}
