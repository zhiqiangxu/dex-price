package config

import (
	"encoding/json"
	"io/ioutil"
)

// Pair ...
type Pair struct {
	TargetTokenName string
	TargetTokenAddr string
	PriceTokenName  string
	PriceTokenAddr  string
}

// Swap ...
type Swap struct {
	Name    string
	Factory string
	Pairs   []*Pair
}

// Chain ...
type Chain struct {
	Name  string
	Nodes []string
	Swaps []*Swap
}

// Config ...
type Config struct {
	Listen uint16
	Chains []*Chain
}

// LoadConfig ...
func LoadConfig(confFile string) (config *Config, err error) {
	jsonBytes, err := ioutil.ReadFile(confFile)
	if err != nil {
		return
	}

	config = &Config{}
	err = json.Unmarshal(jsonBytes, config)
	return
}
