// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/ethereum/ethash"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/pow"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/hashicorp/golang-lru"
)

func init() {
	runtime.GOMAXPROCS(runtime.NumCPU())
}

func thePow() pow.PoW {
	pow, _ := ethash.NewForTesting()
	return pow
}

func theChainManager(db ethdb.Database, t *testing.T) *ChainManager {
	var eventMux event.TypeMux
	WriteTestNetGenesisBlock(db, 0)
	chainMan, err := NewChainManager(db, thePow(), &eventMux)
	if err != nil {
		t.Error("failed creating chainmanager:", err)
		t.FailNow()
		return nil
	}
	blockMan := NewBlockProcessor(db, nil, chainMan, &eventMux)
	chainMan.SetProcessor(blockMan)

	return chainMan
}

// Test fork of length N starting from block i
func testFork(t *testing.T, bman *BlockProcessor, i, N int, f func(td1, td2 *big.Int)) {
	// switch databases to process the new chain
	db, err := ethdb.NewMemDatabase()
	if err != nil {
		t.Fatal("Failed to create db:", err)
	}
	// copy old chain up to i into new db with deterministic canonical
	bman2, err := newCanonical(i, db)
	if err != nil {
		t.Fatal("could not make new canonical in testFork", err)
	}
	// asert the bmans have the same block at i
	bi1 := bman.bc.GetBlockByNumber(uint64(i)).Hash()
	bi2 := bman2.bc.GetBlockByNumber(uint64(i)).Hash()
	if bi1 != bi2 {
		fmt.Printf("%+v\n%+v\n\n", bi1, bi2)
		t.Fatal("chains do not have the same hash at height", i)
	}
	bman2.bc.SetProcessor(bman2)

	// extend the fork
	parent := bman2.bc.CurrentBlock()
	chainB := makeChain(parent, N, db, forkSeed)
	_, err = bman2.bc.InsertChain(chainB)
	if err != nil {
		t.Fatal("Insert chain error for fork:", err)
	}

	tdpre := bman.bc.Td()
	// Test the fork's blocks on the original chain
	td, err := testChain(chainB, bman)
	if err != nil {
		t.Fatal("expected chainB not to give errors:", err)
	}
	// Compare difficulties
	f(tdpre, td)

	// Loop over parents making sure reconstruction is done properly
}

func printChain(bc *ChainManager) {
	for i := bc.CurrentBlock().Number().Uint64(); i > 0; i-- {
		b := bc.GetBlockByNumber(uint64(i))
		fmt.Printf("\t%x %v\n", b.Hash(), b.Difficulty())
	}
}

// process blocks against a chain
func testChain(chainB types.Blocks, bman *BlockProcessor) (*big.Int, error) {
	for _, block := range chainB {
		_, _, err := bman.bc.processor.Process(block)
		if err != nil {
			if IsKnownBlockErr(err) {
				continue
			}
			return nil, err
		}
		bman.bc.mu.Lock()
		WriteTd(bman.bc.chainDb, block.Hash(), new(big.Int).Add(block.Difficulty(), bman.bc.GetTd(block.ParentHash())))
		WriteBlock(bman.bc.chainDb, block)
		bman.bc.mu.Unlock()
	}
	return bman.bc.GetTd(chainB[len(chainB)-1].Hash()), nil
}

func loadChain(fn string, t *testing.T) (types.Blocks, error) {
	fh, err := os.OpenFile(filepath.Join("..", "_data", fn), os.O_RDONLY, os.ModePerm)
	if err != nil {
		return nil, err
	}
	defer fh.Close()

	var chain types.Blocks
	if err := rlp.Decode(fh, &chain); err != nil {
		return nil, err
	}

	return chain, nil
}

func insertChain(done chan bool, chainMan *ChainManager, chain types.Blocks, t *testing.T) {
	_, err := chainMan.InsertChain(chain)
	if err != nil {
		fmt.Println(err)
		t.FailNow()
	}
	done <- true
}

func TestExtendCanonical(t *testing.T) {
	CanonicalLength := 5
	db, err := ethdb.NewMemDatabase()
	if err != nil {
		t.Fatal("Failed to create db:", err)
	}
	// make first chain starting from genesis
	bman, err := newCanonical(CanonicalLength, db)
	if err != nil {
		t.Fatal("Could not make new canonical chain:", err)
	}
	f := func(td1, td2 *big.Int) {
		if td2.Cmp(td1) <= 0 {
			t.Error("expected chainB to have higher difficulty. Got", td2, "expected more than", td1)
		}
	}
	// Start fork from current height (CanonicalLength)
	testFork(t, bman, CanonicalLength, 1, f)
	testFork(t, bman, CanonicalLength, 2, f)
	testFork(t, bman, CanonicalLength, 5, f)
	testFork(t, bman, CanonicalLength, 10, f)
}

func TestShorterFork(t *testing.T) {
	db, err := ethdb.NewMemDatabase()
	if err != nil {
		t.Fatal("Failed to create db:", err)
	}
	// make first chain starting from genesis
	bman, err := newCanonical(10, db)
	if err != nil {
		t.Fatal("Could not make new canonical chain:", err)
	}
	f := func(td1, td2 *big.Int) {
		if td2.Cmp(td1) >= 0 {
			t.Error("expected chainB to have lower difficulty. Got", td2, "expected less than", td1)
		}
	}
	// Sum of numbers must be less than 10
	// for this to be a shorter fork
	testFork(t, bman, 0, 3, f)
	testFork(t, bman, 0, 7, f)
	testFork(t, bman, 1, 1, f)
	testFork(t, bman, 1, 7, f)
	testFork(t, bman, 5, 3, f)
	testFork(t, bman, 5, 4, f)
}

func TestLongerFork(t *testing.T) {
	db, err := ethdb.NewMemDatabase()
	if err != nil {
		t.Fatal("Failed to create db:", err)
	}
	// make first chain starting from genesis
	bman, err := newCanonical(10, db)
	if err != nil {
		t.Fatal("Could not make new canonical chain:", err)
	}
	f := func(td1, td2 *big.Int) {
		if td2.Cmp(td1) <= 0 {
			t.Error("expected chainB to have higher difficulty. Got", td2, "expected more than", td1)
		}
	}
	// Sum of numbers must be greater than 10
	// for this to be a longer fork
	testFork(t, bman, 0, 11, f)
	testFork(t, bman, 0, 15, f)
	testFork(t, bman, 1, 10, f)
	testFork(t, bman, 1, 12, f)
	testFork(t, bman, 5, 6, f)
	testFork(t, bman, 5, 8, f)
}

func TestEqualFork(t *testing.T) {
	db, err := ethdb.NewMemDatabase()
	if err != nil {
		t.Fatal("Failed to create db:", err)
	}
	bman, err := newCanonical(10, db)
	if err != nil {
		t.Fatal("Could not make new canonical chain:", err)
	}
	f := func(td1, td2 *big.Int) {
		if td2.Cmp(td1) != 0 {
			t.Error("expected chainB to have equal difficulty. Got", td2, "expected ", td1)
		}
	}
	// Sum of numbers must be equal to 10
	// for this to be an equal fork
	testFork(t, bman, 0, 10, f)
	testFork(t, bman, 1, 9, f)
	testFork(t, bman, 2, 8, f)
	testFork(t, bman, 5, 5, f)
	testFork(t, bman, 6, 4, f)
	testFork(t, bman, 9, 1, f)
}

func TestBrokenChain(t *testing.T) {
	db, err := ethdb.NewMemDatabase()
	if err != nil {
		t.Fatal("Failed to create db:", err)
	}
	bman, err := newCanonical(10, db)
	if err != nil {
		t.Fatal("Could not make new canonical chain:", err)
	}
	db2, err := ethdb.NewMemDatabase()
	if err != nil {
		t.Fatal("Failed to create db:", err)
	}
	bman2, err := newCanonical(10, db2)
	if err != nil {
		t.Fatal("Could not make new canonical chain:", err)
	}
	bman2.bc.SetProcessor(bman2)
	parent := bman2.bc.CurrentBlock()
	chainB := makeChain(parent, 5, db2, forkSeed)
	chainB = chainB[1:]
	_, err = testChain(chainB, bman)
	if err == nil {
		t.Error("expected broken chain to return error")
	}
}

func TestChainInsertions(t *testing.T) {
	t.Skip("Skipped: outdated test files")

	db, _ := ethdb.NewMemDatabase()

	chain1, err := loadChain("valid1", t)
	if err != nil {
		fmt.Println(err)
		t.FailNow()
	}

	chain2, err := loadChain("valid2", t)
	if err != nil {
		fmt.Println(err)
		t.FailNow()
	}

	chainMan := theChainManager(db, t)

	const max = 2
	done := make(chan bool, max)

	go insertChain(done, chainMan, chain1, t)
	go insertChain(done, chainMan, chain2, t)

	for i := 0; i < max; i++ {
		<-done
	}

	if chain2[len(chain2)-1].Hash() != chainMan.CurrentBlock().Hash() {
		t.Error("chain2 is canonical and shouldn't be")
	}

	if chain1[len(chain1)-1].Hash() != chainMan.CurrentBlock().Hash() {
		t.Error("chain1 isn't canonical and should be")
	}
}

func TestChainMultipleInsertions(t *testing.T) {
	t.Skip("Skipped: outdated test files")

	db, _ := ethdb.NewMemDatabase()

	const max = 4
	chains := make([]types.Blocks, max)
	var longest int
	for i := 0; i < max; i++ {
		var err error
		name := "valid" + strconv.Itoa(i+1)
		chains[i], err = loadChain(name, t)
		if len(chains[i]) >= len(chains[longest]) {
			longest = i
		}
		fmt.Println("loaded", name, "with a length of", len(chains[i]))
		if err != nil {
			fmt.Println(err)
			t.FailNow()
		}
	}

	chainMan := theChainManager(db, t)

	done := make(chan bool, max)
	for i, chain := range chains {
		// XXX the go routine would otherwise reference the same (chain[3]) variable and fail
		i := i
		chain := chain
		go func() {
			insertChain(done, chainMan, chain, t)
			fmt.Println(i, "done")
		}()
	}

	for i := 0; i < max; i++ {
		<-done
	}

	if chains[longest][len(chains[longest])-1].Hash() != chainMan.CurrentBlock().Hash() {
		t.Error("Invalid canonical chain")
	}
}

type bproc struct{}

func (bproc) Process(*types.Block) (state.Logs, types.Receipts, error) { return nil, nil, nil }

func makeChainWithDiff(genesis *types.Block, d []int, seed byte) []*types.Block {
	var chain []*types.Block
	for i, difficulty := range d {
		header := &types.Header{
			Coinbase:   common.Address{seed},
			Number:     big.NewInt(int64(i + 1)),
			Difficulty: big.NewInt(int64(difficulty)),
		}
		if i == 0 {
			header.ParentHash = genesis.Hash()
		} else {
			header.ParentHash = chain[i-1].Hash()
		}
		block := types.NewBlockWithHeader(header)
		chain = append(chain, block)
	}
	return chain
}

func chm(genesis *types.Block, db ethdb.Database) *ChainManager {
	var eventMux event.TypeMux
	bc := &ChainManager{chainDb: db, genesisBlock: genesis, eventMux: &eventMux, pow: FakePow{}}
	bc.headerCache, _ = lru.New(100)
	bc.bodyCache, _ = lru.New(100)
	bc.bodyRLPCache, _ = lru.New(100)
	bc.tdCache, _ = lru.New(100)
	bc.blockCache, _ = lru.New(100)
	bc.futureBlocks, _ = lru.New(100)
	bc.processor = bproc{}
	bc.ResetWithGenesisBlock(genesis)

	return bc
}

func TestReorgLongest(t *testing.T) {
	db, _ := ethdb.NewMemDatabase()

	genesis, err := WriteTestNetGenesisBlock(db, 0)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}
	bc := chm(genesis, db)

	chain1 := makeChainWithDiff(genesis, []int{1, 2, 4}, 10)
	chain2 := makeChainWithDiff(genesis, []int{1, 2, 3, 4}, 11)

	bc.InsertChain(chain1)
	bc.InsertChain(chain2)

	prev := bc.CurrentBlock()
	for block := bc.GetBlockByNumber(bc.CurrentBlock().NumberU64() - 1); block.NumberU64() != 0; prev, block = block, bc.GetBlockByNumber(block.NumberU64()-1) {
		if prev.ParentHash() != block.Hash() {
			t.Errorf("parent hash mismatch %x - %x", prev.ParentHash(), block.Hash())
		}
	}
}

func TestReorgShortest(t *testing.T) {
	db, _ := ethdb.NewMemDatabase()
	genesis, err := WriteTestNetGenesisBlock(db, 0)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}
	bc := chm(genesis, db)

	chain1 := makeChainWithDiff(genesis, []int{1, 2, 3, 4}, 10)
	chain2 := makeChainWithDiff(genesis, []int{1, 10}, 11)

	bc.InsertChain(chain1)
	bc.InsertChain(chain2)

	prev := bc.CurrentBlock()
	for block := bc.GetBlockByNumber(bc.CurrentBlock().NumberU64() - 1); block.NumberU64() != 0; prev, block = block, bc.GetBlockByNumber(block.NumberU64()-1) {
		if prev.ParentHash() != block.Hash() {
			t.Errorf("parent hash mismatch %x - %x", prev.ParentHash(), block.Hash())
		}
	}
}

func TestInsertNonceError(t *testing.T) {
	for i := 1; i < 25 && !t.Failed(); i++ {
		db, _ := ethdb.NewMemDatabase()
		genesis, err := WriteTestNetGenesisBlock(db, 0)
		if err != nil {
			t.Error(err)
			t.FailNow()
		}
		bc := chm(genesis, db)
		bc.processor = NewBlockProcessor(db, bc.pow, bc, bc.eventMux)
		blocks := makeChain(bc.currentBlock, i, db, 0)

		fail := rand.Int() % len(blocks)
		failblock := blocks[fail]
		bc.pow = failpow{failblock.NumberU64()}
		n, err := bc.InsertChain(blocks)

		// Check that the returned error indicates the nonce failure.
		if n != fail {
			t.Errorf("(i=%d) wrong failed block index: got %d, want %d", i, n, fail)
		}
		if !IsBlockNonceErr(err) {
			t.Fatalf("(i=%d) got %q, want a nonce error", i, err)
		}
		nerr := err.(*BlockNonceErr)
		if nerr.Number.Cmp(failblock.Number()) != 0 {
			t.Errorf("(i=%d) wrong block number in error, got %v, want %v", i, nerr.Number, failblock.Number())
		}
		if nerr.Hash != failblock.Hash() {
			t.Errorf("(i=%d) wrong block hash in error, got %v, want %v", i, nerr.Hash, failblock.Hash())
		}

		// Check that all no blocks after the failing block have been inserted.
		for _, block := range blocks[fail:] {
			if bc.HasBlock(block.Hash()) {
				t.Errorf("(i=%d) invalid block %d present in chain", i, block.NumberU64())
			}
		}
	}
}

func TestChainReorgMissingTransactions(t *testing.T) {
	params.MinGasLimit = big.NewInt(125000)      // Minimum the gas limit may ever be.
	params.GenesisGasLimit = big.NewInt(3141592) // Gas limit of the Genesis block.

	var (
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		db, _   = ethdb.NewMemDatabase()
	)
	// Ensure that key1 has some funds in the genesis block.
	genesis := WriteGenesisBlockForTesting(db, GenesisAccount{addr1, big.NewInt(1000000)}, GenesisAccount{addr2, big.NewInt(1000000)})

	shared, _ := types.NewTransaction(0, addr1, big.NewInt(1000), params.TxGas, nil, nil).SignECDSA(key2)
	var remove *types.Transaction
	chain := GenerateChain(genesis, db, 2, func(i int, gen *BlockGen) {
		switch i {
		case 0:
			remove, _ = types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(1000), params.TxGas, nil, nil).SignECDSA(key1)

			gen.AddTx(remove)
			gen.AddTx(shared)
		}
	})

	// Import the chain. This runs all block validation rules.
	evmux := &event.TypeMux{}
	chainman, _ := NewChainManager(db, FakePow{}, evmux)
	chainman.SetProcessor(NewBlockProcessor(db, FakePow{}, chainman, evmux))
	if _, err := chainman.InsertChain(chain); err != nil {
		t.Error(err)
	}

	var keep1, keep2 *types.Transaction
	// overwrite the old chain
	chain = GenerateChain(genesis, db, 5, func(i int, gen *BlockGen) {
		switch i {
		case 2:
			gen.AddTx(shared)
			keep1, _ = types.NewTransaction(gen.TxNonce(addr2), addr2, big.NewInt(1000), params.TxGas, nil, nil).SignECDSA(key2)
			gen.AddTx(keep1)
		case 3:
			keep2, _ = types.NewTransaction(gen.TxNonce(addr2), addr2, big.NewInt(1000), params.TxGas, nil, nil).SignECDSA(key2)
			gen.AddTx(keep2)
		}
	})
	if _, err := chainman.InsertChain(chain); err != nil {
		t.Error(err)
	}

	// removed tx
	if GetTransaction(db, remove.Hash()) != nil {
		t.Error("tx found while shouldn't have been")
	}
	if GetReceipt(db, remove.Hash()) != nil {
		t.Error("receipt found while shouldn't have been")
	}

	// kept tx
	if GetTransaction(db, keep1.Hash()) == nil {
		t.Error("expected tx to be found")
	}
	if GetReceipt(db, keep1.Hash()) == nil {
		t.Error("expected receipt to be found")
	}
	if GetTransaction(db, keep2.Hash()) == nil {
		t.Error("expected tx to be found")
	}
	if GetReceipt(db, keep2.Hash()) == nil {
		t.Error("expected receipt to be found")
	}

	// shared tx
	if GetTransaction(db, shared.Hash()) == nil {
		t.Error("expected tx to be found")
	}
	if GetReceipt(db, shared.Hash()) == nil {
		t.Error("expected receipt to be found")
	}
}

// failpow returns false from Verify for a certain block number.
type failpow struct{ num uint64 }

func (pow failpow) Search(pow.Block, <-chan struct{}) (nonce uint64, mixHash []byte) {
	return 0, nil
}
func (pow failpow) Verify(b pow.Block) bool {
	return b.NumberU64() != pow.num
}
func (pow failpow) GetHashrate() int64 {
	return 0
}
func (pow failpow) Turbo(bool) {
}
