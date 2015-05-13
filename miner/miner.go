package miner

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
)

// Miner is the entity retrieving the work to be done and scheduling it to the
// registered agents.
type Miner interface {
	SetGasPrice(price *big.Int)
	SetExtra(extra []byte)
	Start(coinbase common.Address, threads int)
	Stop()
	Mining() bool
	Register(agent Agent)
	HashRate() int64
	PendingState() *state.StateDB
	PendingBlock() *types.Block
}

// Agent can register itself with the worker.
type Agent interface {
	Work() chan<- *types.Block
	SetReturnCh(chan<- *types.Block)
	Stop()
	Start()
	GetHashRate() int64
}

// Work holds the current work.
type Work struct {
	Number    uint64
	Nonce     uint64
	MixDigest []byte
	SeedHash  []byte
}
