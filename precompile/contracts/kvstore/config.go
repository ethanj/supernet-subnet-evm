// (c) 2024, SuperNet Labs. All rights reserved.
// See the file LICENSE for licensing terms.

package kvstore

import (
	"fmt"

	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
)

var _ precompileconfig.Config = &Config{}

// Config implements the precompileconfig.Config interface and
// adds specific configuration for KVStore precompile.
type Config struct {
	precompileconfig.Upgrade
}

// NewConfig returns a config for a network upgrade at [blockTimestamp] that enables
// KVStore.
func NewConfig(blockTimestamp *uint64) *Config {
	return &Config{
		Upgrade: precompileconfig.Upgrade{
			BlockTimestamp: blockTimestamp,
		},
	}
}

// NewDisableConfig returns config for a network upgrade at [blockTimestamp]
// that disables KVStore.
func NewDisableConfig(blockTimestamp *uint64) *Config {
	return &Config{
		Upgrade: precompileconfig.Upgrade{
			BlockTimestamp: blockTimestamp,
			Disable:        true,
		},
	}
}

// Key returns the key for the KVStore precompileconfig.
// This should be the same key as used in the precompile module.
func (*Config) Key() string { return ConfigKey }

// Verify tries to verify Config and returns an error accordingly.
func (c *Config) Verify(chainConfig precompileconfig.ChainConfig) error {
	// Add any KVStore-specific validation here
	// For now, no additional validation is needed
	return nil
}

// Equal returns true if [s] is a [*Config] and it has been configured identical to [c].
func (c *Config) Equal(s precompileconfig.Config) bool {
	// typecast before comparison
	other, ok := (s).(*Config)
	if !ok {
		return false
	}

	return c.Upgrade.Equal(&other.Upgrade)
}

// String returns a string representation of the Config.
func (c *Config) String() string {
	var timestamp interface{} = "null"
	if c.BlockTimestamp != nil {
		timestamp = *c.BlockTimestamp
	}
	return fmt.Sprintf(`{"blockTimestamp":%v,"disable":%t}`,
		timestamp,
		c.Disable,
	)
}
